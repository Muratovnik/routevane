export type DesktopUpdateState = {
  status:
    'disabled' | 'idle' | 'available' | 'downloading' | 'installing' | 'error'
  version?: string
  percent?: number
}

declare global {
  interface Window {
    routevaneDesktop?: Readonly<{
      backendOrigin?: string
      updates?: {
        state: () => Promise<DesktopUpdateState>
        apply: () => Promise<void>
        subscribe: (callback: (state: DesktopUpdateState) => void) => () => void
      }
    }>
  }
}

export const servingOrigin = (): string => {
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
