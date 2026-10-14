import { once } from 'node:events'
import { join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  access,
  lstat,
  mkdir,
  realpath,
  rm,
  symlink,
  unlink,
  writeFile,
} from 'node:fs/promises'

import { expect, test, type Locator, type Page } from '@playwright/test'

import { contrast } from '../unit/shared/ui/support/contrast'

import {
  assertPortBindable,
  createOwnedProductData,
  delay,
  reserveLoopbackPort,
  resolvesWithin,
  spawnProduct,
  stopOwnedProduct,
  type SpawnedProduct,
} from './support/product'

const testDirectory = fileURLToPath(new URL('.', import.meta.url))
const webRoot = resolve(testDirectory, '..', '..')
const repositoryRoot = resolve(webRoot, '..')
const fixtureRoot = join(webRoot, 'tests', 'fixtures', 'rv-button-nuxt')
const nuxtCLI = join(webRoot, 'node_modules', 'nuxt', 'bin', 'nuxt.mjs')

let runRoot: string | undefined
let builder: SpawnedProduct | undefined
let harness: SpawnedProduct | undefined
let origin = ''
let port = 0
const MODULE_LINKS: string[] = []

/** How long the whole cold start may take: build and first browser render. */
const BOOT_TIMEOUT_MILLISECONDS = 180000
/** Finish the build before the hook deadline so teardown can stop a stuck child. */
const BUILD_TIMEOUT_MILLISECONDS = 120000
/** How long the built server has to accept a request. */
const LISTENER_TIMEOUT_MILLISECONDS = 30000
/** How long the first browser render may take. */
const RENDER_TIMEOUT_MILLISECONDS = 120000

test.beforeAll(async ({ browser }) => {
  // Build the same Nuxt application in an owned directory and use its explicit
  // production loopback host. The source layer keeps the existing relative
  // component imports while generated files belong to this run.
  test.setTimeout(BOOT_TIMEOUT_MILLISECONDS)
  runRoot = await createOwnedProductData(
    join(repositoryRoot, 'tmp', 'browser-fixtures'),
    'rv-button',
  )
  const modulesRoot = join(runRoot, 'node_modules')
  await mkdir(modulesRoot)
  await mkdir(join(modulesRoot, '@nuxt'))
  // The CLI resolves these packages before it reads modulesDir. Keep its
  // package links narrow so node_modules caches also belong to this run.
  for (const parts of [['nuxt'], ['@nuxt', 'kit']]) {
    const target = await realpath(join(webRoot, 'node_modules', ...parts))
    if (!(await lstat(target)).isDirectory())
      throw new Error(`Nuxt fixture package is not a directory: ${target}`)
    const link = join(modulesRoot, ...parts)
    await symlink(
      target,
      link,
      process.platform === 'win32' ? 'junction' : 'dir',
    )
    MODULE_LINKS.push(link)
    if (!(await lstat(link)).isSymbolicLink())
      throw new Error(`Nuxt fixture package link was not created: ${link}`)
  }
  await writeFile(
    join(runRoot, 'nuxt.config.mjs'),
    `export default ${JSON.stringify({
      extends: [fixtureRoot],
      rootDir: runRoot,
      workspaceDir: runRoot,
      srcDir: fixtureRoot,
      modulesDir: [join(webRoot, 'node_modules')],
      buildDir: join(runRoot, '.nuxt'),
      vite: { cacheDir: join(runRoot, '.vite') },
      nitro: {
        output: {
          dir: join(runRoot, '.output'),
          serverDir: join(runRoot, '.output', 'server'),
          publicDir: join(runRoot, '.output', 'public'),
        },
      },
    })}\n`,
    'utf8',
  )
  builder = spawnProduct(
    process.execPath,
    [nuxtCLI, 'build', runRoot, '--logLevel', 'silent'],
    {
      cwd: webRoot,
      env: {
        ...process.env,
        NUXT_TELEMETRY_DISABLED: '1',
        NITRO_PRESET: 'node-server',
      },
      stdio: 'pipe',
      windowsHide: true,
    },
  )
  const waiting = new AbortController()
  const exited = once(builder.process, 'exit', { signal: waiting.signal })
  void exited.catch(() => {})
  try {
    if (!(await resolvesWithin(exited, BUILD_TIMEOUT_MILLISECONDS)))
      throw new Error(`Nuxt button build did not finish:\n${builder.output()}`)
    if (builder.process.exitCode !== 0)
      throw new Error(`Nuxt button build failed:\n${builder.output()}`)
  } finally {
    waiting.abort()
  }

  const serverEntry = join(runRoot, '.output', 'server', 'index.mjs')
  await access(serverEntry)
  port = await reserveLoopbackPort()
  origin = `http://127.0.0.1:${port}`
  harness = spawnProduct(process.execPath, [serverEntry], {
    cwd: runRoot,
    env: {
      ...process.env,
      NODE_ENV: 'production',
      NITRO_HOST: '127.0.0.1',
      NITRO_PORT: String(port),
      NITRO_UNIX_SOCKET: '',
      NITRO_SSL_CERT: '',
      NITRO_SSL_KEY: '',
    },
    stdio: 'pipe',
    windowsHide: true,
  })

  await waitForListener()
  const warmup = await browser.newPage()
  try {
    await warmup.goto(origin)
    await expect(warmup.getByTestId('activations')).toHaveText('0', {
      timeout: RENDER_TIMEOUT_MILLISECONDS,
    })
  } finally {
    await warmup.close()
  }
})

