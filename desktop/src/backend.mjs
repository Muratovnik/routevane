import { spawn } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { createInterface } from 'node:readline'
import { setTimeout as delay } from 'node:timers/promises'

export function startBackend({ binary, catalog, data }) {
  const token = randomBytes(32).toString('hex')
  const child = spawn(
    binary,
    ['desktop', '--catalog-dir', catalog, '--data-dir', data],
    {
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
    },
  )
  let settled = false
  let startupError
  const exited = new Promise((resolve) => {
    child.once('error', (error) => {
      startupError = error
      settled = true
      resolve()
    })
    child.once('exit', () => {
      settled = true
      resolve()
    })
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
        const timeout = new AbortController()
        await Promise.race([
          exited,
          delay(150000, null, { signal: timeout.signal }).catch(() => {}),
        ])
        timeout.abort()
        if (!settled) {
          child.kill('SIGKILL')
          await exited
        }
        lines.close()
      })()
      return stopping
    },
  }
}
