import test from 'node:test'
import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import { createUpdates } from '../src/updates.mjs'

test('checking never downloads; one click downloads once, reports progress and then installs', async () => {
  const updater = new EventEmitter()
  let downloads = 0
  let installed = 0
  let finish
  updater.checkForUpdates = async () =>
    updater.emit('update-available', { version: '1.2.3' })
  updater.downloadUpdate = async () => {
    downloads++
    updater.emit('download-progress', { percent: 24.9 })
    await new Promise((resolve) => {
      finish = resolve
    })
  }
  const updates = createUpdates({
    updater,
    install: async () => {
      installed++
    },
  })
  await updates.check()
  assert.equal(updater.autoDownload, false)
  assert.equal(updater.autoInstallOnAppQuit, false)
  assert.equal(downloads, 0)
  const action = updates.apply()
  await updates.apply()
  assert.equal(downloads, 1)
  assert.equal(installed, 0)
  assert.deepEqual(updates.snapshot(), {
    status: 'downloading',
    version: '1.2.3',
    percent: 24,
  })
  finish()
  await action
  assert.equal(installed, 1)
  assert.equal(updates.snapshot().status, 'installing')
})

test('a rejected download is retryable and quitting never installs a pending download', async () => {
  const updater = new EventEmitter()
  let installed = 0
  updater.checkForUpdates = async () =>
    updater.emit('update-available', { version: '1.2.3' })
  updater.downloadUpdate = async () => {
    throw new Error('checksum mismatch')
  }
  const updates = createUpdates({
    updater,
    install: async () => {
      installed++
    },
  })
  await updates.check()
  await updates.apply()
  assert.equal(updates.snapshot().status, 'error')
  assert.equal(installed, 0)
  let finish
  updater.downloadUpdate = () =>
    new Promise((resolve) => {
      finish = resolve
    })
  const retry = updates.apply()
  updates.close()
  finish()
  await retry
  assert.equal(installed, 0)
})

test('offline discovery stays quiet and unavailable commands have no side effects', async () => {
  const updater = new EventEmitter()
  updater.checkForUpdates = async () => {
    throw new Error('offline')
  }
  updater.downloadUpdate = () => assert.fail('unexpected download')
  const updates = createUpdates({
    updater,
    install: () => assert.fail('unexpected install'),
  })
  await updates.apply()
  await updates.check()
  assert.equal(updates.snapshot().status, 'idle')
})

test('a click during discovery waits for it and retains installation intent', async () => {
  const updater = new EventEmitter()
  let finish
  let downloads = 0
  let installed = 0
  updater.checkForUpdates = () =>
    new Promise((resolve) => {
      finish = resolve
    })
  updater.downloadUpdate = async () => {
    downloads++
  }
  const updates = createUpdates({
    updater,
    install: async () => {
      installed++
    },
  })
  const checking = updates.check()
  updater.emit('update-available', { version: '1.2.3' })
  const action = updates.apply()
  await updates.apply()
  assert.equal(downloads, 0)
  finish()
  await checking
  await action
  assert.equal(downloads, 1)
  assert.equal(installed, 1)
})