const waitForListener = async (): Promise<void> => {
  const deadline = Date.now() + LISTENER_TIMEOUT_MILLISECONDS
  while (Date.now() < deadline) {
    harness?.assertAlive()
    try {
      const response = await fetch(origin, {
        signal: AbortSignal.timeout(Math.max(1, deadline - Date.now())),
      })
      try {
        if (response.ok) return
      } finally {
        await response.body?.cancel().catch(() => {})
      }
    } catch {
      // The owned server has not started accepting requests yet.
    }
    const remaining = deadline - Date.now()
    if (remaining > 0) await delay(Math.min(100, remaining))
  }
  throw new Error(`Nuxt button harness did not start:\n${harness?.output()}`)
}

test.afterAll(async () => {
  if (runRoot === undefined) return
  const cleanupErrors: unknown[] = []
  const children = [builder, harness]
  for (const child of children) {
    if (child === undefined || !child.isAlive()) continue
    try {
      await stopOwnedProduct(child.process)
    } catch (error) {
      cleanupErrors.push(error)
    }
  }
  try {
    if (port !== 0) await assertPortBindable(port)
  } catch (error) {
    cleanupErrors.push(error)
  }
  if (children.some((child) => child?.isAlive())) {
    cleanupErrors.push(
      new Error(
        `Nuxt button child is still running; data preserved at ${runRoot}`,
      ),
    )
  } else {
    try {
      // Remove only our directory links, before recursive cleanup can reach
      // node_modules. The installed packages are never cleanup targets.
      for (const link of MODULE_LINKS) {
        if (!(await lstat(link)).isSymbolicLink())
          throw new Error(`Nuxt fixture package link changed: ${link}`)
        await unlink(link)
      }
      await rm(runRoot, { force: true, recursive: true })
    } catch (error) {
      cleanupErrors.push(error)
    }
  }
  if (cleanupErrors.length > 0)
    throw new AggregateError(
      cleanupErrors,
      'Nuxt button cleanup was incomplete',
    )
})

test('real Nuxt UI blocks disabled native and router links', async ({
  page,
}) => {
  const attemptedPaths: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path === '/probe-href' || path === '/probe-to')
      attemptedPaths.push(path)
  })

  await page.goto(origin)
  const initialURL = page.url()
  const activations = page.getByTestId('activations')
  await expect(activations).toHaveText('0')

  for (const target of ['disabled-href', 'disabled-to']) {
    const control = page.getByTestId(target)
    await expect(control).toHaveAttribute('aria-disabled', 'true')
    await control.click({ force: true })
    await control.dispatchEvent('click')
    await page.evaluate(
      () =>
        new Promise<void>((resolve) => requestAnimationFrame(() => resolve())),
    )
    expect(page.url()).toBe(initialURL)
    await expect(activations).toHaveText('0')
  }

  expect(attemptedPaths).toEqual([])
})

// What the browser paints for one element and a token as it paints it, read the
// way the unit specs read them, but here from the real library's own markup and
// utilities, which the unit suite replaces with doubles.
const paint = (control: Locator) =>
  control.evaluate((element) => {
    const style = getComputedStyle(element)
    return {
      backgroundColor: style.backgroundColor,
      boxShadow: style.boxShadow,
      color: style.color,
      cursor: style.cursor,
      opacity: style.opacity,
    }
  })

const paintedToken = (page: Page, name: string) =>
  page.evaluate((token) => {
    const probe = document.createElement('span')
    probe.style.background = `var(${token})`
    document.body.append(probe)
    const painted = getComputedStyle(probe).backgroundColor
    probe.remove()
    return painted
  }, name)

const TRANSPARENT = 'rgba(0, 0, 0, 0)'

for (const colorScheme of ['dark', 'light'] as const) {
  // One disabled look for everything that takes input: the library lowers a
  // disabled control's opacity, and every one of these gives that up for the
  // flat look of an unavailable button — the disabled ink, no ground, the faint
  // rule, a not-allowed cursor and full opacity.
  test(`draws a disabled library control flat and legible in the ${colorScheme} theme`, async ({
    page,
  }) => {
    await page.emulateMedia({ colorScheme })
    await page.goto(origin)
    const ink = await paintedToken(page, '--rv-color-ink-disabled')
    const rule = await paintedToken(page, '--rv-color-rule')
    const canvas = await paintedToken(page, '--rv-color-canvas')
    // A field and a choice draw their edge as a ring; a button as a border.
    const controls = [
      {
        role: 'button',
        open: 'Open button',
        closed: 'Closed button',
        ring: false,
      },
      {
        role: 'textbox',
        open: 'Open field',
        closed: 'Closed field',
        ring: true,
      },
      {
        role: 'textbox',
        open: 'Open editor',
        closed: 'Closed editor',
        ring: true,
      },
      {
        role: 'combobox',
        open: 'Open select',
        closed: 'Closed select',
        ring: true,
      },
    ] as const

    for (const { closed, open, ring, role } of controls) {
      const enabled = await paint(page.getByRole(role, { name: open }))
      const disabled = await paint(page.getByRole(role, { name: closed }))
      expect(disabled.opacity, closed).toBe('1')
      expect(disabled.color, closed).toBe(ink)
      expect(disabled.backgroundColor, closed).toBe(TRANSPARENT)
      expect(disabled.cursor, closed).toBe('not-allowed')
      expect(disabled.backgroundColor, closed).not.toBe(enabled.backgroundColor)
      expect(disabled.color, closed).not.toBe(enabled.color)
      expect(contrast(disabled.color, canvas), closed).toBeGreaterThanOrEqual(3)
      expect(disabled.boxShadow.includes(rule), closed).toBe(ring)
    }
  })
}
