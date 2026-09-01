/**
 * Shared product-lifecycle harness for the browser e2e suites.
 *
 * Every suite spawns the same `routing-agent` binary against a reserved
 * loopback port and must tear it down without leaking either the process or
 * the port into the next run. This is the one place that owns that
 * lifecycle: spawning with captured diagnostics, reserving a port that is
 * proven bindable, and stopping a child with a bounded wait and a SIGKILL
 * escalation rather than an open-ended one.
 */
import { spawn, type ChildProcess, type SpawnOptions } from 'node:child_process'
import { once } from 'node:events'
import { createServer, type Server } from 'node:net'

/** How long stopOwnedProduct waits for a signal to take effect before escalating or giving up. */
const STOP_TIMEOUT_MILLISECONDS = 5000

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
export function spawnProduct(
  command: string,
  args: string[],
  options: SpawnOptions,
): SpawnedProduct {
  const child = spawn(command, args, options)
  let output = ''
  let spawnError: Error | undefined
  child.on('error', (error) => {
    spawnError = error
  })
  child.stdout?.on('data', (chunk: Buffer) => {
    output += chunk.toString()
  })
  child.stderr?.on('data', (chunk: Buffer) => {
    output += chunk.toString()
  })
  const isAlive = (): boolean =>
    spawnError === undefined && child.exitCode === null
  const assertAlive = (): void => {
    if (!isAlive()) {
      throw new Error(
        `Routevane browser server exited early: ${spawnError?.message ?? output}`,
      )
    }
  }
  return { process: child, output: () => output, isAlive, assertAlive }
}

export function delay(milliseconds: number): Promise<void> {
  return new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds))
}

export async function resolvesWithin(
  value: Promise<unknown>,
  milliseconds: number,
): Promise<boolean> {
  return Promise.race([
    value.then(() => true),
    delay(milliseconds).then(() => false),
  ])
}

/**
 * stopOwnedProduct asks a child process this suite owns to exit, waits up to
 * `timeoutMilliseconds` for the exit event, and escalates to SIGKILL exactly
 * once before giving up. It never resolves while the process might still be
 * running, so a caller that awaits it can safely reuse the port the process
 * held — unlike a bare `kill()` plus an unbounded `once(child, 'exit')`,
 * which hangs forever if the first signal is ignored.
 */
export async function stopOwnedProduct(
  child: ChildProcess,
  timeoutMilliseconds = STOP_TIMEOUT_MILLISECONDS,
): Promise<void> {
  if (child.exitCode !== null)
    throw new Error('owned Routevane child exited before teardown')
  const exited = once(child, 'exit')
  if (!child.kill())
    throw new Error('owned Routevane child could not be terminated')
  if (await resolvesWithin(exited, timeoutMilliseconds)) return
  if (!child.kill('SIGKILL'))
    throw new Error('owned Routevane child resisted termination')
  if (!(await resolvesWithin(exited, timeoutMilliseconds)))
    throw new Error('owned Routevane child did not confirm exit')
}

/**
 * reserveLoopbackPort hands back a port that is provably an IPv4 loopback
 * port and provably bindable again immediately after release, so the caller
 * never hands the product a port number that only looked free.
 */
export async function reserveLoopbackPort(): Promise<number> {
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
export async function assertPortBindable(candidatePort: number): Promise<void> {
  const listener = createServer()
  await listenOnLoopback(listener, candidatePort)
  await closeServer(listener)
}

async function listenOnLoopback(
  listener: Server,
  candidatePort: number,
): Promise<void> {
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

async function closeServer(listener: Server): Promise<void> {
  await new Promise<void>((resolveClose, rejectClose) => {
    listener.close((error) =>
      error === undefined ? resolveClose() : rejectClose(error),
    )
  })
}
