import { effectScope } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'

import { refreshList } from '@/shared/api/catalog'
import {
  useCompositionForecast,
  forgetForecastObservations,
} from '@/entities/profile-composition/model/forecast'

// A real browser reads a response body on a task of its own, and a fake clock
// does not drive the browser's task queue. Settling therefore yields to that
// queue first, then lets the fake clock run whatever the answer scheduled.
const yieldToBrowser = (): Promise<void> =>
  new Promise((resolve) => {
    const channel = new MessageChannel()
    channel.port1.addEventListener('message', () => resolve(), { once: true })
    channel.port1.start()
    channel.port2.postMessage(null)
  })

const settle = async (): Promise<void> => {
  await yieldToBrowser()
  await vi.advanceTimersByTimeAsync(0)
  await yieldToBrowser()
  await vi.advanceTimersByTimeAsync(0)
}

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

it('marks retained details stale and never lands a superseded response', async () => {
  vi.useFakeTimers()
  const responses: ((response: Response) => void)[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(() => new Promise<Response>((resolve) => responses.push(resolve))),
  )
  const scope = effectScope()
  const forecast = scope.run(() => useCompositionForecast(1))!
  const draft = {
    lists: ['alpha', 'beta'],
    categories: [],
    exclusions: [],
    listDomains: {},
  }
  const response = (projected: number) =>
    new Response(
      JSON.stringify({
        targets: [
          {
            target_id: 'singbox',
            maximum_rules: 0,
            projected_rules: projected,
            fits: true,
            per_list: [],
            overlaps: { items: [], truncated: false },
          },
        ],
      }),
      { headers: { 'Content-Type': 'application/json' } },
    )
  forecast.request(draft, draft.lists)
  expect(forecast.pending.value).toBe(true)
  await vi.advanceTimersByTimeAsync(2)
  responses[0]!(response(1))
  await settle()
  expect(forecast.pending.value).toBe(false)
  forecast.request(draft, draft.lists)
  await vi.advanceTimersByTimeAsync(2)
  expect(forecast.forTarget('singbox')?.projectedRules).toBe(1)
  expect(forecast.pending.value).toBe(true)
  forecast.request(draft, draft.lists)
  await vi.advanceTimersByTimeAsync(2)
  responses[1]!(response(99))
  await settle()
  expect(forecast.forTarget('singbox')?.projectedRules).toBe(1)
  expect(forecast.pending.value).toBe(true)
  responses[2]!(response(2))
  await settle()
  expect(forecast.forTarget('singbox')?.projectedRules).toBe(2)
  expect(forecast.pending.value).toBe(false)
  forecast.request(draft, [])
  expect(forecast.forecasts.value).toEqual([])
  expect(forecast.pending.value).toBe(false)
  scope.stop()
})

it('lets an explicit retry read sources again after the session guard', async () => {
  vi.useFakeTimers()
  const fetchMock = vi.fn((input: unknown) =>
    Promise.resolve(
      String(input).endsWith('/refresh')
        ? new Response(JSON.stringify({ refresh: {} }), {
            headers: { 'Content-Type': 'application/json' },
          })
        : new Response(JSON.stringify({ error: 'not observed' }), {
            status: 404,
            headers: { 'Content-Type': 'application/json' },
          }),
    ),
  )
  vi.stubGlobal('fetch', fetchMock)
  const scope = effectScope()
  const forecast = scope.run(() => useCompositionForecast(1))!
  const draft = {
    lists: ['alpha', 'beta'],
    categories: [],
    exclusions: [],
    listDomains: {},
  }

  forecast.request(draft, draft.lists)
  await vi.advanceTimersByTimeAsync(2)
  await settle()
  expect(
    fetchMock.mock.calls.filter(([input]) =>
      String(input).endsWith('/refresh'),
    ),
  ).toHaveLength(2)

  forecast.retry(draft, draft.lists)
  await vi.advanceTimersByTimeAsync(2)
  await settle()
  expect(
    fetchMock.mock.calls.filter(([input]) =>
      String(input).endsWith('/refresh'),
    ),
  ).toHaveLength(4)
  scope.stop()
})

it('refreshes stale coverage once and invalidates the result when sources change', async () => {
  vi.useFakeTimers()
  forgetForecastObservations()
  let available = false
  let projected = 1
  const fetchMock = vi.fn((input: unknown) => {
    if (String(input).endsWith('/refresh')) {
      available = true
      return Promise.resolve(new Response(JSON.stringify({ refresh: {} })))
    }
    return Promise.resolve(
      available
        ? new Response(
            JSON.stringify({
              targets: [
                {
                  target_id: 'keenetic',
                  maximum_rules: 0,
                  projected_rules: projected,
                  fits: true,
                  per_list: [],
                  overlaps: { items: [], truncated: false },
                },
              ],
            }),
          )
        : new Response(
            JSON.stringify({
              code: 'partial_coverage',
              error: 'operation failed',
            }),
            { status: 422 },
          ),
    )
  })
  vi.stubGlobal('fetch', fetchMock)
  const scope = effectScope()
  const forecast = scope.run(() => useCompositionForecast(1))!
  const draft = {
    lists: ['stale-source'],
    categories: [],
    exclusions: [],
    listDomains: {},
  }
  try {
    forecast.request(draft, draft.lists)
    await vi.advanceTimersByTimeAsync(2)
    await settle()
    expect(forecast.forTarget('keenetic')?.projectedRules).toBe(1)
    expect(forecast.failure.value).toBeNull()
    expect(
      fetchMock.mock.calls.filter(([url]) => String(url).endsWith('/refresh')),
    ).toHaveLength(1)
    projected = 2
    await refreshList('stale-source')
    expect(forecast.pending.value).toBe(true)
    await vi.advanceTimersByTimeAsync(2)
    await settle()
    expect(forecast.forTarget('keenetic')?.projectedRules).toBe(2)
    projected = 3
    await forecast.refresh(draft, draft.lists)
    expect(forecast.forTarget('keenetic')?.projectedRules).toBe(3)
    expect(
      fetchMock.mock.calls.filter(([url]) => String(url).endsWith('/refresh')),
    ).toHaveLength(3)
  } finally {
    scope.stop()
  }
})

it('retains partial facts while reading only missing lists, then recovers the full forecast', async () => {
  vi.useFakeTimers()
  forgetForecastObservations()
  let complete = false
  const refreshed: string[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn((input: unknown) => {
      const url = String(input)
      if (url.endsWith('/refresh')) {
        refreshed.push(url)
        complete = true
        return Promise.resolve(new Response(JSON.stringify({ refresh: {} })))
      }
      return Promise.resolve(
        new Response(
          JSON.stringify({
            targets: [
              {
                target_id: 'keenetic',
                maximum_rules: 100,
                projected_rules: complete ? 2 : 1,
                fits: complete,
                per_list: [{ list_id: 'alpha', rules: 1 }],
                incomplete_lists: complete ? [] : ['beta'],
              },
            ],
          }),
        ),
      )
    }),
  )
  const scope = effectScope()
  const forecast = scope.run(() => useCompositionForecast(1))!
  const draft = {
    lists: ['alpha', 'beta'],
    categories: [],
    exclusions: [],
    listDomains: {},
  }
  forecast.request(draft, draft.lists)
  await vi.advanceTimersByTimeAsync(2)
  await settle()
  expect(refreshed).toEqual(['/v1/lists/beta/refresh'])
  expect(forecast.forTarget('keenetic')?.projectedRules).toBe(2)
  expect(forecast.failure.value).toBeNull()
  scope.stop()
})
