import { expect, test } from '@playwright/test'
import { analyze, axeFor } from '../e2e/support/axe'
import { spawn } from 'node:child_process'
import { copyFile, readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { once } from 'node:events'
import { isolatedProductEnvironment } from '../e2e/support/environment'
import {
  reserveLoopbackPort,
  spawnProduct,
  stopOwnedProduct,
} from '../e2e/support/product'
import {
  acceptanceScratch,
  backendReachable,
  backgroundRequests,
  executablePath,
  launchPackaged,
  removeScratchOfPassedTest,
  root,
} from './support/packaged'

test.afterEach(() => removeScratchOfPassedTest(test.info()))

const scratch = async () => {
  const directory = await acceptanceScratch('profile ')
  return {
    ...isolatedProductEnvironment(),
    ROUTEVANE_DESKTOP_PROFILE: join(directory, 'profile'),
    ROUTEVANE_DESKTOP_DATA: join(directory, 'data'),
  }
}

const start = async (env: NodeJS.ProcessEnv) => {
  const app = await launchPackaged(env)
  const page = await app.firstWindow()
  await page.waitForURL('routevane://app/')
  await page.getByRole('heading', { level: 1 }).waitFor()
  return { app, page }
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
    const accessibility = await analyze(page, axeFor(page).setLegacyMode())
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
    // The tray notice is recorded rather than shown on the operator's desktop.
    const hidden = await backgroundRequests(app)
    const notices = await app.evaluate(({ Notification }) =>
      Notification.isSupported(),
    )
    expect(hidden.notifications).toEqual(
      notices
        ? [{ title: 'Routevane', body: expect.stringMatching(/tray|трее/) }]
        : [],
    )
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
    // The reused instance asks for the foreground, which the test records
    // instead of granting so the window never takes focus from the operator.
    expect((await backgroundRequests(app)).focusRequests).toBeGreaterThan(
      hidden.focusRequests,
    )
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

test('desktop publishes and transfers a reviewed configuration to a fresh installation', async () => {
  test.setTimeout(120000)
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
    const origin = await page.evaluate(
      () => window.routevaneDesktop!.backendOrigin!,
    )
    await copy.click()
    // The system clipboard can still hold an address from an earlier run, so
    // wait for this backend's address rather than any subscription address.
    await expect
      .poll(() => app.evaluate(({ clipboard }) => clipboard.readText()))
      .toMatch(
        new RegExp(`^${origin.replaceAll('.', '\\.')}/v1/subscriptions/rv1\\.`),
      )
    const subscription = await app.evaluate(({ clipboard }) =>
      clipboard.readText(),
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

    await page.getByRole('tab', { name: 'Publishing', exact: true }).click()
    await expect(page).toHaveURL(/#tab=outputs$/)
    const prefixURL = page.url()
    await page
      .getByLabel('FQDN group prefix', { exact: true })
      .fill('desktop-home')
    await page.getByRole('button', { name: 'Save prefix', exact: true }).click()
    await expect(
      page.getByText('Prefix saved for the next build', { exact: true }),
    ).toBeVisible()
    await page.reload()
    await expect(
      page.getByLabel('FQDN group prefix', { exact: true }),
    ).toHaveValue('desktop-home')

    const saved = join(env.ROUTEVANE_DESKTOP_PROFILE, 'config.json')
    // Stand in for the operator accepting the native Save dialog; the UI still
    // initiates the real blob download through Electron's download manager.
    // The Electron typings live with the desktop package, so the main-process
    // handle this callback receives is described by what it is asked to do.
    const downloaded = await app.evaluateHandle(
      ({ session }, path) => ({
        done: new Promise<string>((completed) => {
          session.defaultSession.once(
            'will-download',
            (
              _event: unknown,
              item: {
                setSavePath: (target: string) => void
                once: (
                  event: 'done',
                  listener: (_event: unknown, state: string) => void,
                ) => void
              },
            ) => {
              item.setSavePath(path)
              item.once('done', (_event, state) => completed(state))
            },
          )
        }),
      }),
      saved,
    )
    await page.goto('routevane://app/settings')
    await expect(page.getByText(origin, { exact: true })).toBeVisible()
    const runtimeVersion = await page.evaluate(async () => {
      const response = await fetch('/health')
      const runtime = (await response.json()) as { version: string }
      return runtime.version
    })
    expect(runtimeVersion).not.toBe('')
    await expect(
      page
        .getByRole('paragraph')
        .filter({ hasText: `Version · ${runtimeVersion}` }),
    ).toBeVisible()
    await page.screenshot({
      path: join(root, 'tmp/desktop-acceptance/settings.png'),
    })
    await page
      .getByRole('button', { name: 'Download configuration', exact: true })
      .click()
    expect(await downloaded.evaluate(({ done }) => done)).toBe('completed')
    await downloaded.dispose()
    // Keep a completed transfer fixture after its download context closes.
    const transfer = join(env.ROUTEVANE_DESKTOP_PROFILE, 'transfer.json')
    await copyFile(saved, transfer)
    expect(await readFile(transfer, 'utf8')).toContain('Desktop list')
    await app.close()
    const reopened = await start(env)
    try {
      expect(await (await fetch(subscription)).text()).toContain('example.com')
      await reopened.page.goto(prefixURL)
      await expect(
        reopened.page.getByLabel('FQDN group prefix', { exact: true }),
      ).toHaveValue('desktop-home')
    } finally {
      await reopened.app.close()
    }

    const destination = await start(await scratch())
    try {
      await destination.page.evaluate(() =>
        localStorage.setItem('rv.locale', 'en'),
      )
      await destination.page.goto('routevane://app/settings')
      await destination.page
        .getByLabel('Choose a .json file')
        .setInputFiles(transfer)
      await destination.page
        .getByRole('button', { name: 'Preview transfer' })
        .click()
      const preview = destination.page.getByRole('region', {
        name: 'Will be imported',
      })
      await expect(preview).toContainText('1 profile')
      await preview.getByRole('button', { name: 'Review and apply' }).click()
      const confirmation = destination.page.getByRole('dialog', {
        name: 'Add the reviewed configuration to this empty installation?',
      })
      await confirmation.getByRole('button', { name: 'Apply transfer' }).click()
      await expect(destination.page).toHaveURL('routevane://app/')
      await expect(
        destination.page.getByRole('link', {
          name: 'Desktop list',
          exact: true,
        }),
      ).toBeVisible()
      await destination.page.reload()
      await expect(
        destination.page.getByRole('link', {
          name: 'Desktop list',
          exact: true,
        }),
      ).toBeVisible()
    } finally {
      await destination.app.close()
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
