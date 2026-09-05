declare global {
  interface Window {
    routevaneDesktop?: Readonly<{ backendOrigin?: string }>
  }
}

export function servingOrigin(): string {
  if (
    window.location.protocol === 'routevane:' &&
    window.location.hostname === 'app'
  ) {
    const origin = window.routevaneDesktop?.backendOrigin
    if (origin && /^http:\/\/127\.0\.0\.1:[1-9]\d{0,4}$/.test(origin))
      return origin
  }
  return window.location.origin
}
