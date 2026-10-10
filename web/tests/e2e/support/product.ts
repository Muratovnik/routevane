/**
 * Shared product-lifecycle harness for the browser e2e suites.
 *
 * Every suite spawns the same `routevane` binary against a reserved
 * loopback port and must tear it down without leaking either the process or
 * the port into the next run. This is the one place that owns that
 * lifecycle: spawning with captured diagnostics, reserving a port that is
 * proven bindable, and stopping a child with a bounded wait and a SIGKILL
 * escalation rather than an open-ended one.
 */
import { spawn, type ChildProcess, type SpawnOptions } from 'node:child_process'
import { once } from 'node:events'
import { existsSync } from 'node:fs'
import { mkdir, mkdtemp, rm } from 'node:fs/promises'
import { createServer, type Server } from 'node:net'
import { join } from 'node:path'
import { isolatedProductEnvironment } from './environment'

/** How long stopOwnedProduct waits for a signal to take effect before escalating or giving up. */
const STOP_TIMEOUT_MILLISECONDS = 5000
const READY_TIMEOUT_MILLISECONDS = 10000

export interface SpawnedProduct {
  /** The underlying child process, exposed for stopOwnedProduct and tests that need it directly. */
  readonly process: ChildProcess
  /** Combined stdout+stderr captured so far, for diagnostics when the product never became ready. */
  output(): string
  /** True while the process has not exited and did not fail to spawn. */
  isAlive(): boolean
  /** Throws, naming the captured spawn error or output, unless the process is still alive. */
  assertAlive(): void
}

/**
 * spawnProduct starts the binary and wires stdout/stderr/error capture so a
 * caller can diagnose an early exit or a spawn failure without inventing its
 * own listeners at every call site.
 */
export const spawnProduct = (
  command: string,
  args: string[],
  options: SpawnOptions,
): SpawnedProduct => {
  const child = spawn(command, args, {
    ...options,
    env: isolatedProductEnvironment(options.env),
  })
  let output = ''
  let processError: Error | undefined
  child.on('error', (error) => {
    // A failed signal also emits `error`; only a missing PID proves that
    // spawning failed. Diagnostics must not authorize deleting live data.
    processError = error
  })
  child.stdout?.on('data', (chunk: Buffer) => {
    output += chunk.toString()
  })
  child.stderr?.on('data', (chunk: Buffer) => {
    output += chunk.toString()
  })
  const isAlive = (): boolean =>
    child.pid !== undefined &&
    child.exitCode === null &&
    child.signalCode === null
  const assertAlive = (): void => {
    if (!isAlive()) {
      throw new Error(
        `Routevane browser server exited early: ${processError?.message ?? output}`,
      )
    }
  }
  return { process: child, output: () => output, isAlive, assertAlive }
}

export const delay = (milliseconds: number): Promise<void> =>
  new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds))

export const resolvesWithin = async (
  value: Promise<unknown>,
  milliseconds: number,
): Promise<boolean> =>
  new Promise((resolveWithin, reject) => {
    const timeout = setTimeout(() => resolveWithin(false), milliseconds)
    void value.then(
      () => {
        clearTimeout(timeout)
        resolveWithin(true)
      },
      (error: unknown) => {
        clearTimeout(timeout)
        reject(error)
      },
    )
  })

/** One deadline covers both response headers and the generated UI body. */
export const waitForAuthenticatedRoot = async (
  product: {
    origin: string
    expectedUIDigest: string
    assertAlive(): void
  },
  timeoutMilliseconds = READY_TIMEOUT_MILLISECONDS,
): Promise<void> => {
  const deadline = Date.now() + timeoutMilliseconds
  let lastFailure = 'listener did not accept a request'
  while (Date.now() < deadline) {
    product.assertAlive()
    try {
      const response = await fetch(`${product.origin}/`, {
        signal: AbortSignal.timeout(Math.max(1, deadline - Date.now())),
      })
      try {
        if (response.status === 200) {
          const digest = response.headers.get('x-routevane-ui-digest')
          if (digest !== product.expectedUIDigest) {
            throw new Error(
              `listener identity mismatch: got ${digest ?? 'missing'}, want ${product.expectedUIDigest}`,
            )
          }
          // Nuxt owns the optional root attributes; the element id identifies
          // the generated application for this startup handshake.
          if (!/<div\s+id="__nuxt"(?:\s|>)/.test(await response.text())) {
            throw new Error(
              'listener did not return the generated Routevane UI',
            )
          }
          return
        }
        lastFailure = `root returned ${response.status}`
      } finally {
        await response.body?.cancel().catch(() => {})
      }
    } catch (error) {
      if (
        error instanceof Error &&
        error.message.startsWith('listener identity')
      ) {
        throw error
      }
      lastFailure = error instanceof Error ? error.message : String(error)
    }
    const remaining = deadline - Date.now()
    if (remaining > 0) await delay(Math.min(25, remaining))
  }
  product.assertAlive()
  throw new Error(
    `Routevane browser server did not become ready: ${lastFailure}`,
  )
}

