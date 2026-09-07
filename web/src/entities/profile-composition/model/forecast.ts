import { useDebounceFn, useEventListener } from '@vueuse/core'
import { onScopeDispose, ref } from 'vue'

import { listChanges } from '@/shared/lib/listChanges'
import { refreshList } from '@/shared/api/catalog'
import { RoutevaneAPIError } from '@/shared/api/http'
import {
  previewComposition,
  type ProfileComposition,
  type TargetForecast,
} from '@/shared/api/profiles'

// A draft settles between clicks, not during them. Half a second is long enough
// that picking three lists in a row is one question to the server, and short
// enough that the answer arrives while the operator is still looking at what
// they picked.
const SETTLE_DELAY = 500

/**
 * Which lists this tab has already asked the local service to observe on a
 * forecast's behalf.
 *
 * It outlives one screen because the observation it causes belongs to the
 * running service rather than to the screen that asked for it: moving between
 * the composer and a list must not read the same sources again. It is also the
 * loop guard — a list that was observed and still yields no forecast is
 * not automatically observed twice without a source configuration change.
 */
const observedForForecast = new Set<string>()

/**
 * forgetForecastObservations drops that record. Nothing in the product calls
 * it — a reload is what ends a session — and it exists so a test can state the
 * once-per-list guarantee from a known starting point.
 */
export const forgetForecastObservations = (): void => {
  observedForForecast.clear()
}

type Ask = {
  attempt: number
  composition: ProfileComposition
  resolved: string[]
  targets: string[]
}

/**
 * What one read of the endpoint settled.
 *
 * 'landed' — an answer is on screen. 'unread' — the service cannot weigh this
 * draft until it has observed it. 'silent' — anything else, including an
 * answer that no longer describes the draft: the screen says nothing.
 */
type Outcome = 'landed' | 'unread' | 'silent'

// A forecast for a composition nothing has ever observed is not a malformed
// request; it is a question the service cannot answer yet.
const unobserved = (reason: unknown): boolean =>
  reason instanceof RoutevaneAPIError &&
  (reason.status === 404 ||
    (reason.status === 422 && reason.code === 'partial_coverage'))

/**
 * What the draft would weigh, per format, kept beside the draft itself.
 *
 * The forecast is an aid and never a gate on availability. A refusal means
 * "no forecast": the screen reports unavailable data and blocks nothing; it is
 * never read as a fit.
 *
 * One refusal is answered rather than accepted. A list whose sources have
 * never been read cannot be weighed, so the surface reads them once — the same
 * thing the list card does when it opens on unread contents — and asks
 * again. Once per list and once per draft: a second refusal is final, so a
 * catalog that genuinely cannot be forecast costs one round trip, not a loop.
 *
 * A superseded answer is dropped rather than landed, so a slow reply for an
 * earlier draft can never describe the draft on screen. The last delivered
 * answer stays visible while the next one is being asked for: a badge that
 * blinked out on every checkbox would be harder to read than one that is
 * briefly a click behind.
 */
