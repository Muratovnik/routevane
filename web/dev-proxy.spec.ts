import type { IncomingMessage } from 'node:http'

import { describe, expect, it } from 'vitest'

import { createDevProxy } from './dev-proxy'

const ui = 'http://127.0.0.1:8765'
const api = 'http://127.0.0.1:43210'

describe('development API authority', () => {
  function request(headers: IncomingMessage['headers']) {
    return { headers } as IncomingMessage
  }

  const proxy = Object.values(createDevProxy(api, ui))[0]!

  it('translates only the trusted UI origin and preserves mutation headers', () => {
    const input = request({
      host: '127.0.0.1:8765',
      origin: ui,
      'content-type': 'application/json',
      'x-routevane-request': '1',
    })
    expect(proxy.bypass(input)).toBeUndefined()
    expect(input.headers.origin).toBe(api)
    expect(input.headers['x-routevane-request']).toBe('1')
  })

  it('accepts a local read without inventing mutation authorization', () => {
    const input = request({ host: '127.0.0.1:8765' })
    expect(proxy.bypass(input)).toBeUndefined()
    expect(input.headers.origin).toBeUndefined()
    expect(input.headers['x-routevane-request']).toBeUndefined()
  })

  it.each([
    { host: 'evil.example', origin: ui },
    { host: '127.0.0.1:9000', origin: ui },
    { host: '127.0.0.1:8765', origin: 'http://evil.example' },
    { host: '127.0.0.1:8765', origin: api },
    { host: '127.0.0.1:8765', origin: 'null' },
    { host: '127.0.0.1:8765', 'sec-fetch-site': 'cross-site' },
  ])('refuses foreign authority before rewriting it: %j', (headers) => {
    const input = request({ ...headers })
    expect(proxy.bypass(input)).toBe(false)
    expect(input.headers).toEqual(headers)
  })

  it.each([
    'http://example.com:8765',
    'http://0.0.0.0:8765',
    'http://127.0.0.1:8765/path',
    'http://127.0.0.1:8765?query',
    'http://127.0.0.1:65536',
    'http://127.0.0.1:0',
    'https://127.0.0.1:8765',
  ])('rejects noncanonical configured origins: %s', (origin) => {
    expect(() => createDevProxy(origin, ui)).toThrow()
    expect(() => createDevProxy(api, origin)).toThrow()
  })
})
