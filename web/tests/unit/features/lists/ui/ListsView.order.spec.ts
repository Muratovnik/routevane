/** The order the library hands every profile that does not state its own. */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'

import {
  catalogRoutes,
  json,
  renderLibrary,
  resetLibrary,
  restoreGlobals,
  stubAPI,
} from './support/library'

describe('the library default order', () => {
  beforeEach(resetLibrary)
  afterEach(restoreGlobals)

  it('saves the library default order without writing any profile', async () => {
    const saved = ['telegram', 'discord', 'youtube', 'steam']
    const { calls } = stubAPI({
      ...catalogRoutes(),
      'POST /v1/lists/priority': () => json({ default_priority: saved }),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    const handle = screen.getByRole('button', {
      name: 'Change priority of list Discord, position 1',
    })
    handle.element().focus()
    await userEvent.keyboard('{ArrowDown}')
    await screen.getByRole('button', { name: 'Save order' }).click()

    await vi.waitFor(() => {
      expect(calls.at(-1)).toEqual({
        body: JSON.stringify({ default_priority: saved }),
        key: 'POST /v1/lists/priority',
      })
    })
    expect(calls.some((call) => call.key.includes('/v1/profiles'))).toBe(false)
    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
  })

  it('keeps a failed default order available to retry', async () => {
    const { calls } = stubAPI({
      ...catalogRoutes(),
      'POST /v1/lists/priority': () =>
        json({ error: 'controlled failure' }, 503),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    const handle = screen.getByRole('button', {
      name: 'Change priority of list Discord, position 1',
    })
    handle.element().focus()
    await userEvent.keyboard('{ArrowDown}')
    await screen.getByRole('button', { name: 'Save order' }).click()

    await expect.element(screen.getByText('Order not saved')).toBeVisible()
    await expect.element(screen.getByRole('table')).toBeVisible()
    expect(
      calls.filter((call) => call.key.includes('/v1/profiles')),
    ).toHaveLength(0)
  })
})
