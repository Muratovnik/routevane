import type { IncomingMessage } from 'node:http'

function loopbackOrigin(value: string): URL {
  if (!/^http:\/\/127\.0\.0\.1:[1-9]\d{0,4}$/.test(value)) {
    throw new Error('Development origins must be explicit IPv4 loopback ports')
  }
  return new URL(value)
}

export function createDevProxy(apiOrigin: string, uiOrigin: string) {
  loopbackOrigin(apiOrigin)
  const ui = loopbackOrigin(uiOrigin)
  return {
    '^/(?:v1(?:/|$)|health(?:$|\\?))': {
      target: apiOrigin,
      changeOrigin: true,
      ws: false,
      proxyTimeout: 35_000,
      timeout: 35_000,
      // Check the browser's authority BEFORE translating it for the backend.
      // Never turn a foreign Origin into a trusted same-origin mutation.
      bypass(request: IncomingMessage) {
        const origin = request.headers.origin
        if (
          request.headers.host !== ui.host ||
          (origin !== undefined && origin !== uiOrigin) ||
          request.headers['sec-fetch-site'] === 'cross-site'
        ) {
          return false
        }
        if (origin !== undefined) request.headers.origin = apiOrigin
      },
    },
  }
}
