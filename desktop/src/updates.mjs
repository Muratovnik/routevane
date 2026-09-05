// electron-updater owns release selection, download caching and hash/signature
// verification. This controller owns the operator's one-click install intent.
export function createUpdates({ updater, install, notify = () => {} }) {
  let state = { status: 'idle' }
  let checking
  let running = false
  let closed = false
  updater.autoDownload = false
  updater.autoInstallOnAppQuit = false
  updater.allowPrerelease = false
  updater.allowDowngrade = false
  updater.disableWebInstaller = true
  updater.logger = null
  function set(next) {
    if (closed) return
    state = Object.freeze(next)
    notify(state)
  }
  updater.on('update-available', (info) => {
    if (!running) set({ status: 'available', version: info.version })
  })
  updater.on('update-not-available', () => {
    if (!running) set({ status: 'idle' })
  })
  updater.on('download-progress', ({ percent }) => {
    if (running && state.status === 'downloading') {
      set({
        ...state,
        percent: Math.max(0, Math.min(100, Math.floor(percent || 0))),
      })
    }
  })
  // The library also rejects the operation promise. Consume its error event;
  // never expose remote error text, paths or release HTML to the renderer.
  updater.on('error', () => {})
  return {
    snapshot: () => state,
    async check() {
      if (closed || running || checking) return
      checking = updater.checkForUpdates().catch(() => {
        // Offline checks stay quiet. A previously discovered update stays usable.
      })
      await checking
      checking = undefined
    },
    async apply() {
      if (closed || running || !['available', 'error'].includes(state.status))
        return
      running = true
      const version = state.version
      set({ status: 'downloading', version, percent: 0 })
      try {
        if (checking) await checking
        if (closed) return
        await updater.downloadUpdate()
        if (closed) return
        set({ status: 'installing', version })
        await install()
      } catch {
        set({ status: 'error', version })
      } finally {
        running = false
      }
    },
    close() {
      closed = true
    },
  }
}
