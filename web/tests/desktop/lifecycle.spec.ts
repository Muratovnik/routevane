import { _electron as electron, expect, test } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { spawn } from 'node:child_process'
import { mkdtemp, mkdir, readFile } from 'node:fs/promises'
import { resolve, join } from 'node:path'
import { once } from 'node:events'
import {
  reserveLoopbackPort,
  spawnProduct,
  stopOwnedProduct,
} from '../e2e/support/product'

const root = resolve(import.meta.dirname, '../../..')

const packageDirectory = (): string => {
  if (process.platform === 'win32') return 'win-unpacked'
  if (process.platform !== 'darwin') return 'linux-unpacked'
  return process.arch === 'arm64' ? 'mac-arm64' : 'mac'
}

const executableName = (): string =>
  process.platform === 'win32' ? 'Routevane.exe' : 'Routevane'

const packageRoot = join(root, '.cache/desktop', packageDirectory())
const executablePath =
  process.platform === 'darwin'
    ? join(packageRoot, 'Routevane.app/Contents/MacOS/Routevane')
    : join(packageRoot, executableName())

const scratch = async () => {
  const parent = join(root, 'tmp/desktop-acceptance')
  await mkdir(parent, { recursive: true })
  const directory = await mkdtemp(join(parent, 'profile '))
  return {
    ...process.env,
    ROUTEVANE_DESKTOP_PROFILE: join(directory, 'profile'),
    ROUTEVANE_DESKTOP_DATA: join(directory, 'data'),
  }
}

const start = async (env: NodeJS.ProcessEnv) => {
  const app = await electron.launch({ executablePath, env })
  const page = await app.firstWindow()
  await page.waitForURL('routevane://app/')
  await page.getByRole('heading', { level: 1 }).waitFor()
  return { app, page }
}

const backendReachable = async (origin: string) => {
  try {
    return (
      (await fetch(`${origin}/health`, { signal: AbortSignal.timeout(500) }))
        .status === 403
    )
  } catch {
    return false
  }
}

test('packaged app persists data, hides to tray, reuses its instance and quits cleanly', async () => {
  const env = await scratch()
  const { app, page } = await start(env)
  try {
    const origin = await page.evaluate(
      () => window.routevaneDesktop!.backendOrigin!,
    )
    expect(await backendReachable(origin)).toBe(true)
    const result = await page.evaluate(async () => {
      const health = await fetch('/health')
      const ui = await fetch('/')
      const created = await fetch('/v1/profiles', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Routevane-Request': '1',
        },
        body: JSON.stringify({
          name: 'Desktop profile',
          lists: ['chatgpt'],
          categories: [],
          exclusions: [],
          list_domains: {},
          priority: ['chatgpt'],
        }),
      })
      localStorage.setItem('rv.locale', 'ru')
      return {
        health: await health.json(),
        digest: ui.headers.get('X-Routevane-UI-Digest'),
        created: created.status,
        node: typeof (window as unknown as { require: unknown }).require,
      }
    })
    expect(result.health).toMatchObject({ status: 'ok' })
    expect(result.digest).toMatch(/^sha256-[a-f0-9]{64}$/)
    expect(result.created).toBe(201)
    expect(result.node).toBe('undefined')
    await page.reload()
    await expect(
      page.getByRole('heading', { name: 'Профили', exact: true }),
    ).toBeVisible()
    await expect(
      page.getByRole('link', { name: 'Desktop profile', exact: true }),
    ).toBeVisible()
    // Electron cannot create axe's helper target. This application has no
    // cross-origin frames; legacy mode runs the same rules in its actual window.
    const accessibility = await new AxeBuilder({ page })
      .setLegacyMode()
      .analyze()
    expect(
      accessibility.violations.filter((item) =>
        ['serious', 'critical'].includes(item.impact ?? ''),
      ),
    ).toEqual([])
    await page.screenshot({
      path: join(root, 'tmp/desktop-acceptance/profiles.png'),
    })

    await app.evaluate(({ BrowserWindow }) =>
      BrowserWindow.getAllWindows()[0]!.close(),
    )
    expect(
      await app.evaluate(({ BrowserWindow }) =>
        BrowserWindow.getAllWindows()[0]!.isVisible(),
      ),
    ).toBe(false)
    expect(await backendReachable(origin)).toBe(true)
    const second = spawn(executablePath, [], {
      env,
      windowsHide: true,
      stdio: 'ignore',
    })
    const [code] = await once(second, 'exit')
    expect(code).toBe(0)
    await expect
      .poll(() =>
        app.evaluate(({ BrowserWindow }) =>
          BrowserWindow.getAllWindows()[0]!.isVisible(),
        ),
      )
      .toBe(true)
    expect(
      await page.evaluate(() => window.routevaneDesktop!.backendOrigin),
    ).toBe(origin)

    await app.close()
    await expect.poll(() => backendReachable(origin)).toBe(false)
    const restarted = await start(env)
    try {
      await expect(
        restarted.page.getByRole('link', {
          name: 'Desktop profile',
          exact: true,
        }),
      ).toBeVisible()
      await expect(
        restarted.page.getByRole('heading', { name: 'Профили', exact: true }),
      ).toBeVisible()
    } finally {
      await restarted.app.close()
    }
  } finally {
    await app.close()
  }
})

