import { access, mkdir, rm } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

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
  process.platform === 'win32' ? 'routing-agent.exe' : 'routing-agent',
)
const sourceDataRoot = join(repositoryRoot, '.cache', 'transfer-source-data')
const destinationDataRoot = join(
  repositoryRoot,
  '.cache',
  'transfer-destination-data',
)
const downloadRoot = join(repositoryRoot, 'tmp', 'test-artifacts', 'transfer')
const transferPath = join(downloadRoot, 'routevane-config.json')

let sourceOrigin = ''
let destinationOrigin = ''
let sourcePort = 0
let destinationPort = 0
let sourceProduct: SpawnedProduct | undefined
let destinationProduct: SpawnedProduct | undefined

test.use({ locale: 'en-US' })

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

test('configuration transfer moves a reviewed route into a fresh installation', async ({
  page,
}) => {
  const mutationHeaders = {
    Origin: sourceOrigin,
    'X-Routevane-Request': '1',
  }
  const created = await page.request.post(`${sourceOrigin}/v1/lists`, {
    data: {
      categories: [],
      exclusions: [],
      name: 'Transferred route',
      services: ['youtube'],
    },
    headers: mutationHeaders,
  })
  expect(created.ok()).toBe(true)
  const listID = ((await created.json()) as { list: { id: string } }).list.id
  const output = await page.request.post(
    `${sourceOrigin}/v1/lists/${listID}/outputs`,
    { data: { target_id: 'keenetic' }, headers: mutationHeaders },
  )
  expect(output.ok()).toBe(true)

  await page.goto(`${sourceOrigin}/settings`)
  const downloadStarted = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Download configuration' }).click()
  const download = await downloadStarted
  expect(download.suggestedFilename()).toBe('routevane-config.json')
  await download.saveAs(transferPath)

  await page.goto(`${destinationOrigin}/settings`)
  await page.getByLabel('Choose a .json file').setInputFiles(transferPath)
  await page.getByRole('button', { name: 'Preview transfer' }).click()
  const preview = page.getByRole('region', { name: 'Will be imported' })
  await expect(preview).toContainText('1 route')
  await expect(preview).toContainText('1 connection')
  await expect(preview).toContainText(
    'Publish connections again and create new subscriptions.',
  )

  await preview.getByRole('button', { name: 'Review and apply' }).click()
  const confirmation = page.getByRole('dialog', {
    name: 'Add the reviewed configuration to this empty installation?',
  })
  await expect(confirmation).toContainText(
    'Passwords, tokens, and history will not be imported.',
  )
  await Promise.all([
    page.waitForURL(`${destinationOrigin}/`),
    confirmation.getByRole('button', { name: 'Apply transfer' }).click(),
  ])
  await expect(
    page.getByRole('link', { exact: true, name: 'Transferred route' }),
  ).toBeVisible()

  sourceProduct?.assertAlive()
  destinationProduct?.assertAlive()
})

function startProduct(port: number, dataRoot: string): SpawnedProduct {
  return spawnProduct(
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
}

async function waitForHealth(
  origin: string,
  product: SpawnedProduct,
): Promise<void> {
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
