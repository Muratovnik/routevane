/** A committed write whose confirming read never arrived. */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  activeCategory,
  ALL_CATEGORIES,
  categoryNames,
  catalogResponse,
  catalogRoutes,
  CATEGORIES,
  CATEGORY_NAME_FIELD,
  fillField,
  json,
  NEW_CATEGORY,
  openCategory,
  openMenu,
  REFRESH,
  renderLibrary,
  resetLibrary,
  restoreGlobals,
  rowNames,
  STALE,
  stubAPI,
  UNCATEGORIZED,
} from './support/library'

describe('recovering an interrupted library write', () => {
  beforeEach(resetLibrary)
  afterEach(restoreGlobals)

  it('library audit: keeps a committed create stale until a GET-only retry confirms its row', async () => {
    const created = {
      custom: true,
      id: 'custom-stale-create',
      lists: [],
      title: 'Saved while offline',
    }
    const reads: string[] = []
    const { calls } = stubAPI({
      'GET /v1/lists': () => {
        reads.push('read')
        if (reads.length === 2)
          return json({ error: 'catalog unavailable' }, 503)
        return catalogResponse(
          reads.length >= 3 ? [...CATEGORIES, created] : CATEGORIES,
        )
      },
      'GET /v1/targets': () => json({ targets: [] }),
      'POST /v1/categories': () => json({ category: created }, 201),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    await screen.getByRole('button', { name: NEW_CATEGORY }).click()
    await fillField(
      screen.getByRole('dialog', { name: NEW_CATEGORY }),
      CATEGORY_NAME_FIELD,
      created.title,
    )
    await screen
      .getByRole('dialog', { name: NEW_CATEGORY })
      .getByRole('button', { name: 'Create' })
      .click()

    // The POST landed exactly once, but the retained copy cannot contain the
    // new row. The form is closed so its old input is not a resubmit prompt.
    await expect.element(screen.getByText(STALE)).toBeVisible()
    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    expect(
      calls.filter((call) => call.key === 'POST /v1/categories'),
    ).toHaveLength(1)
    expect(activeCategory(screen)).toBe(ALL_CATEGORIES)
    await expect
      .element(screen.getByText(created.title))
      .not.toBeInTheDocument()
    // A library that cannot be written to withdraws the one control that would
    // write, rather than accepting a press that provably changes nothing. Its
    // filters keep working, because reading a retained copy is not writing.
    await expect
      .element(screen.getByRole('button', { name: NEW_CATEGORY }))
      .toBeDisabled()
    await expect
      .element(screen.getByRole('button', { name: ALL_CATEGORIES }))
      .toBeEnabled()

    // Recovery is only GET. Once it confirms the category, the saved identity
    // completes selection; no second POST is sent.
    await screen.getByRole('button', { name: REFRESH }).click()
    await vi.waitFor(() => {
      expect(activeCategory(screen)).toBe(created.title)
    })
    expect(
      calls.filter((call) => call.key === 'POST /v1/categories'),
    ).toHaveLength(1)
    await expect.element(screen.getByText(STALE)).not.toBeInTheDocument()
  })

  it('library audit: preserves the category input when the write itself fails', async () => {
    const { calls } = stubAPI({
      ...catalogRoutes(),
      'POST /v1/categories': () =>
        json({ error: 'catalog unavailable while writing' }, 503),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    await screen.getByRole('button', { name: NEW_CATEGORY }).click()
    await fillField(
      screen.getByRole('dialog', { name: NEW_CATEGORY }),
      CATEGORY_NAME_FIELD,
      'Keep this draft',
    )
    await screen
      .getByRole('dialog', { name: NEW_CATEGORY })
      .getByRole('button', { name: 'Create' })
      .click()

    await vi.waitFor(() => {
      expect(
        calls.filter((call) => call.key === 'POST /v1/categories'),
      ).toHaveLength(1)
    })
    await expect
      .element(screen.getByLabelText(CATEGORY_NAME_FIELD))
      .toHaveValue('Keep this draft')
    await expect.element(screen.getByText(STALE)).not.toBeInTheDocument()
  })

  it('library audit: closes a committed stale rename and updates it on GET retry', async () => {
    const renamed = CATEGORIES.map((category) =>
      category.id === 'custom-home'
        ? { ...category, title: 'Renamed later' }
        : category,
    )
    const reads: string[] = []
    const { calls } = stubAPI({
      'GET /v1/lists': () => {
        reads.push('read')
        if (reads.length === 2)
          return json({ error: 'catalog unavailable' }, 503)
        return catalogResponse(reads.length >= 3 ? renamed : CATEGORIES)
      },
      'GET /v1/targets': () => json({ targets: [] }),
      'POST /v1/categories/custom-home/update': () =>
        json({ category: renamed[2] }),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()
    await openCategory(screen, 'Домашние')
    await openMenu(screen, 'Actions for category Домашние')
    await screen.getByRole('menuitem', { name: 'Rename' }).click()
    await fillField(
      screen.getByRole('dialog'),
      CATEGORY_NAME_FIELD,
      'Renamed later',
    )
    await screen
      .getByRole('dialog')
      .getByRole('button', { name: 'Save' })
      .click()

    await expect.element(screen.getByText(STALE)).toBeVisible()
    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    expect(activeCategory(screen)).toBe('Домашние')
    expect(
      calls.filter(
        (call) => call.key === 'POST /v1/categories/custom-home/update',
      ),
    ).toHaveLength(1)

    await screen.getByRole('button', { name: REFRESH }).click()
    await vi.waitFor(() => {
      expect(activeCategory(screen)).toBe('Renamed later')
    })
    await expect.element(screen.getByText(STALE)).not.toBeInTheDocument()
    expect(
      calls.filter(
        (call) => call.key === 'POST /v1/categories/custom-home/update',
      ),
    ).toHaveLength(1)
  })

  it('library audit: closes a committed stale category deletion on a safe row', async () => {
    const reads: string[] = []
    const remaining = CATEGORIES.filter((entry) => entry.id !== 'custom-home')
    const { calls } = stubAPI({
      'GET /v1/lists': () => {
        reads.push('read')
        if (reads.length === 2)
          return json({ error: 'catalog unavailable' }, 503)
        return catalogResponse(reads.length >= 3 ? remaining : CATEGORIES)
      },
      'GET /v1/targets': () => json({ targets: [] }),
      'POST /v1/categories/custom-home/remove': () =>
        new Response(null, { status: 204 }),
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

    await expect.element(screen.getByText(STALE)).toBeVisible()
    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    expect(activeCategory(screen)).toBe(UNCATEGORIZED)
    expect(await categoryNames(screen)).toContain('Домашние')
    expect(
      calls.filter(
        (call) => call.key === 'POST /v1/categories/custom-home/remove',
      ),
    ).toHaveLength(1)

    await screen.getByRole('button', { name: REFRESH }).click()
    await expect.element(screen.getByText(STALE)).not.toBeInTheDocument()
    expect(activeCategory(screen)).toBe(UNCATEGORIZED)
    expect(await categoryNames(screen)).not.toContain('Домашние')
    expect(
      calls.filter(
        (call) => call.key === 'POST /v1/categories/custom-home/remove',
      ),
    ).toHaveLength(1)
  })

  it('library audit: keeps a stale detach visible until GET confirms membership', async () => {
    const detached = CATEGORIES.map((category) =>
      category.id === 'communication'
        ? { ...category, lists: ['telegram'] }
        : category,
    )
    const reads: string[] = []
    const { calls } = stubAPI({
      'GET /v1/lists': () => {
        reads.push('read')
        if (reads.length === 2)
          return json({ error: 'catalog unavailable' }, 503)
        return catalogResponse(reads.length >= 3 ? detached : CATEGORIES)
      },
      'GET /v1/targets': () => json({ targets: [] }),
      'POST /v1/categories/communication/update': () =>
        json({ category: detached[0] }),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()
    await openCategory(screen, 'Communication')
    await openMenu(screen, 'Actions for list Discord')
    await screen
      .getByRole('menuitem', { name: 'Remove from the category' })
      .click()

    await expect.element(screen.getByText(STALE)).toBeVisible()
    expect(rowNames(screen)).toContain('Discord')
    expect(
      calls.filter(
        (call) => call.key === 'POST /v1/categories/communication/update',
      ),
    ).toHaveLength(1)

    await screen.getByRole('button', { name: REFRESH }).click()
    await vi.waitFor(() => {
      expect(rowNames(screen)).not.toContain('Discord')
    })
    expect(rowNames(screen)).toContain('Telegram')
    await expect.element(screen.getByText(STALE)).not.toBeInTheDocument()
    expect(
      calls.filter(
        (call) => call.key === 'POST /v1/categories/communication/update',
      ),
    ).toHaveLength(1)
  })
})
