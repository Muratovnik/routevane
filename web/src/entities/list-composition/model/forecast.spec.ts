import { flushPromises } from '@vue/test-utils'
import { effectScope } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'

import { refreshService } from '@/shared/api/catalog'
import { useCompositionForecast, forgetForecastObservations } from './forecast'

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
    services: ['alpha', 'beta'],
    categories: [],
    exclusions: [],
    serviceDomains: {},
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
            per_service: [],
            overlaps: { items: [], truncated: false },
          },
        ],
      }),
      { headers: { 'Content-Type': 'application/json' } },
    )
  forecast.request(draft, draft.services)
  expect(forecast.pending.value).toBe(true)
  await vi.advanceTimersByTimeAsync(2)
  responses[0]!(response(1))
  await flushPromises()
  expect(forecast.pending.value).toBe(false)
  forecast.request(draft, draft.services)
  await vi.advanceTimersByTimeAsync(2)
  expect(forecast.forTarget('singbox')?.projectedRules).toBe(1)
  expect(forecast.pending.value).toBe(true)
  forecast.request(draft, draft.services)
  await vi.advanceTimersByTimeAsync(2)
  responses[1]!(response(99))
  await flushPromises()
  expect(forecast.forTarget('singbox')?.projectedRules).toBe(1)
  expect(forecast.pending.value).toBe(true)
  responses[2]!(response(2))
  await flushPromises()
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
    services: ['alpha', 'beta'],
    categories: [],
    exclusions: [],
    serviceDomains: {},
  }

  forecast.request(draft, draft.services)
  await vi.advanceTimersByTimeAsync(2)
  await flushPromises()
  expect(
    fetchMock.mock.calls.filter(([input]) =>
      String(input).endsWith('/refresh'),
    ),
  ).toHaveLength(2)

  forecast.retry(draft, draft.services)
  await vi.advanceTimersByTimeAsync(2)
  await flushPromises()
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
                  per_service: [],
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
    services: ['stale-source'],
    categories: [],
    exclusions: [],
    serviceDomains: {},
  }
  try {
    forecast.request(draft, draft.services)
    await vi.advanceTimersByTimeAsync(2)
    await flushPromises()
    expect(forecast.forTarget('keenetic')?.projectedRules).toBe(1)
    expect(forecast.failure.value).toBeNull()
    expect(
      fetchMock.mock.calls.filter(([url]) => String(url).endsWith('/refresh')),
    ).toHaveLength(1)
    projected = 2
    await refreshService('stale-source')
    expect(forecast.pending.value).toBe(true)
    await vi.advanceTimersByTimeAsync(2)
    await flushPromises()
    expect(forecast.forTarget('keenetic')?.projectedRules).toBe(2)
    projected = 3
    await forecast.refresh(draft, draft.services)
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
                per_service: [{ service_id: 'alpha', rules: 1 }],
                incomplete_services: complete ? [] : ['beta'],
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
    services: ['alpha', 'beta'],
    categories: [],
    exclusions: [],
    serviceDomains: {},
  }
  forecast.request(draft, draft.services)
  await vi.advanceTimersByTimeAsync(2)
  await flushPromises()
  expect(refreshed).toEqual(['/v1/services/beta/refresh'])
  expect(forecast.forTarget('keenetic')?.projectedRules).toBe(2)
  expect(forecast.failure.value).toBeNull()
  scope.stop()
})
