/**
 * The product the browser suites drive.
 *
 * Every suite needs the same thing: the built `routevane` binary serving the
 * embedded interface on a loopback port of its own, over a data directory that
 * starts empty and is gone again afterwards. This module owns that lifecycle
 * as a worker fixture, so a suite states which data directory it wants and
 * nothing about how the product is started, proven or stopped.
 *
 * `productData` names that directory and is a worker option: Playwright runs
 * suites that ask for different worker options in workers of their own, which
 * is what keeps one suite's profiles, lists and categories out of the next
 * suite's library. A suite that names none is refused rather than quietly
 * inheriting another suite's state.
 */
import { createHash } from 'node:crypto'
import { existsSync } from 'node:fs'
import { access, readFile, readdir, rm } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { test as base } from './content-policy'
import {
  assertPortBindable,
  delay,
  reserveLoopbackPort,
  spawnProduct,
  stopOwnedProduct,
  type SpawnedProduct,
} from './product'

/** How long the interface has to answer with its own identity before the boot fails. */
const READY_TIMEOUT_MILLISECONDS = 10000

const supportDirectory = dirname(fileURLToPath(import.meta.url))
export const repositoryRoot = resolve(supportDirectory, '..', '..', '..', '..')
const embeddedUIRoot = join(
  repositoryRoot,
  'internal',
  'infrastructure',
  'httpapi',
  'ui',
)
const binary = join(
  repositoryRoot,
  '.cache',
  'build',
  process.platform === 'win32' ? 'routevane.exe' : 'routevane',
)

/** The running product a suite reads and writes through. */
export interface ServedProduct {
  /** Where the interface answers, including the port reserved for this suite. */
  readonly origin: string
  /** The reserved port, proven bindable before the product was handed it. */
  readonly port: number
  /** The digest the served interface must declare, computed from the embedded files. */
  readonly expectedUIDigest: string
  /** Throws, naming the captured output, unless the product is still running. */
  assertAlive(): void
}

interface ProductOptions {
  /** The suite's own data directory under `.cache/browser-data`. */
  productData: string
  productCatalog: string
}

interface ProductWorkerFixtures {
  product: ServedProduct
}

interface ProductFixtures {
  origin: string
  expectedUIDigest: string
  assertProductAlive: () => void
}

const startProduct = async (
  dataRoot: string,
  catalog: string,
): Promise<{ product: ServedProduct; spawned: SpawnedProduct }> => {
  await access(binary)
  const expectedUIDigest = await embeddedUIDigest()
  const port = await reserveLoopbackPort()
  const origin = `http://127.0.0.1:${port}`
  await rm(dataRoot, { force: true, recursive: true })
  const spawned = spawnProduct(
    binary,
    [
      'serve',
      '--port',
      String(port),
      '--catalog-dir',
      catalog,
      '--data-dir',
      dataRoot,
    ],
    { cwd: repositoryRoot, stdio: 'pipe', windowsHide: true },
  )
  const product: ServedProduct = {
    assertAlive: () => spawned.assertAlive(),
    expectedUIDigest,
    origin,
    port,
  }
  await waitForAuthenticatedRoot(product)
  return { product, spawned }
}

/**
 * Stops the product and takes its data with it, answering with the first
 * failure rather than throwing it: a suite reports a leaked process, a held
 * port and undeleted data as one teardown result.
 */
const stopProduct = async (
  spawned: SpawnedProduct,
  product: ServedProduct,
  dataRoot: string,
): Promise<unknown> => {
  let cleanupError: unknown
  try {
    await stopOwnedProduct(spawned.process)
    await assertPortBindable(product.port)
  } catch (error) {
    cleanupError = error
  }
  try {
    await rm(dataRoot, { force: true, recursive: true })
    if (existsSync(dataRoot))
      throw new Error('browser data cleanup did not complete')
  } catch (error) {
    cleanupError ??= error
  }
  return cleanupError
}

export const test = base.extend<
  ProductFixtures,
  ProductOptions & ProductWorkerFixtures
