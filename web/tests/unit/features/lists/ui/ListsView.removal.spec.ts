/** Deleting a category or a list, and taking a list out of one. */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  categoryNames,
  catalogRoutes,
  CATEGORIES,
  hiddenButton,
  json,
  openCategory,
  openMenu,
  renderLibrary,
  resetLibrary,
  restoreGlobals,
  stubAPI,
  UNCATEGORIZED,
} from './support/library'

describe('deleting and detaching', () => {
  beforeEach(resetLibrary)
  afterEach(restoreGlobals)

  /**
   * Deleting a category asks the one question it has to ask, and the answer is
   * the request: the lists move to «Без категории», or they go with it.
   */
  it.each([
    ['Move them to Uncategorized', 'detach'],
    ['Delete them with it', 'delete'],
  ])('deletes a category, %s', async (choice, disposition) => {
    const { calls } = stubAPI({
      ...catalogRoutes(CATEGORIES.filter((entry) => entry.custom !== true)),
      'POST /v1/categories/custom-home/remove': () =>
        new Response(null, { status: 204 }),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    await openCategory(screen, 'Домашние')
    await openMenu(screen, 'Actions for category Домашние')
    await screen.getByRole('menuitem', { name: 'Delete the category' }).click()

    const confirm = screen.getByRole('dialog')
    await expect
      .element(screen.getByText('What happens to its lists', { exact: false }))
      .toBeVisible()
    await confirm.getByText(choice).click()
    await confirm.getByRole('button', { name: 'Delete' }).click()

    await vi.waitFor(() => {
      const write = calls.find(
        (call) => call.key === 'POST /v1/categories/custom-home/remove',
      )
      expect(write?.body).toBe(JSON.stringify({ lists: disposition }))
    })
    expect(await categoryNames(screen)).not.toContain('Домашние')
  })

  // A category a profile still names is kept, and the refusal names the profiles
  // standing in the way — beside the act that was refused.
  it('keeps a category a profile still holds and names those profiles', async () => {
    stubAPI({
      ...catalogRoutes(),
      'POST /v1/categories/custom-home/remove': () =>
        json(
          {
            error: 'category in use',
            profiles: [
              { id: 'a'.repeat(32), title: 'Дом' },
              { id: 'b'.repeat(32), title: 'Офис' },
            ],
          },
          409,
        ),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    await openCategory(screen, 'Домашние')
    await openMenu(screen, 'Actions for category Домашние')
    await screen.getByRole('menuitem', { name: 'Delete the category' }).click()
    await screen
      .getByRole('dialog')
      .getByRole('button', { name: 'Delete' })
      .click()

    await expect
      .element(
        screen.getByText('The category was not deleted', { exact: false }),
      )
      .toBeVisible()
    await expect
      .element(screen.getByText('Дом, Офис', { exact: false }))
      .toBeVisible()
    await screen
      .getByRole('dialog')
      .getByRole('button', { name: 'Cancel' })
      .click()
    expect(await categoryNames(screen)).toContain('Домашние')
  })

  // The bin means deletion and nothing else now: leaving a category is a menu
  // item with words on it.
  it('takes a list out of its category from the row menu', async () => {
    const trimmed = CATEGORIES.map((category) =>
      category.id === 'communication'
        ? { ...category, lists: ['telegram'] }
        : category,
    )
    const { calls } = stubAPI({
      ...catalogRoutes(trimmed),
      'POST /v1/categories/communication/update': () =>
        json({ category: trimmed[0] }),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    await openCategory(screen, 'Communication')
    await openMenu(screen, 'Actions for list Discord')
    await screen
      .getByRole('menuitem', { name: 'Remove from the category' })
      .click()

    await vi.waitFor(() => {
      const write = calls.find(
        (call) => call.key === 'POST /v1/categories/communication/update',
      )
      expect(write?.body).toBe(JSON.stringify({ lists: ['telegram'] }))
    })
  })

  it('deletes a list, and keeps one a profile still holds', async () => {
    const { calls, keys } = stubAPI({
      ...catalogRoutes(),
      'POST /v1/lists/telegram/remove': () =>
        json(
          {
            error: 'list in use',
            profiles: [{ id: 'a'.repeat(32), title: 'Дом' }],
          },
          409,
        ),
      'POST /v1/lists/steam/remove': () => new Response(null, { status: 204 }),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    await openCategory(screen, 'Communication')
    await openMenu(screen, 'Actions for list Telegram')
    await screen.getByRole('menuitem', { name: 'Delete the list' }).click()
    await expect
      .element(
        screen.getByText('List “Telegram” and its entries are removed.', {
          exact: false,
        }),
      )
      .toBeVisible()
    await screen
      .getByRole('dialog')
      .getByRole('button', { name: 'Delete' })
      .click()

    await expect
      .element(screen.getByText('The list was not deleted', { exact: false }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('dialog').getByText('Дом', { exact: false }))
      .toBeVisible()
    expect(keys().at(-1)).toBe('POST /v1/lists/telegram/remove')

    // The one nothing holds goes, and the catalog is read back after it.
    await screen
      .getByRole('dialog')
      .getByRole('button', { name: 'Cancel' })
      .click()
    await openCategory(screen, UNCATEGORIZED)
    await openMenu(screen, 'Actions for list Steam')
    await screen.getByRole('menuitem', { name: 'Delete the list' }).click()
    await screen
      .getByRole('dialog')
      .getByRole('button', { name: 'Delete' })
      .click()

    await vi.waitFor(() => {
      expect(keys().slice(-2)).toEqual(['GET /v1/lists', 'GET /v1/targets'])
    })
    expect(
      calls.some((call) => call.key === 'POST /v1/lists/steam/remove'),
    ).toBe(true)
  })

  it('blocks every conflicting library action while a deletion is pending', async () => {
    const held = Promise.withResolvers<Response>()
    const { keys } = stubAPI({
      ...catalogRoutes(),
      'POST /v1/lists/telegram/remove': () => held.promise,
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    await openCategory(screen, 'Communication')
    await openMenu(screen, 'Actions for list Telegram')
    await screen.getByRole('menuitem', { name: 'Delete the list' }).click()
    const confirm = screen.getByRole('dialog')
    const submit = confirm.getByRole('button', { name: 'Delete' })
    await submit.click()

    await expect.element(submit).toBeDisabled()
    await expect.element(submit).toHaveAttribute('aria-busy', 'true')
    await expect
      .element(confirm.getByRole('button', { name: 'Close' }))
      .toBeDisabled()
    for (const trigger of hiddenButton(screen, /^Actions for/).elements())
      expect((trigger as HTMLButtonElement).disabled).toBe(true)

    // A second press starts no second deletion.
    await submit.click({ force: true })
    expect(
      keys().filter((key) => key === 'POST /v1/lists/telegram/remove'),
    ).toHaveLength(1)

    held.resolve(json({ error: 'controlled refusal' }, 503))
    await expect.element(submit).toBeEnabled()
    await expect.element(confirm).toBeVisible()
  })
})
