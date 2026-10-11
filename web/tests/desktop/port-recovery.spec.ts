import { expect, test, type ElectronApplication } from '@playwright/test'
import { once } from 'node:events'
import { mkdir, mkdtemp, readFile, writeFile } from 'node:fs/promises'
import { connect, createServer, type AddressInfo } from 'node:net'
import { join } from 'node:path'
import { isolatedProductEnvironment } from '../e2e/support/environment'
import {
  backendReachable,
  launchPackaged,
  nativeQuestions,
  root,
} from './support/packaged'

interface NativeQuestion {
  type: string
  message: string
  detail: string
  buttons: string[]
  cancelId: number
}

interface NativeQuestions {
  questions: NativeQuestion[]
  answer: (response: number) => void
}

// The saved port is held by an application Routevane does not own. Recovery
// must leave it alone, so it answers every connection for the whole test.
const occupiedInstallation = async () => {
  const parent = join(root, 'tmp/desktop-acceptance')
  await mkdir(parent, { recursive: true })
  const directory = await mkdtemp(join(parent, 'port-recovery '))
  const env = {
    ...isolatedProductEnvironment(),
    ROUTEVANE_DESKTOP_PROFILE: join(directory, 'profile'),
    ROUTEVANE_DESKTOP_DATA: join(directory, 'data'),
  }
  const foreign = createServer((socket) => socket.end())
  foreign.listen(0, '127.0.0.1')
  await once(foreign, 'listening')
  const occupied = (foreign.address() as AddressInfo).port
  const portFile = join(env.ROUTEVANE_DESKTOP_DATA, 'desktop.port')
  await mkdir(env.ROUTEVANE_DESKTOP_DATA, { recursive: true })
  await writeFile(portFile, `${occupied}\n`)
  return { env, foreign, occupied, portFile }
}

const accepts = async (port: number): Promise<boolean> => {
  const socket = connect(port, '127.0.0.1')
  try {
    await once(socket, 'connect')
    return true
  } catch {
    return false
  } finally {
    socket.destroy()
  }
}

// The native dialog follows the system locale; English is pinned so its words
// can be asserted on any workstation.
const launch = (env: NodeJS.ProcessEnv) =>
  launchPackaged(env, { args: ['--lang=en-US'], modules: [nativeQuestions] })

const askedQuestions = (app: ElectronApplication) =>
  app.evaluate(
    () =>
      (globalThis as unknown as { nativeQuestions: NativeQuestions })
        .nativeQuestions.questions,
  )

const answer = (app: ElectronApplication, response: number) =>
  app.evaluate(
    (_electron, value) =>
      (
        globalThis as unknown as { nativeQuestions: NativeQuestions }
      ).nativeQuestions.answer(value),
    response,
  )

const expectRecoveryQuestion = async (app: ElectronApplication) => {
  await expect
    .poll(async () => (await askedQuestions(app)).length, { timeout: 20000 })
    .toBe(1)
  const [question] = await askedQuestions(app)
  expect(question).toMatchObject({
    type: 'warning',
    message: 'Routevane’s local address is unavailable.',
    buttons: ['Quit', 'Choose new port'],
    cancelId: 0,
  })
  expect(question!.detail).toContain(
    'you must update the port in every previously copied subscription address',
  )
  // Nothing is shown behind the question until the operator decides.
  expect(app.windows()).toEqual([])
}

const openWindow = async (app: ElectronApplication) => {
  const page = await app.firstWindow()
  await page.waitForURL('routevane://app/')
  await page.getByRole('heading', { level: 1 }).waitFor()
  return page.evaluate(() => window.routevaneDesktop!.backendOrigin!)
}

const stop = async (foreign: ReturnType<typeof createServer>) => {
  foreign.close()
  await once(foreign, 'close')
}

test('quitting at an unavailable saved port leaves the port and the foreign listener unchanged', async () => {
  const { env, foreign, occupied, portFile } = await occupiedInstallation()
  try {
    const app = await launch(env)
    try {
      await expectRecoveryQuestion(app)
      const closed = app.waitForEvent('close', { timeout: 20000 })
      await answer(app, 0)
      await closed
    } finally {
      await app.close().catch(() => {})
    }
    expect(await readFile(portFile, 'utf8')).toBe(`${occupied}\n`)
    expect(await accepts(occupied)).toBe(true)
  } finally {
    await stop(foreign)
  }
})

test('choosing a new port recovers startup and later starts reuse that port', async () => {
  test.setTimeout(120000)
  const { env, foreign, occupied, portFile } = await occupiedInstallation()
  try {
    const app = await launch(env)
    let origin: string
    try {
      await expectRecoveryQuestion(app)
      await answer(app, 1)
      origin = await openWindow(app)
      const recovered = Number(new URL(origin).port)
      expect(recovered).not.toBe(occupied)
      expect(await readFile(portFile, 'utf8')).toBe(`${recovered}\n`)
      expect(await backendReachable(origin)).toBe(true)
      expect(await accepts(occupied)).toBe(true)
      expect(await askedQuestions(app)).toHaveLength(1)
    } finally {
      await app.close().catch(() => {})
    }
    await expect.poll(() => backendReachable(origin)).toBe(false)

    const restarted = await launch(env)
    try {
      expect(await openWindow(restarted)).toBe(origin)
      expect(await askedQuestions(restarted)).toEqual([])
      expect(await readFile(portFile, 'utf8')).toBe(`${new URL(origin).port}\n`)
      expect(await accepts(occupied)).toBe(true)
    } finally {
      await restarted.close().catch(() => {})
    }
  } finally {
    await stop(foreign)
  }
})
