// A packaged application under test runs on the operator's own desktop. Required
// into its main process before the application module runs, this keeps every
// window off the visible screen and out of the taskbar, and turns requests that
// would take the foreground into records the test can assert instead.
const { app, BrowserWindow, Notification, screen } = require('electron')

const record = { focusRequests: 0, notifications: [] }
globalThis.backgroundWindows = record

// Left of every attached display, so no part of the window can be seen.
const offscreen = (window) => {
  const displays = screen.getAllDisplays().map(({ bounds }) => bounds)
  const left = Math.min(...displays.map(({ x }) => x))
  const top = Math.min(...displays.map(({ y }) => y))
  const { width, height } = window.getBounds()
  window.setBounds({ x: left - width - 1024, y: top, width, height })
}

// Emitted while the window is still being constructed, before it can be shown.
app.on('browser-window-created', (_event, window) => {
  window.setSkipTaskbar(true)
  offscreen(window)
})

// A window that is shown still reports itself visible; it just never activates.
BrowserWindow.prototype.show = function show() {
  this.showInactive()
}
BrowserWindow.prototype.focus = function focus() {
  record.focusRequests += 1
}

Notification.prototype.show = function show() {
  record.notifications.push({ title: this.title, body: this.body })
}