test('a killed shell releases its backend and data lock', async () => {
  const env = await scratch()
  const { app, page } = await start(env)
  const origin = await page.evaluate(
    () => window.routevaneDesktop!.backendOrigin!,
  )
  try {
    const mainPID = await app.evaluate(() => process.pid)
    // On Windows the packaged EXE's launcher PID can differ from Electron's
    // main PID. Kill the actual owner rather than just its launcher.
    process.kill(mainPID, 'SIGKILL')
    await expect
      .poll(() => backendReachable(origin), { timeout: 10000 })
      .toBe(false)
    const restarted = await start(env)
    try {
      await expect(
        restarted.page.getByRole('heading', { level: 1 }),
      ).toBeVisible()
    } finally {
      await restarted.app.close()
    }
  } finally {
    await app.close().catch(() => {})
  }
})

test('desktop composes and publishes, copies a usable subscription and downloads configuration', async () => {
  const env = await scratch()
  const { app, page } = await start(env)
  try {
    const status = await page.evaluate(async () => {
      localStorage.setItem('rv.locale', 'en')
      const result = await fetch('/v1/lists', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Routevane-Request': '1',
        },
        body: JSON.stringify({
          title: 'Desktop list',
          domains: ['example.com'],
        }),
      })
      return result.status
    })
    expect(status).toBe(201)
    await page.goto('routevane://app/profiles/new')
    await page
      .getByRole('row')
      .filter({ hasText: 'Desktop list' })
      .getByRole('checkbox')
      .check()
    await page.getByLabel('Where to deliver the profile').click()
    await page.getByRole('option', { name: /Keenetic.*\.txt/ }).click()
    await page
      .getByRole('button', { name: 'Create and prepare', exact: true })
      .click()
    await expect(
      page.getByRole('heading', { name: 'Desktop list', level: 1 }),
    ).toBeVisible()
    const copy = page.getByRole('button', { name: 'Copy', exact: true }).first()
    await expect(copy).toBeVisible()
    await copy.click()
    await expect
      .poll(() => app.evaluate(({ clipboard }) => clipboard.readText()))
      .toContain('/v1/subscriptions/rv1.')
    const subscription = await app.evaluate(({ clipboard }) =>
      clipboard.readText(),
    )
    const origin = await page.evaluate(
      () => window.routevaneDesktop!.backendOrigin!,
    )
    expect(subscription).toMatch(
      new RegExp(`^${origin.replaceAll('.', '\\.')}/v1/subscriptions/rv1\\.`),
    )
    expect(await (await fetch(subscription)).text()).toContain('example.com')
    expect(
      await page.evaluate(() =>
        navigator.clipboard.readText().then(
          () => 'allowed',
          () => 'denied',
        ),
      ),
    ).toBe('denied')
    await page.screenshot({
      path: join(root, 'tmp/desktop-acceptance/published.png'),
    })

    const saved = join(env.ROUTEVANE_DESKTOP_PROFILE, 'config.json')
    // Stand in for the operator accepting the native Save dialog; the UI still
    // initiates the real blob download through Electron's download manager.
    await app.evaluate(({ session }, path) => {
      session.defaultSession.once('will-download', (_event, item) =>
        item.setSavePath(path),
      )
    }, saved)
    await page.goto('routevane://app/settings')
    await page
      .getByRole('button', { name: 'Download configuration', exact: true })
      .click()
    await expect
      .poll(async () => {
        try {
          return await readFile(saved, 'utf8')
        } catch {
          return ''
        }
      })
      .toContain('Desktop list')
    await app.close()
    const reopened = await start(env)
    try {
      expect(await (await fetch(subscription)).text()).toContain('example.com')
    } finally {
      await reopened.app.close()
    }
  } finally {
    await app.close()
  }
})

test('desktop quit leaves an independently started CLI server running', async () => {
  const env = await scratch()
  const port = await reserveLoopbackPort()
  const binary = join(
    root,
    '.cache/build',
    process.platform === 'win32' ? 'routevane.exe' : 'routevane',
  )
  const cli = spawnProduct(
    binary,
    [
      'serve',
      '--port',
      String(port),
      '--catalog-dir',
      join(root, 'catalog'),
      '--data-dir',
      `${env.ROUTEVANE_DESKTOP_DATA}-cli`,
    ],
    { stdio: 'pipe', windowsHide: true },
  )
  const health = async () => {
    try {
      return (await fetch(`http://127.0.0.1:${port}/health`)).status
    } catch {
      return 0
    }
  }
  try {
    await expect.poll(health).toBe(200)
    const { app } = await start(env)
    await app.close()
    expect(cli.isAlive()).toBe(true)
    expect(await health()).toBe(200)
  } finally {
    await stopOwnedProduct(cli.process)
  }
})
