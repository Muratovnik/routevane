import { once } from 'node:events'
import { readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { createServer, type RequestListener } from 'node:http'
import { join, resolve } from 'node:path'

import { describe, expect, it, vi } from 'vitest'

import {
  createOwnedProductData,
  reserveLoopbackPort,
  resolvesWithin,
  spawnProduct,
  stopOwnedProduct,
  stopOwnedProductData,
  waitForAuthenticatedRoot,
  type SpawnedProduct,
} from '../e2e/support/product'

const UI_DIGEST = 'sha256-readiness-fixture'

const withListener = async (
  respond: RequestListener,
  check: (origin: string) => Promise<void>,
): Promise<void> => {
  const listener = createServer(respond)
  listener.listen({ host: '127.0.0.1', port: 0 })
  await once(listener, 'listening')
  try {
    const address = listener.address()
    if (address === null || typeof address === 'string') {
      throw new Error('fixture did not reserve a loopback port')
    }
    await check(`http://127.0.0.1:${address.port}`)
  } finally {
    listener.closeAllConnections()
    await new Promise<void>((resolveClose, rejectClose) => {
      listener.close((error) => {
        if (error === undefined) resolveClose()
        else rejectClose(error)
      })
    })
  }
}

// A broken readiness deadline must still let the test close its own listener.
const bounded = async (value: Promise<void>): Promise<void> => {
  let timeout: ReturnType<typeof setTimeout> | undefined
  try {
    await Promise.race([
      value,
      new Promise<never>((_, reject) => {
        timeout = setTimeout(
          () => reject(new Error('readiness exceeded the fixture deadline')),
          2000,
        )
      }),
    ])
  } finally {
    clearTimeout(timeout)
  }
}

const productAt = (origin: string) => ({
  origin,
  expectedUIDigest: UI_DIGEST,
  assertAlive: vi.fn(),
})

const withOwnedProduct = async (
  check: (fixture: {
    product: SpawnedProduct
    directory: string
    dataRoot: string
    port: number
  }) => Promise<void>,
): Promise<void> => {
  const port = await reserveLoopbackPort()
  const directory = await createOwnedProductData(
    resolve('../tmp/product-readiness'),
    'lifecycle',
  )
  const dataRoot = await createOwnedProductData(directory, 'product-0')
  const product = spawnProduct(
    process.execPath,
    ['-e', 'process.stdin.resume()'],
    { stdio: 'pipe', windowsHide: true },
  )
  try {
    await once(product.process, 'spawn')
    await check({ product, directory, dataRoot, port })
  } finally {
    // An isAlive() regression must not make fixture cleanup skip the child;
    // check its recorded termination directly before removing data.
    if (
      product.process.pid !== undefined &&
      product.process.exitCode === null &&
      product.process.signalCode === null
    ) {
      await stopOwnedProduct(product.process, 1000)
    }
    await rm(directory, { force: true, recursive: true })
  }
}

describe('browser product readiness', () => {
  it('accepts the authenticated generated UI with Nuxt root attributes', async () => {
    await withListener(
      (_, response) => {
        response.writeHead(200, { 'x-routevane-ui-digest': UI_DIGEST })
        response.end('<!doctype html><div id="__nuxt" class="isolate"></div>')
      },
      async (origin) => {
        const product = productAt(origin)
        await bounded(waitForAuthenticatedRoot(product, 1000))
        expect(product.assertAlive).toHaveBeenCalled()
      },
    )
  })

  it('rejects a foreign listener before waiting for its unfinished body', async () => {
    await withListener(
      (_, response) => {
        response.writeHead(200, { 'x-routevane-ui-digest': 'foreign-digest' })
        response.flushHeaders()
      },
      async (origin) => {
        await expect(
          bounded(waitForAuthenticatedRoot(productAt(origin), 1000)),
        ).rejects.toThrow('listener identity mismatch')
      },
    )
  })

  it.each(['headers', 'body'] as const)(
    'enforces the readiness deadline when %s never finish',
    async (stall) => {
      let requests = 0
      await withListener(
        (_, response) => {
          requests += 1
          if (stall === 'body') {
            response.writeHead(200, { 'x-routevane-ui-digest': UI_DIGEST })
            response.write('<div id="__nuxt">')
          }
        },
        async (origin) => {
          await expect(
            bounded(waitForAuthenticatedRoot(productAt(origin), 500)),
          ).rejects.toThrow('Routevane browser server did not become ready')
          expect(requests).toBeGreaterThan(0)
        },
      )
    },
  )

  it('releases the shutdown timer as soon as the owned process exits', async () => {
    vi.useFakeTimers()
    try {
      await expect(resolvesWithin(Promise.resolve(), 5000)).resolves.toBe(true)
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('browser product ownership', () => {
  it('preserves live data after a signal error and isolates a retry', async () => {
    await withOwnedProduct(async ({ product, directory, dataRoot, port }) => {
      const marker = join(dataRoot, 'owned-data.txt')
      await writeFile(marker, 'data held by the first process')
      const child = product.process
      const exitListeners = child.listenerCount('exit')
      const errorListeners = child.listenerCount('error')
      const kill = vi.spyOn(child, 'kill').mockImplementation(() => {
        child.emit('error', new Error('signal delivery refused'))
        return false
      })
      try {
        const failure = await stopOwnedProductData(product, port, dataRoot, 50)
        expect(kill).toHaveBeenCalledOnce()
        expect(String(failure)).toContain(`data preserved at ${dataRoot}`)
        expect(product.isAlive()).toBe(true)
        expect(() => product.assertAlive()).not.toThrow()
        expect(child.exitCode).toBeNull()
        expect(child.signalCode).toBeNull()
        expect(await readFile(marker, 'utf8')).toBe(
          'data held by the first process',
        )
        expect(child.listenerCount('exit')).toBe(exitListeners)
        expect(child.listenerCount('error')).toBe(errorListeners)

        // A retry has the same logical suite/worker prefix while the earlier
        // process still owns its data. Only the new directory starts empty.
        const retryRoot = await createOwnedProductData(directory, 'product-0')
        expect(retryRoot).not.toBe(dataRoot)
        expect(await readdir(retryRoot)).toEqual([])
        await writeFile(join(retryRoot, 'retry-only.txt'), 'retry')
        expect(await readFile(marker, 'utf8')).toBe(
          'data held by the first process',
        )
        expect(await readdir(dataRoot)).toEqual(['owned-data.txt'])
      } finally {
        kill.mockRestore()
      }
    })
  })

  it('removes owned data after confirming requested shutdown', async () => {
    await withOwnedProduct(async ({ product, dataRoot, port }) => {
      await writeFile(join(dataRoot, 'owned-data.txt'), 'owned data')
      await expect(
        stopOwnedProductData(product, port, dataRoot, 1000),
      ).resolves.toBeUndefined()
      expect(product.isAlive()).toBe(false)
      await expect(readdir(dataRoot)).rejects.toMatchObject({ code: 'ENOENT' })
    })
  })

  it('removes owned data when the child has already completed', async () => {
    await withOwnedProduct(async ({ product, dataRoot, port }) => {
      await writeFile(join(dataRoot, 'owned-data.txt'), 'owned data')
      const exited = once(product.process, 'exit')
      product.process.stdin!.end()
      await expect(resolvesWithin(exited, 1000)).resolves.toBe(true)
      expect(product.process.exitCode).toBe(0)
      expect(product.isAlive()).toBe(false)
      await expect(
        stopOwnedProductData(product, port, dataRoot, 1000),
      ).resolves.toBeUndefined()
      await expect(readdir(dataRoot)).rejects.toMatchObject({ code: 'ENOENT' })
    })
  })

  it('releases its exit waiter when neither signal confirms termination', async () => {
    await withOwnedProduct(async ({ product }) => {
      const child = product.process
      const exitListeners = child.listenerCount('exit')
      const errorListeners = child.listenerCount('error')
      const kill = vi.spyOn(child, 'kill').mockReturnValue(true)
      try {
        await expect(stopOwnedProduct(child, 10)).rejects.toThrow(
          'owned Routevane child did not confirm exit',
        )
        expect(kill).toHaveBeenCalledTimes(2)
        expect(kill).toHaveBeenLastCalledWith('SIGKILL')
        expect(product.isAlive()).toBe(true)
        expect(child.listenerCount('exit')).toBe(exitListeners)
        expect(child.listenerCount('error')).toBe(errorListeners)
      } finally {
        kill.mockRestore()
      }
    })
  })
})
