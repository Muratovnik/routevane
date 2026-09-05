const { contextBridge, ipcRenderer } = require('electron')

// Read-only metadata lets subscription validation keep checking the exact
// serving origin. The private API credential never enters the renderer.
const backendOrigin = process.argv
  .find((arg) => arg.startsWith('--routevane-origin='))
  ?.split('=')[1]
contextBridge.exposeInMainWorld(
  'routevaneDesktop',
  Object.freeze({
    backendOrigin,
    updates: Object.freeze({
      state: () => ipcRenderer.invoke('updates:state'),
      apply: () => ipcRenderer.invoke('updates:apply'),
      subscribe: (callback) => {
        const listener = (_event, state) => callback(state)
        ipcRenderer.on('updates:state', listener)
        return () => ipcRenderer.removeListener('updates:state', listener)
      },
    }),
  }),
)
