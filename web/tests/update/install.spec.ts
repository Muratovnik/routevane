import { _electron as electron, chromium, expect, test } from '@playwright/test'
import { analyze, axeFor } from '../e2e/support/axe'
import { createServer, type Server } from 'node:http'
import { createReadStream } from 'node:fs'
import {
  access,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  stat,
} from 'node:fs/promises'
import { execFile, spawn } from 'node:child_process'
import { promisify } from 'node:util'
import { randomUUID } from 'node:crypto'
import { once } from 'node:events'
import { setTimeout as delay } from 'node:timers/promises'
import { join, resolve } from 'node:path'
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
  try {
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
    await expect(
      page.getByRole('link', { name: 'Survives update', exact: true }),
    ).toBeVisible()
    await app.close()
    app = undefined
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
    const retry = page.getByRole('button', { name: 'Повторить', exact: true })
    await expect(retry).toBeVisible({ timeout: 60000 })
    expect(
      await powershell(
        `(Get-Item -LiteralPath ${quote(exe)}).VersionInfo.ProductVersion`,
      ),
    ).toMatch(/^0\.0\.2/)
    expect(
      await page.evaluate(async () => (await fetch('/health')).status),
    ).toBe(200)
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
  } finally {
    await app?.close()
    await browser?.close()
    await stopOwned()
    await uninstall(installDir)
    server.closeAllConnections()
    await new Promise<void>((resolve) => server.close(() => resolve()))
  }
})
