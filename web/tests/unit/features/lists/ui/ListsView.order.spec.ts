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

  // Priority numbers are visible: the handle prints the row's position beside
  // its glyph, and the unsaved order says so beside its two acts.
  it('prints each position beside its handle and says the order is unsaved', async () => {
    stubAPI(catalogRoutes())
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    const handles = () =>
      screen.getByRole('button', { name: /^Change priority of list/ })
    const printed = () =>
      handles()
        .elements()
        .map((handle) => handle.textContent?.trim())
    expect(printed()).toEqual(['1', '2', '3', '4'])
    await expect
      .element(screen.getByText('Unsaved changes'))
      .not.toBeInTheDocument()

    const discord = screen.getByRole('button', {
      name: 'Change priority of list Discord, position 1',
    })
    discord.element().focus()
    await userEvent.keyboard('{ArrowDown}')

    // The moved list prints its new position and still names it.
    await expect
      .element(
        screen.getByRole('button', {
          name: 'Change priority of list Discord, position 2',
        }),
      )
      .toHaveTextContent('2')
    expect(printed()).toEqual(['1', '2', '3', '4'])
    await expect.element(screen.getByText('Unsaved changes')).toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: 'Reset order' }))
      .toBeVisible()

    await screen.getByRole('button', { name: 'Reset order' }).click()
    await expect
      .element(screen.getByText('Unsaved changes'))
      .not.toBeInTheDocument()
  })

  // The service answering with an error and the service not answering ask for
  // different things, so the notice says which one happened.
  it.each([
    [
      'an error answer',
      () => Promise.resolve(json({ error: 'controlled failure' }, 500)),
      'The service received the request but answered with an error. Try again.',
      'The local service did not answer.',
    ],
    [
      'a lost request',
      () => Promise.reject(new TypeError('Failed to fetch')),
      'The local service did not answer. Check that Routevane is running.',
      'The service received the request',
    ],
  ])(
    'keeps a failed default order available to retry: %s',
    async (_case, respond, said, notSaid) => {
      const { calls } = stubAPI({
        ...catalogRoutes(),
        'POST /v1/lists/priority': respond,
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
      await expect.element(screen.getByText(said)).toBeVisible()
      await expect
        .element(screen.getByText(notSaid, { exact: false }))
        .not.toBeInTheDocument()
      await expect.element(screen.getByRole('table')).toBeVisible()
      expect(
        calls.filter((call) => call.key.includes('/v1/profiles')),
      ).toHaveLength(0)
    },
  )
})