>({
  productData: ['', { option: true, scope: 'worker' }],
  productCatalog: [
    'testdata/expiry/browser-catalog',
    { option: true, scope: 'worker' },
  ],
  product: [
    async ({ productData, productCatalog }, use, workerInfo) => {
      if (productData === '')
        throw new Error(
          "a browser suite names its own data directory: test.use({ productData: 'suite' })",
        )
      // The suite names the directory and the worker slot makes it its own.
      // One suite normally holds one worker, but a repeated or sharded run
      // spreads the same suite over several, and two products cannot share a
      // data directory: the second finds the lock taken. A parallel index is
      // never held by two workers at once, which is exactly the guarantee the
      // directory needs.
      const dataRoot = join(
        repositoryRoot,
        '.cache',
        'browser-data',
        `${productData}-${workerInfo.parallelIndex}`,
      )
      const { product, spawned } = await startProduct(dataRoot, productCatalog)
      await use(product)
      const cleanupError = await stopProduct(spawned, product, dataRoot)
      if (cleanupError !== undefined) throw cleanupError
    },
    { scope: 'worker' },
  ],
  origin: async ({ product }, use) => {
    await use(product.origin)
  },
  expectedUIDigest: async ({ product }, use) => {
    await use(product.expectedUIDigest)
  },
  assertProductAlive: async ({ product }, use) => {
    await use(() => product.assertAlive())
  },
})

const waitForAuthenticatedRoot = async (
  product: ServedProduct,
): Promise<void> => {
  const deadline = Date.now() + READY_TIMEOUT_MILLISECONDS
  let lastFailure = 'listener did not accept a request'
  while (Date.now() < deadline) {
    product.assertAlive()
    try {
      const response = await fetch(`${product.origin}/`)
      if (response.status === 200) {
        const digest = response.headers.get('x-routevane-ui-digest')
        if (digest !== product.expectedUIDigest) {
          throw new Error(
            `listener identity mismatch: got ${digest ?? 'missing'}, want ${product.expectedUIDigest}`,
          )
        }
        // Nuxt UI adds its isolation class to the application root. The root
        // identity is the id; optional framework-owned attributes are not part
        // of Routevane's server handshake.
        if (!/<div\s+id="__nuxt"(?:\s|>)/.test(await response.text())) {
          throw new Error('listener did not return the generated Routevane UI')
        }
        return
      }
      lastFailure = `root returned ${response.status}`
    } catch (error) {
      if (
        error instanceof Error &&
        error.message.startsWith('listener identity')
      ) {
        throw error
      }
      lastFailure = error instanceof Error ? error.message : String(error)
    }
    await delay(25)
  }
  product.assertAlive()
  throw new Error(
    `Routevane browser server did not become ready: ${lastFailure}`,
  )
}

const embeddedUIDigest = async (): Promise<string> => {
  const files = await embeddedFiles(embeddedUIRoot)
  const rows: string[] = []
  for (const relativePath of files.sort()) {
    if (relativePath === 'routevane-ui.marker') continue
    const bytes = await readFile(join(embeddedUIRoot, relativePath))
    rows.push(`${sha256(bytes)}  ${relativePath}\n`)
  }
  return `sha256-${sha256(rows.join(''))}`
}

const embeddedFiles = async (root: string, prefix = ''): Promise<string[]> => {
  const files: string[] = []
  for (const entry of await readdir(join(root, prefix), {
    withFileTypes: true,
  })) {
    const relativePath = prefix === '' ? entry.name : `${prefix}/${entry.name}`
    if (entry.isDirectory()) {
      files.push(...(await embeddedFiles(root, relativePath)))
    } else if (entry.isFile()) {
      files.push(relativePath)
    } else {
      throw new Error(`embedded UI has a non-regular entry: ${relativePath}`)
    }
  }
  return files
}

const sha256 = (value: string | Uint8Array): string =>
  createHash('sha256').update(value).digest('hex')
