import { afterEach, describe, expect, it, vi } from 'vitest'

import { RoutevaneAPIError } from '@/shared/api/http'
import { loadRuntimeVersion } from '@/shared/api/runtime'

const respond = (payload: unknown) => {
  const fetchMock = vi.fn(
    async () =>
      new Response(JSON.stringify(payload), {
        headers: { 'Content-Type': 'application/json' },
      }),
  )
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('runtime version', () => {
  afterEach(() => vi.unstubAllGlobals())

  it.each(['0.2.7', 'dev'])(
    'reads the running binary version %s',
    async (version) => {
      const fetchMock = respond({ status: 'ok', version })

      await expect(loadRuntimeVersion()).resolves.toBe(version)

      expect(fetchMock).toHaveBeenCalledWith(
        '/health',
        expect.objectContaining({
          cache: 'no-store',
          method: 'GET',
          signal: expect.any(AbortSignal),
        }),
      )
    },
  )

  it.each([
    { status: 'ok' },
    { status: 'ok', version: '' },
    { status: 'ok', version: 7 },
    { status: 'starting', version: '0.2.7' },
  ])('does not invent a version for %j', async (payload) => {
    respond(payload)

    await expect(loadRuntimeVersion()).rejects.toBeInstanceOf(RoutevaneAPIError)
  })
})
