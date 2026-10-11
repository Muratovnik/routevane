import {
  _electron as electron,
  expect,
  type ElectronApplication,
  type TestInfo,
} from '@playwright/test'
import { once } from 'node:events'
import { mkdir, mkdtemp, rm } from 'node:fs/promises'
import { join, resolve } from 'node:path'
import { reserveLoopbackPort } from '../../e2e/support/product'

export const root = resolve(import.meta.dirname, '../../../..')

// Directories the running test created; a worker runs one test at a time.
const scratchOfThisTest: string[] = []

/** A new directory under tmp/desktop-acceptance owned by the running test. */
export const acceptanceScratch = async (prefix: string): Promise<string> => {
  const parent = join(root, 'tmp/desktop-acceptance')
  await mkdir(parent, { recursive: true })
  const directory = await mkdtemp(join(parent, prefix))
  scratchOfThisTest.push(directory)
  return directory
}

/**
 * Removes the directories the finished test created once it passed; a failed
 * test keeps them as evidence. Runs after the test body, whose own cleanup has
 * already closed the processes that held files in them.
 */
export const removeScratchOfPassedTest = async (
  testInfo: TestInfo,
): Promise<void> => {
  const directories = scratchOfThisTest.splice(0)
  if (testInfo.status !== 'passed') return
  for (const directory of directories)
    await rm(directory, {
      recursive: true,
      force: true,
      maxRetries: 10,
      retryDelay: 200,
    })
}

const packageDirectory = (): string => {
  if (process.platform === 'win32') return 'win-unpacked'
  if (process.platform !== 'darwin') return 'linux-unpacked'
  return process.arch === 'arm64' ? 'mac-arm64' : 'mac'
}

const executableName = (): string =>
  process.platform === 'win32' ? 'Routevane.exe' : 'Routevane'

const packageRoot = join(root, '.cache/desktop', packageDirectory())
export const executablePath =
  process.platform === 'darwin'
    ? join(packageRoot, 'Routevane.app/Contents/MacOS/Routevane')
    : join(packageRoot, executableName())

// Every packaged launch runs in the background of the operator's desktop.
const backgroundWindows = join(import.meta.dirname, 'background-windows.cjs')

/** Records native questions instead of showing them; see the module. */
export const nativeQuestions = join(import.meta.dirname, 'native-questions.cjs')

// Electron's launcher takes a complete string map, while `process.env` declares
// every value as possibly unset. A variable that is not set is not one to hand
// on, so it is dropped rather than passed as an empty string.
const defined = (env: NodeJS.ProcessEnv): Record<string, string> =>
  Object.fromEntries(
    Object.entries(env).filter(
      (entry): entry is [string, string] => entry[1] !== undefined,
    ),
  )

export const backendReachable = async (origin: string) => {
  try {
    return (
      (await fetch(`${origin}/health`, { signal: AbortSignal.timeout(500) }))
        .status === 403
    )
  } catch {
    return false
  }
}

interface InspectorMessage {
  id?: number
  method?: string
  params?: { callFrames?: { callFrameId: string }[] }
  result?: { exceptionDetails?: unknown }
}

const requireAll = (modules: string[]) =>
  modules
    .map(
      (module) =>
        `process.getBuiltinModule('node:module').createRequire(${JSON.stringify(module)})(${JSON.stringify(module)})`,
    )
    .join(';')

// A packaged application ignores `-r`, and the application can show a window or
// ask a question before a launched application accepts evaluation. The main
// process therefore starts held by the Node inspector, and the modules are
// required at the first pause where module loading works: at the latest the
// first statement of the application module, before it can do either.
const holdUntilRequired = async (inspectorPort: number, modules: string[]) => {
  let address = ''
  await expect(async () => {
    const targets = (await (
      await fetch(`http://127.0.0.1:${inspectorPort}/json/list`)
    ).json()) as { webSocketDebuggerUrl: string }[]
    address = targets[0]!.webSocketDebuggerUrl
  }).toPass({ timeout: 15000 })
  const socket = new WebSocket(address)
  await once(socket, 'open')
  let sent = 0
  const replies = new Map<number, (message: InspectorMessage) => void>()
  let paused: (callFrameId: string) => void = () => {}
  socket.addEventListener('message', ({ data }) => {
    const message = JSON.parse(String(data)) as InspectorMessage
    if (message.method === 'Debugger.paused')
      paused(message.params!.callFrames![0]!.callFrameId)
    else if (message.id !== undefined) replies.get(message.id)?.(message)
  })
  const send = (method: string, params: object = {}) =>
    new Promise<InspectorMessage>((reply) => {
      sent += 1
      replies.set(sent, reply)
      socket.send(JSON.stringify({ id: sent, method, params }))
    })
  const nextPause = () =>
    new Promise<string>((resolvePause) => {
      paused = resolvePause
    })
  try {
    await send('Debugger.enable')
    await send('Debugger.setBreakpointByUrl', {
      urlRegex: String.raw`[\\/]main\.mjs$`,
      lineNumber: 0,
    })
    let pause = nextPause()
    await send('Runtime.runIfWaitingForDebugger')
    for (;;) {
      const { result } = await send('Debugger.evaluateOnCallFrame', {
        callFrameId: await pause,
        expression: requireAll(modules),
      })
      pause = nextPause()
      await send('Debugger.resume')
      if (result?.exceptionDetails === undefined) return
    }
  } finally {
    socket.close()
  }
}

/**
 * Launches the packaged application with its windows kept in the background,
 * after requiring any further test modules into its main process.
 */
export const launchPackaged = async (
  env: NodeJS.ProcessEnv,
  { args = [], modules = [] }: { args?: string[]; modules?: string[] } = {},
) => {
  const inspectorPort = await reserveLoopbackPort()
  const launching = electron.launch({
    executablePath,
    args: [`--inspect-brk=${inspectorPort}`, ...args],
    env: defined(env),
  })
  // The launch settles only once the held process is released below.
  launching.catch(() => {})
  try {
    await holdUntilRequired(inspectorPort, [backgroundWindows, ...modules])
  } catch (error) {
    await launching.then(
      (app) => app.close(),
      () => {},
    )
    throw error
  }
  return launching
}

interface BackgroundWindows {
  focusRequests: number
  notifications: { title: string; body: string }[]
}

/** What the application asked of the foreground while it ran in the background. */
export const backgroundRequests = (app: ElectronApplication) =>
  app.evaluate(
    () =>
      (globalThis as unknown as { backgroundWindows: BackgroundWindows })
        .backgroundWindows,
  )
