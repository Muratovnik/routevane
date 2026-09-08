/** What the library pane draws, and which row owns which action. */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  activeCategory,
  ALL_CATEGORIES,
  categoryNames,
  catalogRoutes,
  CATEGORIES,
  CATEGORY_NAME_FIELD,
  closeMenu,
  contentsResponse,
  NEW_CATEGORY,
  NEW_LIST,
  openCategories,
  openCategory,
  openMenu,
  renderLibrary,
  resetLibrary,
  restoreGlobals,
  rowNames,
  stubAPI,
  UNCATEGORIZED,
} from './support/library'

describe('the library pane', () => {
  beforeEach(resetLibrary)
  afterEach(restoreGlobals)

  // Every category, then the lists no category claims — the same column the
  // composer draws, without a checkbox, because nothing is being selected.
  it('lists every category and the lists none of them claims', async () => {
    stubAPI(catalogRoutes())
    const screen = await renderLibrary()

    await expect.element(screen.getByRole('table')).toBeVisible()
    // Nothing is being selected here, so no row carries a choice.
    expect(screen.getByRole('checkbox').all()).toEqual([])

    const names = await categoryNames(screen)
    expect(names).toHaveLength(4)
    expect(names.at(-1)).toBe(UNCATEGORIZED)

    await openCategory(screen, UNCATEGORIZED)
    expect(rowNames(screen)).toEqual(['Steam'])
  })

  it('offers category management and list creation through their owning actions', async () => {
    stubAPI(catalogRoutes())
    const screen = await renderLibrary()

    await expect.element(screen.getByRole('table')).toBeVisible()
    expect(rowNames(screen)).toHaveLength(4)

    await openCategories(screen)
    await screen.getByRole('button', { name: NEW_CATEGORY }).click()
    // A new category is a full working surface rather than a centred box.
    const sheet = screen.getByRole('dialog', { name: NEW_CATEGORY })
    await expect.element(sheet).toHaveClass('rv-dialog--sheet')
    await sheet.getByRole('button', { name: 'Cancel' }).click()

    await openCategory(screen, 'Communication')
    await screen.getByRole('button', { name: NEW_LIST }).click()
    const listSheet = screen.getByRole('dialog', { name: NEW_LIST })
    await expect.element(listSheet).toHaveClass('rv-dialog--sheet')
    await expect
      .element(listSheet.getByRole('radio', { name: 'Existing list' }))
      .toBeVisible()
  })

  it('keeps category and list actions on their rows without a duplicate add action', async () => {
    stubAPI(catalogRoutes())
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    // This menu belongs to an unselected built-in category. Built-in records
    // can be removed through the server overlay, but their catalog title is not
    // renamed and list creation stays at the pane footer.
    await openMenu(screen, 'Actions for category Video')
    await expect
      .element(screen.getByRole('menuitem', { name: 'Delete the category' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('menuitem', { name: 'Rename' }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('menuitem', { name: 'Add a list' }))
      .not.toBeInTheDocument()
    await closeMenu(screen)

    await openCategory(screen, 'Communication')
    await openMenu(screen, 'Actions for list Discord')
    for (const item of ['Remove from the category', 'Delete the list'])
      await expect
        .element(screen.getByRole('menuitem', { name: item }))
        .toBeVisible()
    await expect
      .element(screen.getByRole('menuitem', { name: 'Rename' }))
      .not.toBeInTheDocument()
    await closeMenu(screen)

    await openMenu(screen, 'Actions for list Telegram')
    for (const item of [
      'Rename',
      'Remove from the category',
      'Delete the list',
    ])
      await expect
        .element(screen.getByRole('menuitem', { name: item }))
        .toBeVisible()
  })

  it('acts on an unselected category without moving the current pane', async () => {
    const remaining = CATEGORIES.filter((category) => category.id !== 'video')
    const { keys } = stubAPI({
      ...catalogRoutes(remaining),
      'POST /v1/categories/video/remove': () =>
        new Response(null, { status: 204 }),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()
    expect(activeCategory(screen)).toBe(ALL_CATEGORIES)

    await openMenu(screen, 'Actions for category Video')
    await screen.getByRole('menuitem', { name: 'Delete the category' }).click()
    await screen
      .getByRole('dialog')
      .getByRole('button', { name: 'Delete' })
      .click()

    await vi.waitFor(() => {
      expect(keys()).toContain('POST /v1/categories/video/remove')
    })
    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    expect(activeCategory(screen)).toBe(ALL_CATEGORIES)
  })

  it('opens a custom list rename directly from its row menu', async () => {
    stubAPI({
      ...catalogRoutes(),
      'GET /v1/lists/telegram/contents': () => contentsResponse('telegram'),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    await openCategory(screen, 'Communication')
    await openMenu(screen, 'Actions for list Telegram')
    await screen.getByRole('menuitem', { name: 'Rename' }).click()

    const rename = screen.getByRole('dialog', { name: 'Telegram' })
    await expect.element(rename).toBeVisible()
    await expect
      .element(rename.getByRole('textbox', { name: CATEGORY_NAME_FIELD }))
      .toHaveValue('Telegram')
  })

  // The address states where the operator is, so the composing card's link
  // lands on the category and the list it named.
  it('opens the category and the list named in the address', async () => {
    stubAPI({
      ...catalogRoutes(),
      'GET /v1/lists/youtube/contents': () => contentsResponse('youtube'),
    })
    const screen = await renderLibrary('#category=video&list=youtube')

    const card = screen.getByRole('dialog', { name: 'YouTube' })
    await expect.element(card).toBeVisible()
    await expect.element(card).toMatchTextContent('youtube.example')
    expect(activeCategory(screen)).toBe('Video')
    // No profile is in question here, so the card asks about none.
    await expect
      .element(screen.getByText('this profile', { exact: false }))
      .not.toBeInTheDocument()
  })
})
