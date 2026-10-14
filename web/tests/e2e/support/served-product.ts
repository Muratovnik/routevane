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
import { readFile, readdir, rm } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { test as base } from './content-policy'
import {
  createOwnedProductData,
  reserveLoopbackPort,
  spawnProduct,
  stopOwnedProductData,
  waitForAuthenticatedRoot,
  type SpawnedProduct,
} from './product'
import { requireTestBuild, testBuildBinary } from './test-build'

const supportDirectory = dirname(fileURLToPath(import.meta.url))
export const repositoryRoot = resolve(supportDirectory, '..', '..', '..', '..')
const embeddedUIRoot = join(
  repositoryRoot,
  'internal',
  'infrastructure',
  'httpapi',
  'ui',
)
const binary = testBuildBinary

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
  dataPrefix: string,
  catalog: string,
): Promise<{
  product: ServedProduct
  spawned: SpawnedProduct
  dataRoot: string
}> => {
  await requireTestBuild()
  const expectedUIDigest = await embeddedUIDigest()
  const port = await reserveLoopbackPort()
  const origin = `http://127.0.0.1:${port}`
  const dataRoot = await createOwnedProductData(
    join(repositoryRoot, '.cache', 'browser-data'),
    dataPrefix,
  )
  let spawned: SpawnedProduct
  try {
    spawned = spawnProduct(
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
  } catch (error) {
    // No child was returned; this newly allocated directory has no owner.
    try {
      await rm(dataRoot, { force: true, recursive: true })
    } catch (cleanupError) {
      throw new AggregateError(
        [error, cleanupError],
        'Routevane spawn failed and cleanup was incomplete',
        { cause: cleanupError },
      )
    }
    throw error
  }
  const product: ServedProduct = {
    assertAlive: () => spawned.assertAlive(),
    expectedUIDigest,
    origin,
    port,
  }
  try {
    await waitForAuthenticatedRoot(product)
    return { product, spawned, dataRoot }
  } catch (error) {
    const cleanupError = await stopOwnedProductData(spawned, port, dataRoot)
    if (cleanupError !== undefined) {
      throw new AggregateError(
        [error, cleanupError],
        'Routevane startup failed and cleanup was incomplete',
        { cause: error },
      )
    }
    throw error
  }
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
      // The prefix identifies the suite and worker; mkdtemp gives every start
      // its own directory, including a retry whose earlier child is still live.
      const { product, spawned, dataRoot } = await startProduct(
        `${productData}-${workerInfo.parallelIndex}`,
        productCatalog,
      )
      let useFailed = false
      let useError: unknown
      try {
        await use(product)
        product.assertAlive()
      } catch (error) {
        useFailed = true
        useError = error
      }
      const cleanupError = await stopOwnedProductData(
        spawned,
        product.port,
        dataRoot,
      )
      if (useFailed && cleanupError !== undefined)
        throw new AggregateError(
          [useError, cleanupError],
          'Routevane browser scenario failed and cleanup was incomplete',
          { cause: useError },
        )
      if (useFailed) throw useError
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
