/**
 * The binary every browser suite drives: the test build (ADR 0052).
 *
 * `tools/dev.ps1 build` and `test-browser` compile it with the
 * `routevane_testmode` tag into `.cache/test-build`, apart from the
 * release-shaped core in `.cache/build` that the desktop shell packages. It
 * accepts RFC 5737 documentation addresses as devices, keeps a saved password
 * in its own memory instead of the workstation's credential store, and opens no
 * device connection. What these suites prove is therefore the test build's
 * behaviour, not a release binary's.
 */
import { execFile } from 'node:child_process'
import { access } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { promisify } from 'node:util'

const run = promisify(execFile)

const repositoryRoot = resolve(
  dirname(fileURLToPath(import.meta.url)),
  '..',
  '..',
  '..',
  '..',
)

export const testBuildBinary = join(
  repositoryRoot,
  '.cache',
  'test-build',
  process.platform === 'win32' ? 'routevane.exe' : 'routevane',
)

let proven: Promise<void> | undefined

/**
 * Resolves once the binary exists and says it is a test build. A missing or
 * release-shaped binary is refused here, by name, rather than later as a
 * refused documentation address or a write to the real credential store.
 */
export const requireTestBuild = (): Promise<void> => {
  proven ??= (async () => {
    await access(testBuildBinary)
    const { stdout } = await run(testBuildBinary, ['version'], {
      timeout: 10_000,
      windowsHide: true,
    })
    if (!stdout.trim().endsWith('(test build)'))
      throw new Error(
        `${testBuildBinary} is not a test build (${stdout.trim()}); run tools/dev.ps1 build`,
      )
  })()
  return proven
}
