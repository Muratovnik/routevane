import test from 'node:test'
import assert from 'node:assert/strict'
import { createRecovery } from '../src/recovery.mjs'

// The codes Chromium hands to did-fail-load: a navigation the application
// itself cancelled, and a transport that could not deliver the document.
const ABORTED = -3
const FAILED = -2

const failure = { isMainFrame: true, errorCode: FAILED }

test('a cancelled load and a subordinate frame are not failures, and a burst asks once', () => {
  const recovery = createRecovery()
  assert.equal(
    recovery.failed({ isMainFrame: true, errorCode: ABORTED }),
    'ignore',
  )
  assert.equal(
    recovery.failed({ isMainFrame: false, errorCode: FAILED }),
    'ignore',
  )
  assert.equal(recovery.failed(failure), 'retry')
  assert.equal(recovery.failed(failure), 'ignore')
  assert.equal(recovery.retried(), true)
  assert.equal(recovery.retried(), false)
})

test('consecutive failures spend the attempts and the error document cannot refill them', () => {
  const recovery = createRecovery({ retries: 2 })
  assert.equal(recovery.failed(failure), 'retry')
  // Chromium finishes the error document for the failure just reported.
  recovery.loaded()
  assert.equal(recovery.retried(), true)
  assert.equal(recovery.failed(failure), 'retry')
  recovery.loaded()
  assert.equal(recovery.retried(), true)
  assert.equal(recovery.failed(failure), 'stop')
  assert.equal(recovery.failed(failure), 'ignore')
})

test('a page that loads restores the attempts for a later, unrelated failure', () => {
  const recovery = createRecovery({ retries: 1 })
  assert.equal(recovery.failed(failure), 'retry')
  assert.equal(recovery.retried(), true)
  recovery.loaded()
  assert.equal(recovery.failed(failure), 'retry')
  assert.equal(recovery.retried(), true)
  assert.equal(recovery.failed(failure), 'stop')
})

test('a hung window is questioned once per hang', () => {
  const recovery = createRecovery()
  assert.equal(recovery.unresponsive(), true)
  assert.equal(recovery.unresponsive(), false)
  recovery.responsive()
  assert.equal(recovery.unresponsive(), true)
})
