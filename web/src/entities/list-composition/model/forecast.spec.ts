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
