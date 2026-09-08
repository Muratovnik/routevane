import { access, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'

import {
  assertPortBindable,
  delay,
  reserveLoopbackPort,
  spawnProduct,
  stopOwnedProduct,
  type SpawnedProduct,
} from './support/product'

const testDirectory = dirname(fileURLToPath(import.meta.url))
const repositoryRoot = resolve(testDirectory, '..', '..', '..')
const binary = join(
  repositoryRoot,
  '.cache',
  'build',
  process.platform === 'win32' ? 'routevane.exe' : 'routevane',
)
const sourceDataRoot = join(repositoryRoot, '.cache', 'transfer-source-data')
const destinationDataRoot = join(
  repositoryRoot,
  '.cache',
  'transfer-destination-data',
)
const downloadRoot = join(repositoryRoot, 'tmp', 'test-artifacts', 'transfer')
const transferPath = join(downloadRoot, 'routevane-config.json')
const invalidUTF8Path = join(downloadRoot, 'invalid-utf8.json')
const bomPath = join(downloadRoot, 'utf8-bom.json')
const topDuplicatePath = join(downloadRoot, 'duplicate-top.json')
const nestedDuplicatePath = join(downloadRoot, 'duplicate-nested.json')

let sourceOrigin = ''
let destinationOrigin = ''
let sourcePort = 0
let destinationPort = 0
let sourceProduct: SpawnedProduct | undefined
let destinationProduct: SpawnedProduct | undefined

test.use({ locale: 'en-US' })

// The window the walkthrough reads outside a width sweep, which is the desktop
// viewport this suite starts every page at. A sweep states the width it leaves
// the window at rather than restoring whichever one it happened to find.
const DESKTOP_VIEWPORT = { height: 720, width: 1280 }

test.beforeAll(async () => {
  await access(binary)
  await rm(sourceDataRoot, { force: true, recursive: true })
  await rm(destinationDataRoot, { force: true, recursive: true })
  await rm(downloadRoot, { force: true, recursive: true })
  await mkdir(downloadRoot, { recursive: true })

  sourcePort = await reserveLoopbackPort()
  do {
    destinationPort = await reserveLoopbackPort()
  } while (destinationPort === sourcePort)
  sourceOrigin = `http://127.0.0.1:${sourcePort}`
  destinationOrigin = `http://127.0.0.1:${destinationPort}`
  sourceProduct = startProduct(sourcePort, sourceDataRoot)
  destinationProduct = startProduct(destinationPort, destinationDataRoot)
  await Promise.all([
    waitForHealth(sourceOrigin, sourceProduct),
    waitForHealth(destinationOrigin, destinationProduct),
  ])
})

test.afterAll(async () => {
  const cleanupErrors: unknown[] = []
  for (const product of [sourceProduct, destinationProduct]) {
    if (product === undefined) continue
    try {
      await stopOwnedProduct(product.process)
    } catch (error) {
      cleanupErrors.push(error)
    }
  }
  for (const port of [sourcePort, destinationPort]) {
    if (port === 0) continue
    try {
      await assertPortBindable(port)
    } catch (error) {
      cleanupErrors.push(error)
    }
  }
  for (const path of [sourceDataRoot, destinationDataRoot, downloadRoot]) {
    try {
      await rm(path, { force: true, recursive: true })
    } catch (error) {
      cleanupErrors.push(error)
    }
  }
  if (cleanupErrors.length > 0) throw cleanupErrors[0]
})

test('configuration transfer moves a reviewed profile into a fresh installation', async ({
  page,
}) => {
  const mutationHeaders = {
    Origin: sourceOrigin,
    'X-Routevane-Request': '1',
  }
  const addedSource = await page.request.post(
    `${sourceOrigin}/v1/lists/youtube/sources`,
    {
      data: {
        format: 'text',
        url: 'https://secret.example.test/token/SENTINEL/feed?format=json',
      },
      headers: mutationHeaders,
    },
  )
  expect(addedSource.ok()).toBe(true)
  const customSourceID = (
    (await addedSource.json()) as { source: { id: string } }
  ).source.id
  for (const sourceID of ['dns-playback', customSourceID]) {
    const disabled = await page.request.post(
      `${sourceOrigin}/v1/lists/youtube/sources/${sourceID}/update`,
      { data: { enabled: false }, headers: mutationHeaders },
    )
    expect(disabled.ok()).toBe(true)
  }
  const created = await page.request.post(`${sourceOrigin}/v1/profiles`, {
    data: {
      categories: [],
      exclusions: [],
      name: 'Transferred profile',
      lists: ['youtube'],
    },
    headers: mutationHeaders,
  })
  expect(created.ok()).toBe(true)
  const profileID = ((await created.json()) as { profile: { id: string } })
    .profile.id
  const output = await page.request.post(
    `${sourceOrigin}/v1/profiles/${profileID}/outputs`,
    { data: { target_id: 'keenetic' }, headers: mutationHeaders },
  )
  expect(output.ok()).toBe(true)

  await page.goto(`${sourceOrigin}/settings`)
  const downloadStarted = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Download configuration' }).click()
  const download = await downloadStarted
  expect(download.suggestedFilename()).toBe('routevane-config.json')
  await download.saveAs(transferPath)

  const rawTransfer = await readFile(transferPath, 'utf8')
  expect(rawTransfer).not.toContain('SENTINEL')
  expect(rawTransfer).not.toContain('secret.example.test')
  expect(rawTransfer).not.toContain('format=json')
  expect(rawTransfer).not.toContain(customSourceID)
  const portable = JSON.parse(rawTransfer) as {
    omitted_custom_sources: number
    settings: { default_priority: string[] }
    tunings: {
      custom_sources: unknown[]
      disabled_sources: string[]
      list_ref: string
    }[]
    version: string
  }
  const youtubeTuning = portable.tunings.find(
    (tuning) => tuning.list_ref === 'youtube',
  )
  expect(portable.omitted_custom_sources).toBe(1)
  expect(portable.version).toBe('config-transfer-v1.4')
  expect(portable.settings.default_priority).toContain('youtube')
  expect(youtubeTuning?.custom_sources).toEqual([])
  expect(youtubeTuning?.disabled_sources).toEqual(['dns-playback'])

  const topDuplicate = rawTransfer.replace(
    '{',
    '{"version":"config-transfer-v1.4",',
  )
  const nestedDuplicate = rawTransfer.replace(
    '"settings":{',
    '"settings":{"refresh_interval":"off",',
  )
  expect(topDuplicate).not.toBe(rawTransfer)
  expect(nestedDuplicate).not.toBe(rawTransfer)
  await writeFile(
    invalidUTF8Path,
    new Uint8Array([
      ...Buffer.from('{"label":"', 'utf8'),
      0xff,
      ...Buffer.from('"}', 'utf8'),
    ]),
  )
  await writeFile(bomPath, `\uFEFF${rawTransfer}`, 'utf8')
  await writeFile(topDuplicatePath, topDuplicate, 'utf8')
  await writeFile(nestedDuplicatePath, nestedDuplicate, 'utf8')

  await page.goto(`${destinationOrigin}/settings`)
  for (const invalidPath of [invalidUTF8Path, bomPath]) {
    // Reload for each case so a previous invalid-file notice cannot make this
    // assertion pass if the current bytes are accidentally accepted.
    await page.goto(`${destinationOrigin}/settings`)
    await page.getByLabel('Choose a .json file').setInputFiles(invalidPath)
    await expect(
      page.getByText('The configuration could not be read.'),
    ).toBeVisible()
  }
  for (const [duplicatePath, duplicateKeyPath] of [
    [topDuplicatePath, '/version'],
    [nestedDuplicatePath, '/settings/refresh_interval'],
  ] as const) {
    await page.getByLabel('Choose a .json file').setInputFiles(duplicatePath)
    const refusedPreview = page.waitForResponse(
      (response) =>
        response.url() === `${destinationOrigin}/v1/config-transfer/preview` &&
        response.request().method() === 'POST',
    )
    await page.getByRole('button', { name: 'Preview transfer' }).click()
    const refusal = await refusedPreview
    expect(refusal.status()).toBe(422)
    await expect(refusal.json()).resolves.toMatchObject({
      code: 'config_transfer_duplicate_key',
      path: duplicateKeyPath,
    })
    await expect(
      page.getByText('The configuration could not be checked.'),
    ).toBeVisible()
  }
  await page.getByLabel('Choose a .json file').setInputFiles(transferPath)
  const validPreviewRequest = page.waitForRequest(
    (request) =>
      request.url() === `${destinationOrigin}/v1/config-transfer/preview` &&
      request.method() === 'POST',
  )
  await page.getByRole('button', { name: 'Preview transfer' }).click()
  expect((await validPreviewRequest).postData()).toBe(rawTransfer)
  const preview = page.getByRole('region', { name: 'Will be imported' })
  await expect(preview).toContainText('1 profile')
  await expect(preview).toContainText('1 connection')
  await expect(preview).toContainText(
    'Publish connections again and create new subscriptions.',
  )
  await expect(preview).toContainText(
    'Custom sources are not in the file. Add them again after transfer.',
  )

  for (const width of [320, 1440]) {
    await page.setViewportSize({ height: 900, width })
    expect(
      await page.evaluate(
        () =>
          document.documentElement.scrollWidth <=
          document.documentElement.clientWidth,
      ),
    ).toBe(true)
    const accessibility = await new AxeBuilder({ page }).analyze()
    expect(
      accessibility.violations.filter(
        (violation) =>
          violation.impact === 'serious' || violation.impact === 'critical',
      ),
    ).toEqual([])
  }
  await page.setViewportSize(DESKTOP_VIEWPORT)

  await preview.getByRole('button', { name: 'Review and apply' }).click()
  const confirmation = page.getByRole('dialog', {
    name: 'Add the reviewed configuration to this empty installation?',
  })
  await expect(confirmation).toContainText(
    'Passwords, tokens, and history will not be imported.',
  )
  const [, applyRequest] = await Promise.all([
    page.waitForURL(`${destinationOrigin}/`),
    page.waitForRequest(
      (request) =>
        request.url() === `${destinationOrigin}/v1/config-transfer/apply` &&
        request.method() === 'POST',
    ),
    confirmation.getByRole('button', { name: 'Apply transfer' }).click(),
  ])
  expect(applyRequest.postData()).toBe(rawTransfer)
  expect(applyRequest.headers()['x-routevane-transfer-digest']).toMatch(
    /^sha256:[a-f0-9]{64}$/,
  )
  await restartDestination()

  await page.goto(`${destinationOrigin}/`)
  await expect(
    page.getByRole('link', { exact: true, name: 'Transferred profile' }),
  ).toBeVisible()
  const importedCatalog = (await (
    await page.request.get(`${destinationOrigin}/v1/lists`)
  ).json()) as { default_priority: string[] }
  expect(importedCatalog.default_priority).toEqual(
    portable.settings.default_priority,
  )
  const contentsResponse = await page.request.get(
    `${destinationOrigin}/v1/lists/youtube/contents`,
  )
  expect(contentsResponse.ok()).toBe(true)
  const contents = (await contentsResponse.json()) as {
    sources: { enabled: boolean; id: string }[]
  }
  expect(contents.sources).toContainEqual(
    expect.objectContaining({
      enabled: false,
      id: 'dns-playback',
      type: 'dns',
    }),
  )
  expect(contents.sources.some((source) => source.id.startsWith('feed-'))).toBe(
    false,
  )

  sourceProduct?.assertAlive()
  destinationProduct?.assertAlive()
})

/**
 * Restarts the destination installation on the port it already held, proving
 * the port came back in between.
 *
 * The module reference is what `afterAll` stops, so it is cleared while nothing
 * is running: keeping the lifecycle here rather than in the walkthrough is what
 * lets the walkthrough state the restart as one step.
 */
const restartDestination = async (): Promise<void> => {
  if (destinationProduct === undefined)
    throw new Error('the destination installation was never started')
  await stopOwnedProduct(destinationProduct.process)
  destinationProduct = undefined
  await assertPortBindable(destinationPort)
  destinationProduct = startProduct(destinationPort, destinationDataRoot)
  await waitForHealth(destinationOrigin, destinationProduct)
}

const startProduct = (port: number, dataRoot: string): SpawnedProduct =>
  spawnProduct(
    binary,
    [
      'serve',
      '--port',
      String(port),
      '--catalog-dir',
      'testdata/expiry/browser-catalog',
      '--data-dir',
      dataRoot,
    ],
    { cwd: repositoryRoot, stdio: 'pipe', windowsHide: true },
  )

const waitForHealth = async (
  origin: string,
  product: SpawnedProduct,
): Promise<void> => {
  const deadline = Date.now() + 15_000
  while (Date.now() < deadline) {
    product.assertAlive()
    try {
      const response = await fetch(`${origin}/health`)
      if (response.ok) return
    } catch {
      // The listener is not ready yet.
    }
    await delay(50)
  }
  throw new Error(`Routevane did not become ready: ${product.output()}`)
}
