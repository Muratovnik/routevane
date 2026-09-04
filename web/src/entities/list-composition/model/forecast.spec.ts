import { flushPromises } from '@vue/test-utils'
import { effectScope } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'

import { useCompositionForecast } from './forecast'

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
