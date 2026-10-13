/**
 * Whether a failed read says nothing answered or that the service answered with
 * an error. The words a screen shows depend on it: the first sends the operator
 * to start the service, the second to repeat a request the running service
 * refused.
 */
import { afterEach, describe, expect, it, vi } from 'vitest'

import { loadCatalog } from '@/shared/api/catalog'
import { failureKind, RoutevaneAPIError } from '@/shared/api/http'

afterEach(() => vi.unstubAllGlobals())

const failedRead = async (): Promise<unknown> => {
  try {
    await loadCatalog()
  } catch (error) {
    return error
  }
  throw new Error('the read was expected to fail')
}

describe('failureKind', () => {
  it('says nothing answered when the request never reached the service', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('Failed to fetch'))),
    )
    expect(failureKind(await failedRead())).toBe('unreachable')
  })

  it('says the service answered when it replied with an error status', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ error: 'internal' }), {
            headers: { 'Content-Type': 'application/json' },
            status: 500,
          }),
        ),
      ),
    )
    const error = await failedRead()
    expect(error).toBeInstanceOf(RoutevaneAPIError)
    expect(failureKind(error)).toBe('refused')
  })

  it('says the service answered when its reply is not JSON at all', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response('<html>Bad gateway</html>', { status: 502 }),
        ),
      ),
    )
    expect(failureKind(await failedRead())).toBe('refused')
  })

  // The desktop transport answers 503 with no body when its backend never
  // replied, which is the one status line that means nothing answered.
  it('reads the desktop transport silence as nothing answered', () => {
    expect(failureKind(new RoutevaneAPIError('x', 503))).toBe('unreachable')
    expect(
      failureKind(new RoutevaneAPIError('x', 503, '', {}, { error: 'down' })),
    ).toBe('refused')
  })
})
