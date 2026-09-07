import type { IncomingMessage } from 'node:http'

import { describe, expect, it } from 'vitest'

import { createDevProxy } from '../../dev-proxy'

const UI = 'http://127.0.0.1:8765'
const API = 'http://127.0.0.1:43210'

describe('development API authority', () => {
  const request = (headers: IncomingMessage['headers']) =>
    ({ headers }) as IncomingMessage

  const proxy = Object.values(createDevProxy(API, UI))[0]!

  it('translates only the trusted UI origin and preserves mutation headers', () => {
    const input = request({
      host: '127.0.0.1:8765',
      origin: UI,
      'content-type': 'application/json',
      'x-routevane-request': '1',
    })
    expect(proxy.bypass(input)).toBeUndefined()
    expect(input.headers.origin).toBe(API)
    expect(input.headers['x-routevane-request']).toBe('1')
  })

  it('accepts a local read without inventing mutation authorization', () => {
    const input = request({ host: '127.0.0.1:8765' })
    expect(proxy.bypass(input)).toBeUndefined()
    expect(input.headers.origin).toBeUndefined()
    expect(input.headers['x-routevane-request']).toBeUndefined()
  })

  it.each([
    { host: 'evil.example', origin: UI },
    { host: '127.0.0.1:9000', origin: UI },
    { host: '127.0.0.1:8765', origin: 'http://evil.example' },
    { host: '127.0.0.1:8765', origin: API },
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
    expect(() => createDevProxy(origin, UI)).toThrow()
    expect(() => createDevProxy(API, origin)).toThrow()
  })
})
