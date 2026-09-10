import { describe, expect, it } from 'vitest'

import { isModuleLoadError } from '@/shared/lib/moduleLoadError'

// The words each engine actually writes when a section's module does not
// arrive. They are the whole contract: nothing else about the rejection says
// what failed, so a wording this table misses is a section that fails silently.
const refusals = [
  {
    engine: 'Chromium',
    value: new TypeError(
      'Failed to fetch dynamically imported module: http://127.0.0.1:8765/_nuxt/BhMIwfn2.js',
    ),
  },
  {
    engine: 'Firefox',
    value: new TypeError(
      'error loading dynamically imported module: http://127.0.0.1:8765/_nuxt/BhMIwfn2.js',
    ),
  },
  {
    engine: 'Safari',
    value: new TypeError('Importing a module script failed.'),
  },
  {
    engine: "the bundler's preload helper",
    value: new Error('Unable to preload CSS for /_nuxt/AppShell.DrViVyMX.css'),
  },
  // A rejection crosses a realm — a worker, an event from another frame — as a
  // plain object that is no longer an Error. The message is still the message.
  {
    engine: 'another realm',
    value: { message: 'Failed to fetch dynamically imported module: /x.js' },
  },
  { engine: 'a bare message', value: 'Importing a module script failed.' },
]

// Failures that are not a missing module. A screen answers each of these on its
// own, and reloading the page for one of them would throw away the operator's
// work to fix nothing.
const otherFailures = [
  {
    what: 'a request that never reached the service',
    value: new TypeError('Failed to fetch'),
  },
  { what: 'a load event with no message', value: new Event('error') },
  {
    what: 'a refusal the service stated',
    value: new Error('Операция не выполнена.'),
  },
  { what: 'a rejection with no reason', value: undefined },
  { what: 'nothing at all', value: null },
  { what: 'a number', value: 500 },
  { what: 'an object that states no message', value: { status: 503 } },
  { what: 'an object whose message is not text', value: { message: 404 } },
]

describe('isModuleLoadError', () => {
  it.each(refusals)('recognises $engine', ({ value }) => {
    expect(isModuleLoadError(value)).toBe(true)
  })

  it.each(otherFailures)('does not claim $what', ({ value }) => {
    expect(isModuleLoadError(value)).toBe(false)
  })
})
