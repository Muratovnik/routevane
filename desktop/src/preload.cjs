const { contextBridge } = require('electron')

// Read-only metadata lets subscription validation keep checking the exact
// serving origin. The private API credential never enters the renderer.
const backendOrigin = process.argv
  .find((arg) => arg.startsWith('--routevane-origin='))
  ?.split('=')[1]
contextBridge.exposeInMainWorld(
  'routevaneDesktop',
  Object.freeze({ backendOrigin }),
)
