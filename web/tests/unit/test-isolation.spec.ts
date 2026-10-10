import { execFile } from 'node:child_process'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { join, resolve } from 'node:path'
import { promisify } from 'node:util'
import { describe, expect, it } from 'vitest'
import { ownedFiles } from '../dev/owned-files'
import { isolatedProductEnvironment } from '../e2e/support/environment'

const exec = promisify(execFile)
const scratch = async () => {
  const parent = resolve('../tmp/test-isolation')
  await mkdir(parent, { recursive: true })
  return mkdtemp(join(parent, 'run-'))
}

describe('test resource ownership', () => {
  it('preserves an existing probe after exclusive creation fails', async () => {
    const directory = await scratch()
    const path = join(directory, 'probe.vue')
    const files = ownedFiles()
    try {
      await writeFile(path, 'foreign content')
      await expect(files.create(path, 'replacement')).rejects.toMatchObject({
        code: 'EEXIST',
      })
      expect(files.paths()).toEqual([])
      await files.cleanup()
      expect(await readFile(path, 'utf8')).toBe('foreign content')
    } finally {
      await rm(directory, { recursive: true })
    }
  })

  it('keeps cleanup ownership when a diagnostic snapshot is changed', async () => {
    const directory = await scratch()
    const path = join(directory, 'probe.vue')
    const files = ownedFiles()
    try {
      await files.create(path, 'owned content')
      const snapshot = files.paths()
      expect(snapshot).toEqual([path])
      expect(Reflect.set(snapshot, 'length', 0)).toBe(true)
      expect(snapshot).toEqual([])
      expect(files.paths()).toEqual([path])
      await files.cleanup()
      await expect(readFile(path)).rejects.toMatchObject({ code: 'ENOENT' })
    } finally {
      await rm(directory, { recursive: true })
    }
  })

  it('lists an exclusively created probe even when its write fails', async () => {
    const directory = await scratch()
    const path = join(directory, 'probe.vue')
    const files = ownedFiles()
    try {
      // Force writeFile to reject after open without disk-space or permission
      // assumptions. The empty file already belongs to this fixture.
      await expect(
        files.create(path, undefined as unknown as string),
      ).rejects.toThrow(TypeError)
      expect(files.paths()).toEqual([path])
      expect(await readFile(path, 'utf8')).toBe('')
      await files.cleanup()
      await expect(readFile(path)).rejects.toMatchObject({ code: 'ENOENT' })
    } finally {
      await rm(directory, { recursive: true })
    }
  })

  it.each([false, true])(
    'removes owned probes when a body fails: %s',
    async (fail) => {
      const directory = await scratch()
      const path = join(directory, 'probe.vue')
      const files = ownedFiles()
      const run = async () => {
        try {
          await files.create(path, 'owned content')
          if (fail) throw new Error('body failed')
        } finally {
          await files.cleanup()
        }
      }
      try {
        const result = await run().then(
          () => 'completed',
          (error: Error) => error.message,
        )
        expect(result).toBe(fail ? 'body failed' : 'completed')
        await expect(readFile(path)).rejects.toMatchObject({ code: 'ENOENT' })
      } finally {
        await rm(directory, { recursive: true })
      }
    },
  )

  it('does not pass personal plugin discovery to a launched test process', async () => {
    const directory = await scratch()
    const marker = join(directory, 'started')
    const parent = { ...process.env, ROUTEVANE_PLUGINS_DIR: directory }
    const script = `const fs = require('node:fs'); const path = require('node:path'); if (process.env.ROUTEVANE_PLUGINS_DIR) fs.writeFileSync(path.join(process.env.ROUTEVANE_PLUGINS_DIR, 'started'), 'started');`
    try {
      await exec(process.execPath, ['-e', script], {
        env: isolatedProductEnvironment(parent),
        windowsHide: true,
      })
      await expect(readFile(marker)).rejects.toMatchObject({ code: 'ENOENT' })
      await exec(process.execPath, ['-e', script], {
        env: parent,
        windowsHide: true,
      })
      expect(await readFile(marker, 'utf8')).toBe('started')
      expect(parent.ROUTEVANE_PLUGINS_DIR).toBe(directory)
    } finally {
      await rm(directory, { recursive: true })
    }
  })
})
