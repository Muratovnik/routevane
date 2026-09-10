import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  checkService,
  reportReachable,
  reportTransportFailure,
  useServiceHealth,
} from '@/shared/model/useServiceHealth'

const HEALTH = '/health'

const answered = (status: number): Response =>
  new Response(status === 200 ? '{"status":"ok"}' : null, { status })

const stubService = (
  respond: (path: string, init?: RequestInit) => Promise<Response>,
) => {
  const service = vi.fn(respond)
  vi.stubGlobal('fetch', service)
  return service
}

// How each status line is read. The question is only ever "did anything
// answer", so the two refusals below are the interesting pair: one is the
// service speaking for itself, the other is the desktop transport speaking
// about a backend that never did.
const statusLines = [
  { expected: 'ok', status: 200, what: 'an answer' },
  { expected: 'ok', status: 403, what: 'a refusal the service stated itself' },
  { expected: 'ok', status: 421, what: 'a host the service does not serve' },
  {
    expected: 'unreachable',
    status: 503,
    what: "the desktop transport's report that its backend never answered",
  },
]

// This state belongs to the whole surface, so it is one value rather than one
// per caller. A case therefore hands it back the way it found it — answering,
// with nothing scheduled — instead of leaving the next case a starting point it
// never asked for.
afterEach(() => {
  reportReachable()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('service health', () => {
  it('waits for a check before it calls one failed request an outage', async () => {
    const answer = Promise.withResolvers<Response>()
    const service = stubService(async () => answer.promise)
    const { probing, reach } = useServiceHealth()

    reportTransportFailure()
    // The request that failed proves nothing on its own: it could have been
    // abandoned by a navigation or have raced a restart.
    expect(reach.value).toBe('ok')
    expect(probing.value).toBe(true)

    // The same check, not a second one: a screen making six requests at once
    // asks the service once.
    const checked = checkService()
    expect(service).toHaveBeenCalledTimes(1)
    const asked = service.mock.calls[0]
    expect(asked?.[0]).toBe(HEALTH)
    // Relative, so it reaches the service through the development proxy, the
    // served build and the desktop scheme alike; uncached, so a stored answer
    // cannot report a service that has since stopped; and bounded, because a
    // check without a budget hangs exactly when it matters.
    expect(asked?.[1]).toMatchObject({ cache: 'no-store', method: 'GET' })
    expect(asked?.[1]?.signal).toBeInstanceOf(AbortSignal)

    answer.resolve(answered(200))
    expect(await checked).toBe(true)
    expect(reach.value).toBe('ok')
    expect(probing.value).toBe(false)
  })

  it('announces the outage only when the check fails too, and takes it back', async () => {
    stubService(() => Promise.reject(new TypeError('Failed to fetch')))
    const { reach } = useServiceHealth()

    reportTransportFailure()
    expect(reach.value).toBe('ok')
    expect(await checkService()).toBe(false)
    expect(reach.value).toBe('unreachable')

    const recovered = stubService(async () => answered(200))
    expect(await checkService()).toBe(true)
    expect(reach.value).toBe('ok')
    expect(recovered).toHaveBeenCalledTimes(1)
  })

  it.each(statusLines)(
    'reads $what as $expected',
    async ({ status, expected }) => {
      stubService(async () => answered(status))
      const { reach } = useServiceHealth()

      await checkService()

      expect(reach.value).toBe(expected)
    },
  )

  it('stops asking again for every failed request once the outage is known', async () => {
    const silent = stubService(() => Promise.reject(new TypeError('offline')))
    const { reach } = useServiceHealth()

    reportTransportFailure()
    expect(await checkService()).toBe(false)
    expect(reach.value).toBe('unreachable')
    expect(silent).toHaveBeenCalledTimes(1)

    // A screen whose reads keep failing is describing the outage that is
    // already established. The waits own the next question from here.
    reportTransportFailure()
    reportTransportFailure()
    await Promise.resolve()
    expect(silent).toHaveBeenCalledTimes(1)
  })

  it('checks a silent service on growing waits and stops the moment it answers', async () => {
    vi.useFakeTimers()
    const silent = stubService(async () => answered(503))
    const { reach } = useServiceHealth()

    reportTransportFailure()
    expect(await checkService()).toBe(false)
    expect(reach.value).toBe('unreachable')
    expect(silent).toHaveBeenCalledTimes(1)

    // Two seconds, then five, then ten, then thirty from there on: an outage
    // that has lasted a minute is not over in the next second, and one that
    // lasts all afternoon is still checked twice a minute rather than
    // continuously.
    for (const [wait, calls] of [
      [2000, 2],
      [5000, 3],
      [10_000, 4],
      [30_000, 5],
      [30_000, 6],
    ] as const) {
      await vi.advanceTimersByTimeAsync(wait - 1)
      expect(silent, `${wait}ms`).toHaveBeenCalledTimes(calls - 1)
      await vi.advanceTimersByTimeAsync(1)
      expect(silent, `${wait}ms`).toHaveBeenCalledTimes(calls)
    }

    const recovered = stubService(async () => answered(200))
    expect(await checkService()).toBe(true)
    expect(reach.value).toBe('ok')

    // Nothing is scheduled against a service that answers. The surface asks
    // when something failed, never on a clock.
    await vi.advanceTimersByTimeAsync(120_000)
    expect(recovered).toHaveBeenCalledTimes(1)
  })

  it('starts the waits again from the shortest after the service comes back', async () => {
    vi.useFakeTimers()
    const silent = stubService(async () => answered(503))

    reportTransportFailure()
    await checkService()
    await vi.advanceTimersByTimeAsync(2000)
    await vi.advanceTimersByTimeAsync(5000)
    expect(silent).toHaveBeenCalledTimes(3)

    stubService(async () => answered(200))
    await checkService()

    const silentAgain = stubService(async () => answered(503))
    reportTransportFailure()
    await checkService()
    await vi.advanceTimersByTimeAsync(2000)
    expect(silentAgain).toHaveBeenCalledTimes(2)
  })
})
