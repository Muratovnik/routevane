import {
  _electron as electron,
  chromium,
  expect,
  test,
  type Page,
} from '@playwright/test'
import { analyze, axeFor } from '../e2e/support/axe'
import { copyFor } from '../e2e/support/copy'
import { createServer, type Server } from 'node:http'
import { createReadStream } from 'node:fs'
import {
  access,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  stat,
  writeFile,
} from 'node:fs/promises'
import { execFile, spawn } from 'node:child_process'
import { promisify } from 'node:util'
import { randomUUID } from 'node:crypto'
import { once } from 'node:events'
import { setTimeout as delay } from 'node:timers/promises'
import { basename, dirname, isAbsolute, join, resolve } from 'node:path'
import { isolatedProductEnvironment } from '../e2e/support/environment'

const root = resolve(import.meta.dirname, '../../..')
const exec = promisify(execFile)
const quote = (value: string) => `'${value.replaceAll("'", "''")}'`
const powershell = async (script: string) => {
  const result = await exec(
    'pwsh',
    [
      '-NoLogo',
      '-NoProfile',
      '-EncodedCommand',
      Buffer.from(script, 'utf16le').toString('base64'),
    ],
    { windowsHide: true },
  )
  return result.stdout.trim()
}
const exists = async (path: string) => {
  try {
    await access(path)
    return true
  } catch {
    return false
  }
}

/** The version the running core reports to the window it serves. */
const reportedVersion = (page: Page): Promise<string> =>
  page.evaluate(async () => {
    const health = (await (await fetch('/health')).json()) as {
      version: string
    }
    return health.version
  })

/**
 * electron-updater's download cache for one fixture identity. It lives outside
 * the scratch directory, under the user's local application data, and holds a
 * full installer, so the test that chose the identity removes it.
 */
const updaterCacheFor = (fixtureID: string): string => {
  const localAppData = process.env.LOCALAPPDATA
  if (!localAppData) throw new Error('update acceptance needs LOCALAPPDATA')
  return join(localAppData, `routevane-${fixtureID}-updater`)
}

// Paths may hold characters outside the console's code page; read them as UTF-8.
const UTF8_OUTPUT =
  '[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false); '

/**
 * The Start Menu and Desktop shortcuts an installer with this fixture identity
 * owns: the template names both after the shortcut name, which a fixture build
 * sets to its identity, in the current user's known folders.
 */
const shortcutsFor = async (fixtureID: string): Promise<string[]> => {
  const folders = (
    await powershell(
      `${UTF8_OUTPUT}[Environment]::GetFolderPath('Programs'); [Environment]::GetFolderPath('Desktop')`,
    )
  )
    .split(/\r?\n/)
    .map((folder) => folder.trim())
  if (folders.length !== 2 || !folders.every((folder) => isAbsolute(folder)))
    throw new Error(`unexpected shortcut folders: ${folders.join(', ')}`)
  return folders.map((folder) => join(folder, `${fixtureID}.lnk`))
}

/** Saves a shortcut at `link` that opens `target`. */
const plantShortcut = (link: string, target: string) =>
  powershell(
    `$link = (New-Object -ComObject WScript.Shell).CreateShortcut(${quote(link)}); $link.TargetPath = ${quote(target)}; $link.Save()`,
  )

/** The executable a shortcut opens. */
const shortcutTarget = (link: string) =>
  powershell(
    `${UTF8_OUTPUT}(New-Object -ComObject WScript.Shell).CreateShortcut(${quote(link)}).TargetPath`,
  )

/** The application identity the taskbar groups a shortcut's windows under. */
const shortcutAppID = (link: string) =>
  powershell(
    `(New-Object -ComObject Shell.Application).NameSpace(${quote(dirname(link))}).ParseName(${quote(basename(link))}).ExtendedProperty('System.AppUserModel.ID')`,
  )

/** The loopback port a listening server was given by the operating system. */
const listeningPort = (server: Server): number => {
  const address = server.address()
  if (address === null || typeof address === 'string')
    throw new Error('the update fixture server has no port')
  return address.port
}

/**
 * Removes an installation this test made, if one reached the disk: the
 * installer is what put the uninstaller there, so neither the directory nor
 * the uninstaller inside it is guaranteed once a step has failed.
 */
