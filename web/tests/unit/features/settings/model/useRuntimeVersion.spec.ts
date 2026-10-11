import { afterEach, describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'

import { useRuntimeVersion } from '@/features/settings/model/useRuntimeVersion'

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

// The composable belongs to a screen, so each case owns the scope that screen
// would, and ends it the way leaving the screen does.
const mount = () => {
  const scope = effectScope()
  const runtime = scope.run(() => useRuntimeVersion())!
  return { runtime, scope }
}

describe('useRuntimeVersion', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('states the version the service reports', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(json({ status: 'ok', version: 'v0.0.3' })),
    )
    vi.stubGlobal('fetch', fetchMock)
    const { runtime, scope } = mount()

    await runtime.load()

    expect(runtime.version.value).toBe('v0.0.3')
    expect(fetchMock).toHaveBeenCalledExactlyOnceWith(
      '/health',
      expect.objectContaining({ method: 'GET' }),
    )
    scope.stop()
  })

  it.each([
    ['a refusal', () => json({ error: 'unavailable' }, 503)],
    ['an older reply without a version', () => json({ status: 'ok' })],
  ])('leaves the version unknown after %s', async (_, reply) => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(reply())),
    )
    const { runtime, scope } = mount()

    await runtime.load()

    expect(runtime.version.value).toBeNull()
    scope.stop()
  })

  it('writes nothing once the screen that asked has gone', async () => {
    const reply = Promise.withResolvers<Response>()
    vi.stubGlobal(
      'fetch',
      vi.fn(() => reply.promise),
    )
    const { runtime, scope } = mount()

    const loading = runtime.load()
    scope.stop()
    reply.resolve(json({ status: 'ok', version: 'v0.0.3' }))
    await loading

    expect(runtime.version.value).toBeNull()
  })
})
