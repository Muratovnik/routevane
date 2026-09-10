import { useEventListener, useTimeoutFn } from '@vueuse/core'
import { computed, ref } from 'vue'

/**
 * Whether the local service is answering at all.
 *
 * Every screen already states what its own read or write did. What none of them
 * can state is the fact underneath: that nothing this surface asks for is
 * arriving, so a second attempt on this screen or any other will fail the same
 * way. That fact belongs to the whole surface, so it is module scope — one
 * value the shell reads, whichever screen produced the evidence.
 *
 * A reported failure is not the fact. A single request can be aborted by a
 * navigation, lose a race with a reload, or hit a moment the service was
 * restarting; announcing an outage on one of those would blink a message on and
 * off. So a report starts a check, and only a check that also fails makes the
 * state `unreachable`.
 */

export type ServiceReach = 'ok' | 'unreachable'

const HEALTH_PATH = '/health'

/** How long the service has to answer the check before it counts as silent. */
const PROBE_BUDGET_MILLISECONDS = 4000

/**
 * The desktop package serves this page from its own scheme and translates it
 * to the private backend origin; a request its backend never answered comes
 * back as this status from the transport rather than from the service
 * (`desktop/src/transport.mjs`). It is the one status line that means "nothing
 * answered" instead of "something answered".
 */
const BACKEND_SILENT_STATUS = 503

/**
 * Waits between checks while the service stays silent, in milliseconds. They
 * grow because a service that has been down for a minute is not coming back in
 * the next two seconds, and the last one holds from then on: a long outage is
 * checked twice a minute rather than continuously.
 */
const RETRY_CEILING_MILLISECONDS = 30_000
const retryDelays = [2000, 5000, 10_000, RETRY_CEILING_MILLISECONDS]

const reach = ref<ServiceReach>('ok')
const probing = ref(false)
const sectionFailure = ref('')
const devDisconnected = ref(false)
const attempt = ref(0)

/** The check in flight, so a burst of failed requests asks the service once. */
let running: Promise<boolean> | null = null

const nextDelay = computed(
  () => retryDelays[attempt.value] ?? RETRY_CEILING_MILLISECONDS,
)

const probe = async (): Promise<boolean> => {
  probing.value = true
  try {
    // Deliberately not the reporting transport in `shared/api`: this request is
    // the thing those reports are about, and sending it through them would let
    // the check answer its own question. A cached answer would too, and a check
    // without a budget would hang exactly when the service is unreachable.
    const response = await fetch(HEALTH_PATH, {
      cache: 'no-store',
      method: 'GET',
      signal: AbortSignal.timeout(PROBE_BUDGET_MILLISECONDS),
    })
    // A status line is an answer, and an answer means something is listening.
    // A refusal — 403 for a request this build did not mark, 421 for a host it
    // does not serve — is still an answer, and answering is the whole question
    // here. Only the transport's own 503 above means nothing answered.
    return response.status !== BACKEND_SILENT_STATUS
  } catch {
    // No answer: the browser has no route to the service, the page has no
    // network at all, or the budget ran out.
    return false
  } finally {
    probing.value = false
  }
}

const holdUnreachable = (): void => {
  reach.value = 'unreachable'
  // The only schedule this module keeps, and it exists only while the service
  // is silent: `reportReachable` stops it, and nothing starts it while the
  // service answers. A surface with a working service never polls.
  scheduleCheck()
  attempt.value = Math.min(attempt.value + 1, retryDelays.length)
}

/**
 * Asks the service whether it is there and settles the state on the answer.
 * Concurrent callers share one request.
 */
export const checkService = (): Promise<boolean> => {
  running ??= probe()
    .then((reachable) => {
      if (reachable) reportReachable()
      else holdUnreachable()
      return reachable
    })
    .finally(() => {
      running = null
    })
  return running
}

const { start: scheduleCheck, stop: cancelSchedule } = useTimeoutFn(
  () => {
    void checkService()
  },
  nextDelay,
  { immediate: false },
)

/**
 * A request never reached the service. Not an outage on its own — the check
 * decides — so nothing is announced here.
 */
export const reportTransportFailure = (): void => {
  // Once the outage is established the schedule owns the next question, so a
  // screen that keeps failing does not add traffic to a service that is
  // already known to be silent.
  if (reach.value === 'unreachable') return
  void checkService()
}

/** The service answered. Whatever it answered, it is there. */
export const reportReachable = (): void => {
  cancelSchedule()
  attempt.value = 0
  reach.value = 'ok'
}

/**
 * A section the operator asked for did not arrive while the service itself was
 * answering. The path is kept because recovering means asking for it again.
 */
export const reportSectionFailure = (path: string): void => {
  sectionFailure.value = path
}

export const clearSectionFailure = (): void => {
  sectionFailure.value = ''
}

/**
 * How this surface recovers a section that did not arrive. Reloading a document
 * is the application's own act, not this state's, so
 * `plugins/navigation.client.ts` registers it here: the shell then asks for the
 * recovery by name instead of importing a plugin across the layer order, and a
 * spec can watch what is asked for without a browser reloading anything.
 */
let recoverSection: ((path: string) => Promise<void>) | null = null

export const onSectionRecovery = (
  recover: (path: string) => Promise<void>,
): void => {
  recoverSection = recover
}

/**
 * The operator asked for that section again. Nothing is cleared here: the
 * recovery either replaces this document or finds the service silent, and in
 * the second case the section is still a section that did not open.
 */
export const retrySection = async (): Promise<void> => {
  const path = sectionFailure.value
  if (path === '') return
  await recoverSection?.(path)
}

/** Development only: the development server's own socket went away. */
export const reportDevDisconnected = (disconnected: boolean): void => {
  devDisconnected.value = disconnected
}

if (typeof window !== 'undefined') {
  // Two moments worth asking again, and neither is a schedule: the browser
  // found a network, and the operator came back to the window. Both are
  // ignored while the service is answering, so a healthy surface sends nothing.
  const recheck = (): void => {
    if (reach.value === 'unreachable') void checkService()
  }
  useEventListener(window, 'online', recheck)
  useEventListener(document, 'visibilitychange', () => {
    if (document.visibilityState === 'visible') recheck()
  })
}

export const useServiceHealth = () => ({
  devDisconnected: computed(() => devDisconnected.value),
  probing: computed(() => probing.value),
  reach: computed(() => reach.value),
  sectionFailure: computed(() => sectionFailure.value),
  unreachable: computed(() => reach.value === 'unreachable'),
})