const uninstall = async (installDir: string): Promise<void> => {
  if (!(await exists(installDir))) return
  const uninstaller = (await readdir(installDir)).find((name) =>
    /uninstall.*\.exe$/i.test(name),
  )
  if (uninstaller === undefined) return
  await exec(join(installDir, uninstaller), ['/S'], {
    windowsHide: true,
    timeout: 60000,
  })
}

/**
 * Removes a run's scratch directory once the run passed: nothing in it is left
 * to inspect, and it holds three full installers. A failed run keeps it as
 * evidence. An NSIS uninstaller can finish from a copy of itself after the one
 * the test started has exited, so the installation inside must be gone first.
 */
const removeScratchOfPassedRun = async (
  passed: boolean,
  installDir: string,
  scratch: string,
): Promise<void> => {
  if (!passed) return
  await expect.poll(() => exists(installDir), { timeout: 60000 }).toBe(false)
  await rm(scratch, {
    recursive: true,
    force: true,
    maxRetries: 10,
    retryDelay: 200,
  })
}

test('installed app rejects a damaged update, retries, restarts into the new version and preserves profiles', async () => {
  const parent = join(root, 'tmp/update-acceptance')
  await mkdir(parent, { recursive: true })
  const scratch = await mkdtemp(join(parent, 'run-'))
  const installDir = join(scratch, 'installed')
  const exe = join(installDir, 'Routevane.exe')
  const first = join(scratch, 'first')
  const middle = join(scratch, 'middle')
  const next = join(scratch, 'next')
  const env = {
    ...isolatedProductEnvironment(),
    ROUTEVANE_DESKTOP_PROFILE: join(scratch, 'profile'),
    ROUTEVANE_DESKTOP_DATA: join(scratch, 'data'),
  }
  const fixtureID = `test-${randomUUID().replaceAll('-', '')}`
  const updaterCache = updaterCacheFor(fixtureID)
  const ru = copyFor('ru')
  let corrupt = true
  let downloads = 0
  const server = createServer(async (request, response) => {
    try {
      const name = new URL(request.url!, 'http://localhost').pathname.slice(1)
      if (
        !/^(latest\.yml|Routevane-[\d.]+-x64-setup\.exe(?:\.blockmap)?)$/.test(
          name,
        )
      ) {
        response.writeHead(404).end()
        return
      }
      let release = next
      if (name.includes('0.0.1')) release = first
      else if (name.includes('0.0.2')) release = middle
      const file = join(release, name)
      const metadata = await stat(file)
      response.writeHead(200, { 'Content-Length': metadata.size })
      let firstChunk = true
      if (name.endsWith('.exe')) downloads++
      for await (const chunk of createReadStream(file, {
        highWaterMark: 1024 * 1024,
      })) {
        const body = Buffer.from(chunk)
        if (name.endsWith('.exe') && corrupt && firstChunk)
          body[0] = body[0]! ^ 255
        firstChunk = false
        if (!response.write(body)) await once(response, 'drain')
        if (name.endsWith('.exe')) await delay(20)
      }
      response.end()
    } catch {
      response.writeHead(404).end()
    }
  })
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  const feed = `http://127.0.0.1:${listeningPort(server)}/`
  const running = async (): Promise<number[]> => {
    const json = await powershell(
      `ConvertTo-Json -InputObject @(Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq ${quote(exe)} -and $_.CommandLine -notmatch ' --type=' } | ForEach-Object ProcessId)`,
    )
    return JSON.parse(json) as number[]
  }
  const stopOwned = async () => {
    await powershell(
      `Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq ${quote(exe)} -and $_.CommandLine -notmatch ' --type=' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force }`,
    )
  }
  let app: Awaited<ReturnType<typeof electron.launch>> | undefined
  let browser: Awaited<ReturnType<typeof chromium.connectOverCDP>> | undefined
  // Only links named after this run's fixture identity; set before any exists.
  let shortcuts: string[] = []
  let passed = false
  try {
    shortcuts = await shortcutsFor(fixtureID)
    for (const [version, output] of [
      ['0.0.1', first],
      ['0.0.2', middle],
      ['0.0.3', next],
    ]) {
      await exec(
        process.execPath,
        [
          'desktop/package.mjs',
          '--installer',
          '--version',
          version!,
          '--fixture-feed',
          feed,
          '--fixture-id',
          fixtureID,
          '--fixture-root',
          scratch,
          // Each installer carries a core stamped with its own version, built
          // by `tools/dev.ps1 test-update`.
          '--core-binary',
          join(root, '.cache/update-fixture', `v${version}`, 'routevane.exe'),
          '--output',
          output!,
        ],
        {
          cwd: root,
          windowsHide: true,
          timeout: 240000,
          maxBuffer: 4 * 1024 * 1024,
        },
      )
    }
    await exec(
      join(first, 'Routevane-0.0.1-x64-setup.exe'),
      ['/S', `/D=${installDir}`],
      { env, windowsHide: true, timeout: 90000 },
    )
    await expect.poll(() => exists(exe), { timeout: 30000 }).toBe(true)
    app = await electron.launch({ executablePath: exe, env })
    let page = await app.firstWindow()
    await page.getByRole('heading', { level: 1 }).waitFor()
    expect(await app.evaluate(({ app }) => app.getVersion())).toBe('0.0.1')
    expect(await reportedVersion(page)).toBe('v0.0.1')
    const created = await page.evaluate(async () => {
      localStorage.setItem('rv.locale', 'ru')
      return (
        await fetch('/v1/profiles', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-Routevane-Request': '1',
          },
          body: JSON.stringify({
            name: 'Survives update',
            lists: ['chatgpt'],
            categories: [],
            exclusions: [],
            list_domains: {},
            priority: ['chatgpt'],
          }),
        })
      ).status
    })
    expect(created).toBe(201)
    await app.close()
    app = undefined
    // The same installer also supports a manual upgrade over an existing install.
    await exec(
      join(middle, 'Routevane-0.0.2-x64-setup.exe'),
      ['/S', `/D=${installDir}`],
      {
        env,
        windowsHide: true,
        timeout: 90000,
      },
    )
    app = await electron.launch({ executablePath: exe, env })
    page = await app.firstWindow()
    expect(await app.evaluate(({ app }) => app.getVersion())).toBe('0.0.2')
    expect(await reportedVersion(page)).toBe('v0.0.2')
    await expect(
      page.getByRole('link', { name: 'Survives update', exact: true }),
    ).toBeVisible()
    await app.close()
    app = undefined
    // A fixture build creates no shortcuts of its own, and its two installs so
    // far found none to re-point: the installer must not create one either, so
    // a shortcut the operator deleted stays deleted.
    for (const link of shortcuts) expect(await exists(link)).toBe(false)
    // An earlier build, such as a development one, left both shortcuts opening
    // another executable; the update must bring them back to the installed one.
    const stale = join(scratch, 'stale', 'Routevane.exe')
    await mkdir(dirname(stale), { recursive: true })
    await writeFile(stale, '')
    for (const link of shortcuts) {
      await plantShortcut(link, stale)
      expect((await shortcutTarget(link)).toLowerCase()).toBe(
        stale.toLowerCase(),
      )
    }
    // Exercise installation without a Node inspector: a debugger holds app.quit
    // open and Playwright's Electron cleanup also kills detached descendants.
    const child = spawn(exe, ['--remote-debugging-port=0'], {
      env,
      windowsHide: true,
      stdio: ['ignore', 'ignore', 'pipe'],
    })
    const endpoint = await new Promise<string>((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error('desktop debug endpoint unavailable')),
        30000,
      )
      child.once('error', reject)
      child.stderr.on('data', (data) => {
        const match = String(data).match(
          /DevTools listening on (ws:\/\/[^\s]+)/,
        )
        if (match) {
          clearTimeout(timer)
          resolve(match[1]!)
        }
      })
    })
    browser = await chromium.connectOverCDP(endpoint)
    page =
      browser.contexts()[0]!.pages()[0] ??
      (await browser.contexts()[0]!.waitForEvent('page'))
    await page.getByRole('heading', { level: 1 }).waitFor()
    const update = page.getByRole('button', { name: 'Обновить', exact: true })
    await expect(update).toBeVisible({ timeout: 15000 })
    expect(downloads).toBe(0)
    await page.screenshot({ path: join(parent, 'available.png') })
    await page.setViewportSize({ width: 600, height: 720 })
    await page.emulateMedia({ colorScheme: 'dark' })
    await expect(update).toBeInViewport()
    await page.screenshot({ path: join(parent, 'constrained.png') })
    await page.setViewportSize({ width: 1424, height: 920 })
    await page
      .getByRole('button', { name: 'Свернуть меню', exact: true })
      .click()
    await expect(update).toBeVisible()
    await expect(
      page.getByRole('banner').getByText('Routevane', { exact: true }),
    ).toHaveCSS('opacity', '0')
    await update.focus()
    // The tooltip's role belongs to an aria-hidden node inside the panel,
    // which is how a reader hears it as the trigger's description; the panel
    // that is actually drawn carries the hook instead.
    await expect(page.getByTestId('rv-tooltip')).toContainText('0.0.3')
    const violations = (await analyze(page, axeFor(page).setLegacyMode()))
      .violations
    expect(
      violations.filter((item) =>
        ['critical', 'serious'].includes(item.impact ?? ''),
      ),
    ).toEqual([])
    await page.screenshot({ path: join(parent, 'collapsed.png') })
    await update.press('Enter')
    await expect(page.getByRole('button', { name: /^Загрузка/ })).toBeDisabled()
    await page.screenshot({ path: join(parent, 'downloading.png') })
    const retry = page.getByRole('button', {
      name: ru('update.retry'),
      exact: true,
    })
    await expect(retry).toBeVisible({ timeout: 60000 })
    expect(
      await powershell(
        `(Get-Item -LiteralPath ${quote(exe)}).VersionInfo.ProductVersion`,
      ),
    ).toMatch(/^0\.0\.2/)
    expect(
      await page.evaluate(async () => (await fetch('/health')).status),
    ).toBe(200)
    // The rejected download replaced nothing: the running core is still 0.0.2.
    expect(await reportedVersion(page)).toBe('v0.0.2')
    await page.screenshot({ path: join(parent, 'retry.png') })
    corrupt = false
    const exited = once(child, 'exit')
    await retry.click()
    await exited
    await browser.close()
    browser = undefined
    // NSIS itself must launch the new process; the test does not launch it here.
    await expect
      .poll(async () => (await running()).length, { timeout: 90000 })
      .toBe(1)
    const port = (
      await readFile(join(env.ROUTEVANE_DESKTOP_DATA, 'desktop.port'), 'utf8')
    ).trim()
    await expect
      .poll(
        async () => {
          try {
            return (await fetch(`http://127.0.0.1:${port}/health`)).status
          } catch {
            return 0
          }
        },
        { timeout: 30000 },
      )
      .toBe(403)
    expect(
      await powershell(
        `(Get-Item -LiteralPath ${quote(exe)}).VersionInfo.ProductVersion`,
      ),
    ).toMatch(/^0\.0\.3/)
    for (const link of shortcuts) {
      expect((await shortcutTarget(link)).toLowerCase()).toBe(exe.toLowerCase())
      expect(await shortcutAppID(link)).toBe(`io.routevane.${fixtureID}`)
    }
    await stopOwned()
    await expect
      .poll(async () => {
        try {
          await fetch(`http://127.0.0.1:${port}/health`)
          return false
        } catch {
          return true
        }
      })
      .toBe(true)
    // Reopen the installed replacement to inspect its live identity and UI.
    app = await electron.launch({ executablePath: exe, env })
    expect(await app.evaluate(({ app }) => app.getVersion())).toBe('0.0.3')
    const reopened = await app.firstWindow()
    await expect(
      reopened.getByRole('link', { name: 'Survives update', exact: true }),
    ).toBeVisible()
    await expect(
      reopened.getByRole('heading', { name: 'Профили', exact: true }),
    ).toBeVisible()
    await reopened.screenshot({ path: join(parent, 'updated.png') })
    // What the operator reads after the update: the core restarted by the
    // installer states the new version on the Settings page.
    await reopened
      .getByRole('link', { name: ru('shell.nav.settings'), exact: true })
      .click()
    await expect(
      reopened
        .getByRole('region', { name: ru('settings.runtime'), exact: true })
        .getByText(`${ru('settings.runtime.version')} · v0.0.3`, {
          exact: true,
        }),
    ).toBeVisible()
    await reopened.screenshot({ path: join(parent, 'updated-settings.png') })
    passed = true
  } finally {
    // The fixture uninstaller leaves shortcuts alone; these sit in the
    // operator's own folders, so they go first, whatever failed.
    for (const link of shortcuts) await rm(link, { force: true })
    await app?.close()
    await browser?.close()
    await stopOwned()
    await uninstall(installDir)
    await rm(updaterCache, { recursive: true, force: true })
    server.closeAllConnections()
    await new Promise<void>((resolve) => server.close(() => resolve()))
    await removeScratchOfPassedRun(passed, installDir, scratch)
  }
})
