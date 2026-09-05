export const appURL = 'routevane://app/'

export function isAppURL(value) {
  try {
    const url = new URL(value)
    return (
      url.protocol === 'routevane:' &&
      url.hostname === 'app' &&
      url.port === '' &&
      url.username === '' &&
      url.password === ''
    )
  } catch {
    return false
  }
}

export function externalURL(value) {
  try {
    const url = new URL(value)
    return ['https:', 'http:'].includes(url.protocol) &&
      !url.username &&
      !url.password
      ? url.href
      : undefined
  } catch {
    return undefined
  }
}

// The renderer receives neither a generic IPC fetch primitive nor credentials.
// Only this application's scheme is translated to the private backend origin.
export function createTransport(origin, token) {
  return async (request) => {
    if (
      !isAppURL(request.url) ||
      !['GET', 'HEAD', 'POST'].includes(request.method)
    ) {
      return new Response(null, { status: 403 })
    }
    const incomingOrigin = request.headers.get('Origin')
    if (incomingOrigin && incomingOrigin !== 'routevane://app') {
      return new Response(null, { status: 403 })
    }
    const url = new URL(request.url)
    const headers = new Headers({
      'X-Routevane-Desktop': token,
      Origin: origin,
    })
    for (const name of ['Content-Type', 'X-Routevane-Request', 'Accept']) {
      if (request.headers.has(name))
        headers.set(name, request.headers.get(name))
    }
    try {
      const response = await fetch(`${origin}${url.pathname}${url.search}`, {
        method: request.method,
        headers,
        body: request.method === 'POST' ? request.body : undefined,
        duplex: 'half',
        redirect: 'manual',
        signal: AbortSignal.any([
          request.signal,
          AbortSignal.timeout(10 * 60 * 1000),
        ]),
      })
      const resultHeaders = new Headers(response.headers)
      resultHeaders.delete('content-encoding')
      resultHeaders.delete('content-length')
      return new Response(response.body, {
        status: response.status,
        headers: resultHeaders,
      })
    } catch {
      return new Response(null, { status: 503 })
    }
  }
}
