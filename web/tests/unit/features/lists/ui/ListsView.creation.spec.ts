/** Creating a category or a list, and what belongs to which of them. */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  activeCategory,
  catalogResponse,
  catalogRoutes,
  CATEGORIES,
  CATEGORY_NAME_FIELD,
  fillField,
  json,
  LISTS,
  NEW_CATEGORY,
  NEW_LIST,
  openCategory,
  renderLibrary,
  resetLibrary,
  restoreGlobals,
  rowNames,
  stubAPI,
  UNCATEGORIZED,
} from './support/library'

describe('creating categories and lists', () => {
  beforeEach(resetLibrary)
  afterEach(restoreGlobals)

  it('creates a category and opens it', async () => {
    const created = {
      custom: true,
      id: 'custom-1234567890abcdef',
      lists: [],
      title: 'Дом',
    }
    const { keys } = stubAPI({
      ...catalogRoutes([...CATEGORIES, created]),
      'POST /v1/categories': () => json({ category: created }, 201),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    await screen.getByRole('button', { name: NEW_CATEGORY }).click()
    await fillField(
      screen.getByRole('dialog', { name: NEW_CATEGORY }),
      CATEGORY_NAME_FIELD,
      'Дом',
    )
    await screen
      .getByRole('dialog', { name: NEW_CATEGORY })
      .getByRole('button', { name: 'Create' })
      .click()

    await vi.waitFor(() => {
      expect(keys()).toEqual([
        'GET /v1/lists',
        'GET /v1/targets',
        'POST /v1/categories',
        'GET /v1/lists',
        'GET /v1/targets',
      ])
    })
    // A category made to be filled has to be the one on screen.
    await vi.waitFor(() => {
      expect(activeCategory(screen)).toBe('Дом')
    })
  })

  // The server works the overlay out itself, so an edit states the whole
  // membership the operator wants rather than the one list that moved.
  it('adds a list to a category from the whole catalog', async () => {
    const widened = CATEGORIES.map((category) =>
      category.id === 'video'
        ? { ...category, lists: ['youtube', 'steam'] }
        : category,
    )
    const { calls } = stubAPI({
      ...catalogRoutes(widened),
      'POST /v1/categories/video/update': () => json({ category: widened[1] }),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()

    await openCategory(screen, 'Video')
    await screen.getByRole('button', { name: NEW_LIST }).click()

    const sheet = screen.getByRole('dialog', { name: NEW_LIST })
    // The native radio is kept out of sight; its segment is the hit area.
    await sheet.getByText('Existing list').click()
    await sheet.getByRole('button', { name: 'Choose a list' }).click()
    await screen.getByLabelText('Search').fill('Steam')
    await screen.getByRole('option', { name: /^Steam/ }).click()
    await sheet.getByRole('button', { name: 'Add' }).click()

    await vi.waitFor(() => {
      const write = calls.find(
        (call) => call.key === 'POST /v1/categories/video/update',
      )
      expect(write?.body).toBe(JSON.stringify({ lists: ['youtube', 'steam'] }))
    })
  })

  it('creates a new list in the same sheet without rereading the whole library', async () => {
    const created = {
      id: 'custom-created-list',
      title: 'Local list',
      domains: ['local.example'],
    }
    const { calls } = stubAPI({
      ...catalogRoutes(),
      'POST /v1/lists': () => json({ list: created }, 201),
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()
    await openCategory(screen, UNCATEGORIZED)
    await screen.getByRole('button', { name: NEW_LIST }).click()

    const sheet = screen.getByRole('dialog', { name: NEW_LIST })
    await expect.element(sheet).toHaveClass('rv-dialog--sheet')
    await fillField(screen, CATEGORY_NAME_FIELD, created.title)
    await fillField(screen, 'Domains', created.domains.join('\n'))
    await sheet.getByRole('button', { name: 'Create' }).click()

    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    expect(calls.at(-1)).toEqual({
      body: JSON.stringify({ title: created.title, domains: created.domains }),
      key: 'POST /v1/lists',
    })
    expect(rowNames(screen)).toContain(created.title)
  })

  it('retries only category attachment after a list was already created', async () => {
    const created = {
      domains: ['local.example'],
      id: 'custom-created-once',
      title: 'Local list',
    }
    const catalogCreated = {
      categories: ['video'],
      custom: true,
      domains: [{ include_subdomains: true, value: 'local.example' }],
      id: created.id,
      title: created.title,
    }
    const widened = CATEGORIES.map((category) =>
      category.id === 'video'
        ? { ...category, lists: ['youtube', created.id] }
        : category,
    )
    const reads: string[] = []
    const attachments: string[] = []
    const { calls } = stubAPI({
      'GET /v1/lists': () => {
        reads.push('read')
        return catalogResponse(
          reads.length > 1 ? widened : CATEGORIES,
          reads.length > 1 ? [...LISTS, catalogCreated] : LISTS,
        )
      },
      'GET /v1/targets': () => json({ targets: [] }),
      'POST /v1/lists': () => json({ list: created }, 201),
      'POST /v1/categories/video/update': () => {
        attachments.push('attach')
        return attachments.length === 1
          ? json({ error: 'controlled attachment failure' }, 503)
          : json({ category: widened[1] })
      },
    })
    const screen = await renderLibrary()
    await expect.element(screen.getByRole('table')).toBeVisible()
    await openCategory(screen, 'Video')
    await screen.getByRole('button', { name: NEW_LIST }).click()

    const sheet = screen.getByRole('dialog', { name: NEW_LIST })
    await fillField(sheet, CATEGORY_NAME_FIELD, created.title)
    await fillField(sheet, 'Domains', 'local.example')
    await sheet.getByRole('button', { name: 'Create' }).click()

    // The list exists now, so the sheet stops offering to create it again and
    // offers only the step that failed.
    await expect
      .element(
        screen.getByText('The list was created but was not added', {
          exact: false,
        }),
      )
      .toBeVisible()
    await expect
      .element(sheet.getByLabelText(CATEGORY_NAME_FIELD))
      .not.toBeInTheDocument()

    await screen.getByRole('button', { name: 'Try adding again' }).click()

    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    expect(calls.filter((call) => call.key === 'POST /v1/lists')).toHaveLength(
      1,
    )
    expect(
      calls.filter((call) => call.key === 'POST /v1/categories/video/update'),
    ).toHaveLength(2)
    expect(rowNames(screen)).toContain(created.title)
  })
})
