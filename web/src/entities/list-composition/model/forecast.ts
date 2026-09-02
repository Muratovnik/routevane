import { useDebounceFn } from '@vueuse/core'
import { onScopeDispose, ref } from 'vue'

import { refreshService } from '@/shared/api/catalog'
import { RoutevaneAPIError } from '@/shared/api/http'
import {
  previewComposition,
  type ListComposition,
  type TargetForecast,
} from '@/shared/api/lists'

// A draft settles between clicks, not during them. Half a second is long enough
// that picking three services in a row is one question to the server, and short
// enough that the answer arrives while the operator is still looking at what
// they picked.
const settleDelay = 500

/**
 * Which services this tab has already asked the service to observe on a
 * forecast's behalf.
 *
 * It outlives one screen because the observation it causes belongs to the
 * running service rather than to the screen that asked for it: moving between
 * the composer and a list must not read the same sources again. It is also the
 * loop guard — a service that was observed and still yields no forecast is
 * never observed a second time, whatever the operator does next.
 */
const observedForForecast = new Set<string>()

/**
 * forgetForecastObservations drops that record. Nothing in the product calls
 * it — a reload is what ends a session — and it exists so a test can state the
 * once-per-service guarantee from a known starting point.
 */
export function forgetForecastObservations(): void {
  observedForForecast.clear()
}

type Ask = {
  attempt: number
  composition: ListComposition
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
function unobserved(reason: unknown): boolean {
  return reason instanceof RoutevaneAPIError && reason.status === 404
}

/**
 * What the draft would weigh, per format, kept beside the draft itself.
 *
 * The forecast is an aid and never a gate on availability. A refusal means
 * "no forecast": the screen says nothing and blocks nothing, and a refusal is
 * never read as a fit.
 *
 * One refusal is answered rather than accepted. A service whose sources have
 * never been read cannot be weighed, so the surface reads them once — the same
 * thing the service card does when it opens on unread contents — and asks
 * again. Once per service and once per draft: a second refusal is final, so a
 * catalog that genuinely cannot be forecast costs one round trip, not a loop.
 *
 * A superseded answer is dropped rather than landed, so a slow reply for an
 * earlier draft can never describe the draft on screen. The last delivered
 * answer stays visible while the next one is being asked for: a badge that
 * blinked out on every checkbox would be harder to read than one that is
 * briefly a click behind.
 */
export function useCompositionForecast(delay = settleDelay) {
  const forecasts = ref<TargetForecast[]>([])
  const pending = ref(false)
  // True only while sources are being read for a forecast. It explains a wait;
  // it never gates anything.
  const observing = ref(false)
  // Bumped whenever an answer in flight stops describing the draft. The
  // debouncer owns waiting; this owns which answer is still the current one.
  let issued = 0

  const ask = useDebounceFn((next: Ask) => read(next, false), delay)

  // The server answers sorted by identifier and deduplicated, so a forecast is
  // found by the format it names rather than by the order it was asked for.
  function forTarget(targetID: string): TargetForecast | null {
    return (
      forecasts.value.find((forecast) => forecast.targetID === targetID) ?? null
    )
  }

  async function attemptRead(next: Ask): Promise<Outcome> {
    try {
      const answer = await previewComposition(next.composition, next.targets)
      if (next.attempt !== issued) return 'silent'
      forecasts.value = answer
      return 'landed'
    } catch (reason) {
      if (next.attempt !== issued) return 'silent'
      forecasts.value = []
      return unobserved(reason) ? 'unread' : 'silent'
    }
  }

  async function read(next: Ask, retried: boolean): Promise<void> {
    try {
      const outcome = await attemptRead(next)
      // A draft that was already read once and still cannot be weighed is a
      // draft this surface has no forecast for. It says so by saying nothing.
      if (retried || outcome !== 'unread') return
      await observeThenRetry(next)
    } finally {
      if (next.attempt === issued) pending.value = false
    }
  }

  async function observeThenRetry(next: Ask): Promise<void> {
    const pending = next.resolved.filter((id) => !observedForForecast.has(id))
    // Everything in this draft has already been read once. Reading again would
    // answer the same way, so the screen simply stays quiet.
    if (pending.length === 0) return
    for (const id of pending) observedForForecast.add(id)
    observing.value = true
    try {
      // One failed source must not withhold the forecast the others allow, so
      // every read is awaited and none of them is decisive.
      await Promise.allSettled(pending.map((id) => refreshService(id)))
    } finally {
      observing.value = false
    }
    if (next.attempt !== issued) return
    await read(next, true)
  }

  /**
   * request schedules one read for the draft as it stands. A draft that
   * resolves to no service is not asked about at all — the endpoint refuses it,
   * and there is nothing to forecast.
   */
  function request(
    composition: ListComposition,
    resolved: string[],
    targets: string[] = [],
  ): void {
    issued += 1
    pending.value = resolved.length > 0
    if (resolved.length === 0) {
      ask.cancel()
      forecasts.value = []
      return
    }
    void ask({ attempt: issued, composition, resolved, targets })
  }

  function forget(): void {
    issued += 1
    pending.value = false
    ask.cancel()
    forecasts.value = []
  }

  onScopeDispose(() => {
    issued += 1
    ask.cancel()
  })

  return { forTarget, forecasts, forget, observing, pending, request }
}
