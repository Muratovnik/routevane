import { join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { rm } from 'node:fs/promises'

import { expect, test } from '@playwright/test'

import {
  assertPortBindable,
  delay,
  reserveLoopbackPort,
  spawnProduct,
  stopOwnedProduct,
  type SpawnedProduct,
} from './support/product'

const testDirectory = fileURLToPath(new URL('.', import.meta.url))
const webRoot = resolve(testDirectory, '..', '..')
const repositoryRoot = resolve(webRoot, '..')
const fixtureRoot = join(webRoot, 'tests', 'fixtures', 'rv-button-nuxt')
const nuxtCLI = join(webRoot, 'node_modules', 'nuxt', 'bin', 'nuxt.mjs')
const generatedRoots = [
  join(fixtureRoot, '.nuxt'),
  join(fixtureRoot, 'node_modules'),
  join(
    repositoryRoot,
    'node_modules',
    '.cache',
    'vite',
    'web',
    'tests',
    'fixtures',
    'rv-button-nuxt',
  ),
]

let harness: SpawnedProduct | undefined
let origin = ''
let port = 0

/** How long the whole cold start may take: compile, optimize, first render. */
const BOOT_TIMEOUT_MILLISECONDS = 180000
/** How long the listener has to accept a request once Nuxt holds the port. */
const LISTENER_TIMEOUT_MILLISECONDS = 30000
/** How long the first render may take, which is where a cold optimize lands. */
const RENDER_TIMEOUT_MILLISECONDS = 120000

test.beforeAll(async ({ browser }) => {
  // A cold fixture pays for its own start twice over: Nuxt compiles the app,
  // and then Vite optimizes the dependencies the client entry pulls in and
  // reloads the page in the middle of the very load that discovered them. An
  // HTTP 200 proves only that the listener is up, so the test used to absorb
  // that reload inside its own first assertion and lose it to the default
  // expect timeout. This hook owns the cold start instead: its deadline covers
  // the optimize, and it loads the app once in a page of its own, so the test
  // below opens a server that has already served this app to a browser.
  test.setTimeout(BOOT_TIMEOUT_MILLISECONDS)
  port = await reserveLoopbackPort()
  origin = `http://127.0.0.1:${port}`
  harness = spawnProduct(
    process.execPath,
    [
      nuxtCLI,
      'dev',
      fixtureRoot,
      '--host',
      '127.0.0.1',
      '--port',
      String(port),
      '--no-fork',
      '--logLevel',
      'silent',
    ],
    {
      cwd: webRoot,
      env: { ...process.env, NUXT_TELEMETRY_DISABLED: '1' },
      stdio: 'pipe',
      windowsHide: true,
    },
  )

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
      const response = await fetch(origin)
      if (response.ok) return
    } catch {
      // Nuxt has reserved the port but has not finished compiling the fixture.
    }
    await delay(100)
  }
  throw new Error(`Nuxt button harness did not start:\n${harness?.output()}`)
}

test.afterAll(async () => {
  let cleanupError: unknown
  try {
    if (harness !== undefined) await stopOwnedProduct(harness.process)
    if (port !== 0) await assertPortBindable(port)
  } catch (error) {
    cleanupError = error
  }
  for (const generatedRoot of generatedRoots) {
    try {
      await rm(generatedRoot, { force: true, recursive: true })
    } catch (error) {
      cleanupError ??= error
    }
  }
  if (cleanupError !== undefined) throw cleanupError
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
