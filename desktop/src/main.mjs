import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  Menu,
  nativeImage,
  Notification,
  protocol,
  powerMonitor,
  session,
  shell,
  Tray,
} from 'electron'
import { join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { mkdir } from 'node:fs/promises'
import { existsSync } from 'node:fs'
import electronUpdater from 'electron-updater'
import { createUpdates } from './updates.mjs'
import { startBackend } from './backend.mjs'
import { appURL, createTransport, externalURL, isAppURL } from './transport.mjs'

app.setName('Routevane')
app.setAppUserModelId('io.routevane.desktop')
// Overrides are explicit developer/test inputs, never inferred from cwd.
if (process.env.ROUTEVANE_DESKTOP_PROFILE) {
  app.setPath('userData', resolve(process.env.ROUTEVANE_DESKTOP_PROFILE))
}
protocol.registerSchemesAsPrivileged([
  {
    scheme: 'routevane',
    privileges: {
      standard: true,
      secure: true,
      supportFetchAPI: true,
      corsEnabled: true,
    },
  },
])

let window
let tray
let backend
let quitting = false
let stopped = false
let failed = false
let requestedExitCode = 0
let updates
let updateTimer

function ownsUpdateRequest(event) {
  return (
    event.sender === window?.webContents &&
    event.senderFrame === event.sender.mainFrame &&
    isAppURL(event.senderFrame?.url)
  )
}
ipcMain.handle('updates:state', (event, ...args) => {
  if (!ownsUpdateRequest(event) || args.length)
    throw new Error('request_rejected')
  return updates?.snapshot() ?? { status: 'disabled' }
})
ipcMain.handle('updates:apply', (event, ...args) => {
  if (!ownsUpdateRequest(event) || args.length)
    throw new Error('request_rejected')
  void updates?.apply()
})

function foreground() {
  if (!window || window.isDestroyed()) return
  if (window.isMinimized()) window.restore()
  window.show()
  window.focus()
}

function failure() {
  if (failed || quitting) return
  failed = true
  const ru = app.getLocale().startsWith('ru')
  dialog.showErrorBox(
    'Routevane',
    ru
      ? 'Не удалось запустить Routevane или связь с приложением прервалась. Проверьте, не заняты ли каталог данных или порт другим приложением, затем запустите Routevane снова.'
      : 'Routevane could not start or its backend stopped. Check whether another application is using its data folder or port, then start Routevane again.',
  )
  requestedExitCode = 1
  app.quit()
}

async function openExternal(value) {
  const url = externalURL(value)
  if (!url) return
  const ru = app.getLocale().startsWith('ru')
  const result = await dialog.showMessageBox(window, {
    type: 'question',
    title: 'Routevane',
    message: ru
      ? 'Открыть ссылку в браузере?'
      : 'Open this link in your browser?',
    detail: url,
    buttons: ru ? ['Отмена', 'Открыть'] : ['Cancel', 'Open'],
    defaultId: 0,
    cancelId: 0,
  })
  if (result.response === 1) await shell.openExternal(url)
}

async function start() {
  const here = fileURLToPath(new URL('.', import.meta.url))
  const root = resolve(here, '../..')
  const resources = app.isPackaged
    ? process.resourcesPath
    : join(root, '../.cache/build')
  const assets = app.isPackaged
    ? join(app.getAppPath(), 'assets')
    : join(root, '../.cache/desktop/assets')
  const binary = join(
    resources,
    process.platform === 'win32' ? 'routing-agent.exe' : 'routing-agent',
  )
  const catalog = app.isPackaged
    ? join(resources, 'catalog')
    : join(root, '../catalog')
  const data = process.env.ROUTEVANE_DESKTOP_DATA
    ? resolve(process.env.ROUTEVANE_DESKTOP_DATA)
    : join(app.getPath('userData'), 'data')
  await mkdir(data, { recursive: true })
  if (quitting) return
  backend = startBackend({ binary, catalog, data })
  void backend.exited.then(() => {
    if (!quitting) failure()
  })
  const origin = await backend.ready
  if (quitting) return
  const identity = await fetch(origin, {
    headers: { 'X-Routevane-Desktop': backend.token },
    signal: AbortSignal.timeout(5000),
    redirect: 'error',
  })
  await identity.body?.cancel()
  if (
    !identity.ok ||
    !/^sha256-[a-f0-9]{64}$/.test(
      identity.headers.get('X-Routevane-UI-Digest') ?? '',
    )
  ) {
    throw new Error('embedded_ui_unavailable')
  }
  const ses = session.defaultSession
  ses.protocol.handle('routevane', createTransport(origin, backend.token))
  const canWriteClipboard = (contents, permission, origin) =>
    permission === 'clipboard-sanitized-write' &&
    contents === window?.webContents &&
    isAppURL(origin)
  ses.setPermissionRequestHandler((contents, permission, callback, details) =>
    callback(canWriteClipboard(contents, permission, details.requestingUrl)),
  )
  ses.setPermissionCheckHandler(canWriteClipboard)
  ses.webRequest.onBeforeRequest((details, callback) => {
    callback({
      cancel:
        !isAppURL(details.url) &&
        !details.url.startsWith('blob:routevane://app/'),
    })
  })
  window = new BrowserWindow({
    title: 'Routevane',
    width: 1440,
    height: 960,
    show: false,
    autoHideMenuBar: true,
    icon: join(assets, 'icon.png'),
    webPreferences: {
      preload: join(here, 'preload.cjs'),
      additionalArguments: [`--routevane-origin=${origin}`],
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
      webSecurity: true,
      webviewTag: false,
    },
  })
  window.webContents.setWindowOpenHandler(({ url }) => {
    if (isAppURL(url)) void window.loadURL(url)
    else void openExternal(url).catch(() => {})
    return { action: 'deny' }
  })
  window.webContents.on('will-navigate', (event, url) => {
    if (!isAppURL(url)) {
      event.preventDefault()
      void openExternal(url).catch(() => {})
    }
  })
  window.webContents.on('will-attach-webview', (event) =>
    event.preventDefault(),
  )
  window.webContents.on('render-process-gone', () => failure())
  const ru = app.getLocale().startsWith('ru')
  const trayImage = nativeImage
    .createFromPath(
      join(
        assets,
        process.platform === 'darwin' ? 'trayTemplate.png' : 'icon.png',
      ),
    )
    .resize({ width: 20, height: 20 })
  if (process.platform === 'darwin') trayImage.setTemplateImage(true)
  tray = new Tray(trayImage)
  tray.setToolTip('Routevane')
  tray.setContextMenu(
    Menu.buildFromTemplate([
      { label: ru ? 'Открыть Routevane' : 'Open Routevane', click: foreground },
      { type: 'separator' },
      { id: 'quit', label: ru ? 'Выйти' : 'Quit', click: () => app.quit() },
    ]),
  )
  tray.on('click', foreground)
  let notified = false
  window.on('close', (event) => {
    if (quitting || !tray || tray.isDestroyed()) return
    event.preventDefault()
    window.hide()
    if (!notified && Notification.isSupported()) {
      notified = true
      new Notification({
        title: 'Routevane',
        body: ru
          ? 'Приложение продолжает работать в трее. Чтобы завершить работу, выберите «Выйти».'
          : 'Routevane is still running in the tray. Choose Quit there to stop it.',
      }).show()
    }
  })
  window.on('closed', () => {
    window = undefined
    app.quit()
  })
  Menu.setApplicationMenu(
    Menu.buildFromTemplate([
      ...(process.platform === 'darwin' ? [{ role: 'appMenu' }] : []),
      { role: 'fileMenu', submenu: [{ role: 'quit' }] },
      { role: 'editMenu' },
      {
        role: 'viewMenu',
        submenu: [
          { role: 'reload' },
          { role: 'resetZoom' },
          { role: 'zoomIn' },
          { role: 'zoomOut' },
          { role: 'togglefullscreen' },
        ],
      },
    ]),
  )
  await window.loadURL(appURL)
  foreground()
  if (
    app.isPackaged &&
    process.platform === 'win32' &&
    existsSync(join(resources, 'desktop-installed'))
  ) {
    const { autoUpdater } = electronUpdater
    updates = createUpdates({
      updater: autoUpdater,
      notify: (state) => {
        if (window && !window.isDestroyed())
          window.webContents.send('updates:state', state)
      },
      install: async () => {
        // Stop owned work BEFORE NSIS may replace the Go executable or files.
        // Normal Quit never installs a cached update without a fresh click.
        quitting = true
        await backend.stop()
        stopped = true
        autoUpdater.once('error', () => {
          // If the installer cannot start, reopen the unchanged application.
          app.relaunch()
          app.exit(1)
        })
        autoUpdater.quitAndInstall(true, true)
      },
    })
    setTimeout(() => void updates.check(), 3000).unref()
    updateTimer = setInterval(() => void updates.check(), 4 * 60 * 60 * 1000)
    updateTimer.unref()
    powerMonitor.on('resume', () => void updates.check())
  }
}

app.on('before-quit', (event) => {
  if (stopped) return
  event.preventDefault()
  if (quitting) return
  quitting = true
  updates?.close()
  clearInterval(updateTimer)
  if (window && !window.isDestroyed()) window.hide()
  if (tray && !tray.isDestroyed()) {
    const ru = app.getLocale().startsWith('ru')
    tray.setToolTip(
      ru ? 'Routevane — завершение работы' : 'Routevane — shutting down',
    )
    tray.setContextMenu(
      Menu.buildFromTemplate([
        {
          label: ru ? 'Завершаем операции…' : 'Finishing operations…',
          enabled: false,
        },
      ]),
    )
  }
  void (async () => {
    await backend?.stop()
    tray?.destroy()
    stopped = true
    app.exit(requestedExitCode)
  })()
})
app.on('window-all-closed', () => app.quit())
app.on('activate', foreground)
app.on('second-instance', foreground)
process.on('SIGINT', () => app.quit())
process.on('SIGTERM', () => app.quit())

if (!app.requestSingleInstanceLock()) {
  stopped = true
  app.quit()
} else {
  void app.whenReady().then(start).catch(failure)
}