export const useCompositionForecast = (delay = SETTLE_DELAY) => {
  const forecasts = ref<TargetForecast[]>([])
  const pending = ref(false)
  // True only while sources are being read for a forecast. It explains a wait;
  // it never gates anything.
  const observing = ref(false)
  // Bumped whenever an answer in flight stops describing the draft. The
  // debouncer owns waiting; this owns which answer is still the current one.
  let issued = 0
  let current: Ask | null = null
  const failure = ref<'coverage' | 'unavailable' | null>(null)
  const subscription = listChanges.on(({ listID, observed }) => {
    if (!current?.resolved.includes(listID)) return
    if (!observed) observedForForecast.delete(listID)
    if (!observing.value)
      request(current.composition, current.resolved, current.targets)
  })

  // Returning from another library tab must also invalidate observations that
  // can have changed without changing the selected list identifiers.
  useEventListener(window, 'focus', () => {
    if (current && !observing.value)
      request(current.composition, current.resolved, current.targets)
  })

  const ask = useDebounceFn((next: Ask) => read(next, false), delay)

  // The server answers sorted by identifier and deduplicated, so a forecast is
  // found by the format it names rather than by the order it was asked for.
  const forTarget = (targetID: string): TargetForecast | null =>
    forecasts.value.find((forecast) => forecast.targetID === targetID) ?? null

  const attemptRead = async (next: Ask): Promise<Outcome> => {
    try {
      const answer = await previewComposition(next.composition, next.targets)
      if (next.attempt !== issued) return 'silent'
      forecasts.value = answer
      failure.value = null
      return answer.some((row) => (row.incompleteLists?.length ?? 0) > 0)
        ? 'unread'
        : 'landed'
    } catch (reason) {
      if (next.attempt !== issued) return 'silent'
      forecasts.value = []
      failure.value = unobserved(reason) ? 'coverage' : 'unavailable'
      return unobserved(reason) ? 'unread' : 'silent'
    }
  }

  const read = async (next: Ask, retried: boolean): Promise<void> => {
    try {
      const outcome = await attemptRead(next)
      // A draft that was already read once and still cannot be weighed is a
      // draft with unavailable coverage; never turn that refusal into zero.
      if (retried || outcome !== 'unread') return
      await observeThenRetry(next)
    } finally {
      if (next.attempt === issued) pending.value = false
    }
  }

  const observeThenRetry = async (next: Ask, all = false): Promise<void> => {
    const incomplete = new Set(
      forecasts.value.flatMap((row) => row.incompleteLists ?? []),
    )
    const pending = next.resolved.filter(
      (id) =>
        !observedForForecast.has(id) &&
        (all || incomplete.size === 0 || incomplete.has(id)),
    )
    // Everything in this draft has already been read once. Reading again would
    // answer the same way, so the screen simply stays quiet.
    if (pending.length === 0) return
    for (const id of pending) observedForForecast.add(id)
    observing.value = true
    try {
      // One failed source must not withhold the forecast the others allow, so
      // every read is awaited and none of them is decisive.
      await Promise.allSettled(pending.map((id) => refreshList(id)))
    } finally {
      observing.value = false
    }
    if (next.attempt !== issued) return
    await read(next, true)
  }

  /**
   * request schedules one read for the draft as it stands. A draft that
   * resolves to no list is not asked about at all — the endpoint refuses it,
   * and there is nothing to forecast.
   */
  const request = (
    composition: ProfileComposition,
    resolved: string[],
    targets: string[] = [],
  ): void => {
    issued += 1
    current = { attempt: issued, composition, resolved, targets }
    failure.value = null
    pending.value = resolved.length > 0
    if (resolved.length === 0) {
      ask.cancel()
      forecasts.value = []
      return
    }
    void ask({ attempt: issued, composition, resolved, targets })
  }

  // An explicit retry is new operator intent. Let it read the sources again;
  // the session guard above is only for automatic retries inside one attempt.
  const retry = (
    composition: ProfileComposition,
    resolved: string[],
    targets: string[] = [],
  ): void => {
    for (const id of resolved) observedForForecast.delete(id)
    request(composition, resolved, targets)
  }

  const refresh = async (
    composition: ProfileComposition,
    resolved: string[],
    targets: string[] = [],
  ): Promise<void> => {
    if (observing.value) return
    ask.cancel()
    issued += 1
    const next = { attempt: issued, composition, resolved, targets }
    current = next
    failure.value = null
    pending.value = resolved.length > 0
    for (const id of resolved) observedForForecast.delete(id)
    try {
      await observeThenRetry(next, true)
    } finally {
      if (next.attempt === issued) pending.value = false
    }
  }

  const forget = (): void => {
    issued += 1
    current = null
    failure.value = null
    pending.value = false
    ask.cancel()
    forecasts.value = []
  }

  onScopeDispose(() => {
    subscription.off()
    issued += 1
    ask.cancel()
  })

  return {
    forTarget,
    forecasts,
    forget,
    observing,
    pending,
    failure,
    request,
    retry,
    refresh,
  }
}
