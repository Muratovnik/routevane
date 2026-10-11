import { spawn } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { createInterface } from 'node:readline'
import { setTimeout as delay } from 'node:timers/promises'

export function startBackend({ binary, catalog, data, recoverPort = false }) {
  const token = randomBytes(32).toString('hex')
  const child = spawn(
    binary,
    [
      'desktop',
      '--catalog-dir',
      catalog,
      '--data-dir',
      data,
      ...(recoverPort ? ['--recover-desktop-port'] : []),
    ],
    {
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
    },
  )
  let startupError
  // 'close' also waits for every holder of the output pipes, so a descendant
  // that inherited them can outlive the process. Shutdown waits for the
  // process itself; consumers of `exited` keep reading all of its output.
  const gone = new Promise((resolve) => {
    child.once('error', resolve)
    child.once('exit', resolve)
  })
  const exited = new Promise((resolve) => {
    child.once('error', (error) => {
      startupError = error
      resolve()
    })
    child.once('close', resolve)
  })
  // Drain diagnostics without retaining arbitrary source/device payloads.
  child.stderr.resume()
  child.stdin.on('error', () => {})
  child.stdin.write(`${token}\n`)
  const lines = createInterface({ input: child.stdout })
  const ready = new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('startup_timeout')), 30000)
    lines.once('line', (line) => {
      clearTimeout(timer)
      if (line === 'routevane:desktop-port-unavailable') {
        reject(new Error('desktop_port_unavailable'))
        return
      }
      if (
        !/^http:\/\/127\.0\.0\.1:[1-9]\d{0,4}$/.test(line) ||
        Number(new URL(line).port) > 65535
      ) {
        reject(new Error('startup_protocol'))
      } else {
        resolve(line)
      }
    })
    void exited.then(() => {
      clearTimeout(timer)
      reject(startupError ?? new Error('backend_stopped'))
    })
  })
  let stopping
  return {
    child,
    token,
    ready,
    exited,
    stop() {
      stopping ??= (async () => {
        child.stdin.end()
        // Allow bounded in-flight cancellation and device recovery to finish.
        if (!(await within(gone, 150000))) {
          child.kill('SIGKILL')
          await within(gone, 5000)
        }
        // Let the remaining output drain, then release pipes still held by a
        // descendant so 'close' can fire instead of holding quit open.
        if (!(await within(exited, 1000))) {
          child.stdout.destroy()
          child.stderr.destroy()
          await within(exited, 1000)
        }
        lines.close()
      })()
      return stopping
    },
  }
}

async function within(promise, ms) {
  const timeout = new AbortController()
  try {
    return await Promise.race([
      promise.then(() => true),
      delay(ms, false, { signal: timeout.signal }).catch(() => false),
    ])
  } finally {
    timeout.abort()
  }
}