/**
 * stopOwnedProduct asks a child process this suite owns to exit, waits up to
 * `timeoutMilliseconds` for the exit event, and escalates to SIGKILL exactly
 * once before giving up. It never resolves while the process might still be
 * running, so a caller that awaits it can safely reuse the port the process
 * held — unlike a bare `kill()` plus an unbounded `once(child, 'exit')`,
 * which hangs forever if the first signal is ignored.
 */
export const stopOwnedProduct = async (
  child: ChildProcess,
  timeoutMilliseconds = STOP_TIMEOUT_MILLISECONDS,
): Promise<void> => {
  if (child.exitCode !== null || child.signalCode !== null)
    throw new Error('owned Routevane child exited before teardown')
  const waiting = new AbortController()
  const exited = once(child, 'exit', { signal: waiting.signal })
  // kill() can emit `error` and return false before the waiter is awaited.
  // Its rejection stays handled even when that branch throws immediately.
  void exited.catch(() => {})
  try {
    if (!child.kill())
      throw new Error('owned Routevane child could not be terminated')
    if (await resolvesWithin(exited, timeoutMilliseconds)) return
    if (!child.kill('SIGKILL'))
      throw new Error('owned Routevane child resisted termination')
    if (!(await resolvesWithin(exited, timeoutMilliseconds)))
      throw new Error('owned Routevane child did not confirm exit')
  } finally {
    // A timeout or failed kill leaves no exit/error listeners on a live child.
    waiting.abort()
  }
}

/** A retry gets a fresh directory without touching an earlier owner's data. */
export const createOwnedProductData = async (
  parent: string,
  prefix: string,
): Promise<string> => {
  await mkdir(parent, { recursive: true })
  return mkdtemp(join(parent, `${prefix}-`))
}

/** Stop only this child and remove its data only after it is known to be gone. */
export const stopOwnedProductData = async (
  spawned: SpawnedProduct,
  port: number,
  dataRoot: string,
  timeoutMilliseconds = STOP_TIMEOUT_MILLISECONDS,
): Promise<unknown> => {
  let cleanupError: unknown
  try {
    if (spawned.isAlive())
      await stopOwnedProduct(spawned.process, timeoutMilliseconds)
    await assertPortBindable(port)
  } catch (error) {
    cleanupError = error
  }
  if (spawned.isAlive()) {
    return new Error(
      `owned Routevane child is still running; data preserved at ${dataRoot}`,
      { cause: cleanupError },
    )
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

/**
 * reserveLoopbackPort hands back a port that is provably an IPv4 loopback
 * port and provably bindable again immediately after release, so the caller
 * never hands the product a port number that only looked free.
 */
export const reserveLoopbackPort = async (): Promise<number> => {
  const listener = createServer()
  await listenOnLoopback(listener, 0)
  const address = listener.address()
  if (
    address === null ||
    typeof address === 'string' ||
    address.address !== '127.0.0.1'
  ) {
    await closeServer(listener)
    throw new Error('failed to reserve an IPv4 loopback port')
  }
  const reservedPort = address.port
  await closeServer(listener)
  await assertPortBindable(reservedPort)
  return reservedPort
}

/** assertPortBindable proves a port is actually free by binding and releasing it. */
export const assertPortBindable = async (
  candidatePort: number,
): Promise<void> => {
  const listener = createServer()
  await listenOnLoopback(listener, candidatePort)
  await closeServer(listener)
}

const listenOnLoopback = async (
  listener: Server,
  candidatePort: number,
): Promise<void> => {
  await new Promise<void>((resolveListen, rejectListen) => {
    const rejectOnce = (error: Error) => {
      listener.off('listening', resolveListen)
      rejectListen(error)
    }
    listener.once('error', rejectOnce)
    listener.once('listening', () => {
      listener.off('error', rejectOnce)
      resolveListen()
    })
    listener.listen({ exclusive: true, host: '127.0.0.1', port: candidatePort })
  })
}

const closeServer = async (listener: Server): Promise<void> => {
  await new Promise<void>((resolveClose, rejectClose) => {
    listener.close((error) =>
      error === undefined ? resolveClose() : rejectClose(error),
    )
  })
}
