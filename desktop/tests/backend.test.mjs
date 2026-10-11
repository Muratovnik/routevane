import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'
import { startBackend } from '../src/backend.mjs'

test('saved-port failure is classified and recovery is passed only explicitly', async () => {
  const scratch = await mkdtemp(join(tmpdir(), 'routevane-handshake-'))
  const entry = join(scratch, 'entry.mjs')
  const previous = process.env.NODE_OPTIONS
  await writeFile(
    entry,
    `
    import { createInterface } from 'node:readline';
    const input = createInterface({ input: process.stdin });
    input.once('line', () => {
      if (!process.argv.includes('--recover-desktop-port')) {
        process.stdout.write('routevane:desktop-port-unavailable\\n', () => process.exit(1));
      } else {
        process.stdout.write('http://127.0.0.1:43211\\n');
        input.once('close', () => process.exit(0));
      }
    });
    await new Promise(() => {});
  `,
  )
  process.env.NODE_OPTIONS = `--import=${JSON.stringify(pathToFileURL(entry).href)}`
  try {
    for (const recoverPort of [false, true]) {
      const backend = startBackend({
        binary: process.execPath,
        catalog: scratch,
        data: scratch,
        recoverPort,
      })
      try {
        if (recoverPort) {
          assert.equal(await backend.ready, 'http://127.0.0.1:43211')
        } else {
          await assert.rejects(backend.ready, {
            message: 'desktop_port_unavailable',
          })
        }
      } finally {
        await backend.stop()
      }
      assert.equal(backend.child.exitCode, recoverPort ? 0 : 1)
    }
  } finally {
    if (previous === undefined) delete process.env.NODE_OPTIONS
    else process.env.NODE_OPTIONS = previous
    await rm(scratch, { recursive: true, force: true })
  }
})

test('unrecognized startup output cannot request port recovery', async () => {
  const scratch = await mkdtemp(join(tmpdir(), 'routevane-handshake-'))
  const entry = join(scratch, 'entry.mjs')
  const previous = process.env.NODE_OPTIONS
  await writeFile(
    entry,
    `
    import { createInterface } from 'node:readline';
    const input = createInterface({ input: process.stdin });
    input.once('line', () => {
      process.stdout.write('routevane:database-unavailable\\n');
      input.once('close', () => process.exit(1));
    });
    await new Promise(() => {});
  `,
  )
  process.env.NODE_OPTIONS = `--import=${JSON.stringify(pathToFileURL(entry).href)}`
  try {
    const backend = startBackend({
      binary: process.execPath,
      catalog: scratch,
      data: scratch,
    })
    try {
      await assert.rejects(backend.ready, { message: 'startup_protocol' })
    } finally {
      await backend.stop()
    }
    assert.equal(backend.child.exitCode, 1)
  } finally {
    if (previous === undefined) delete process.env.NODE_OPTIONS
    else process.env.NODE_OPTIONS = previous
    await rm(scratch, { recursive: true, force: true })
  }
})
