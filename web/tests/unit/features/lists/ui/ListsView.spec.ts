import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render, type RenderResult } from 'vitest-browser-vue'

import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

import ListsView from '@/features/lists/ui/ListsView.vue'

const CATEGORIES_DIALOG = 'Categories'
const NEW_CATEGORY = 'New category'
const NEW_LIST = 'New list'
const REFRESH = 'Refresh lists'
const STALE = 'Saved, but the lists were not reread'
const ALL_CATEGORIES = 'All categories'
const UNCATEGORIZED = 'Uncategorized'
const CATEGORY_NAME_FIELD = 'Name'

const CATEGORIES = [
  {
    custom: false,
    id: 'communication',
    lists: ['discord', 'telegram'],
    title: 'Общение',
  },
  { custom: false, id: 'video', lists: ['youtube'], title: 'Видео' },
  { custom: true, id: 'custom-home', lists: ['youtube'], title: 'Домашние' },
]

// Steam belongs to no category, which is what fills the last row.
const LISTS = [
  { categories: ['communication'], id: 'discord', title: 'Discord' },
  {
    categories: ['communication'],
    custom: true,
    id: 'telegram',
    title: 'Telegram',
  },
  { categories: ['custom-home', 'video'], id: 'youtube', title: 'YouTube' },
  { categories: [], custom: true, id: 'steam', title: 'Steam' },
]

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const catalogResponse = (next = CATEGORIES, nextLists = LISTS): Response =>
  json({
    lists: nextLists.map((list) => list.id),
    list_details: nextLists,
    categories: next,
    default_priority: nextLists.map((list) => list.id),
  })

const contentsResponse = (listID: string): Response =>
  json({
    list_id: listID,
    rows: [
      {
        value: `${listID}.example`,
        kind: 'domain',
        origin: 'catalog',
        enabled: true,
      },
    ],
    sources: [],
    observed: true,
  })

const stubAPI = (
  routes: Record<string, () => Response | Promise<Response>>,
) => {
  const calls: { body: string | null; key: string }[] = []
  const fetchMock = vi.fn((input: unknown, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${String(input)}`
    calls.push({ body: (init?.body as string | undefined) ?? null, key })
    const handler = routes[key]
    return Promise.resolve(
      handler === undefined ? json({ error: 'unrouted' }, 500) : handler(),
    )
  })
  vi.stubGlobal('fetch', fetchMock)
  return { calls, keys: () => calls.map((call) => call.key) }
}

/**
 * The catalog every test starts from, and what the read after a write answers
 * with. The first read is always the catalog as it stood, so a test states the
 * result of its own edit rather than the state it wanted to start in.
 */
const catalogRoutes = (after = CATEGORIES): Record<string, () => Response> => {
  const reads: string[] = []
  return {
    'GET /v1/lists': () => {
      reads.push('read')
      return catalogResponse(reads.length === 1 ? CATEGORIES : after)
    },
    'GET /v1/targets': () => json({ targets: [] }),
  }
}

const renderLibrary = (hash = '') => {
  vi.stubGlobal('useRoute', () => ({ hash }))
  vi.stubGlobal('useRouter', () => ({ replace: vi.fn() }))
  return render(ListsView)
}

type Library = RenderResult<unknown>

// A category is drawn as its name followed by how many lists it holds, and the
// count is not part of the name, so the name is what stands before the figure.
const beforeCount = (text: string): string => {
  const figure = text.search(/\d/)
  return (figure < 0 ? text : text.slice(0, figure)).trim()
}

// Which category the pane is showing. Exactly one filter chip is pressed at a
// time, and that chip is the answer.
const activeCategory = (screen: Library): string =>
  beforeCount(
    screen.getByRole('button', { pressed: true }).element().textContent ?? '',
  )

const rowNames = (screen: Library): string[] =>
  screen
    .getByRole('rowheader')
    .elements()
    .map((header) => header.textContent?.trim() ?? '')

const openCategories = async (screen: Library): Promise<void> => {
  const standing = screen.getByRole('dialog', {
    includeHidden: true,
    name: CATEGORIES_DIALOG,
  })
  if (standing.query() !== null) return
  await screen.getByRole('button', { name: CATEGORIES_DIALOG }).click()
  await expect
    .element(screen.getByRole('dialog', { name: CATEGORIES_DIALOG }))
    .toBeVisible()
}

// Leaving a menu is a step of its own: the panel behind it only takes the
// pointer again once the menu has actually gone.
const closeMenu = async (screen: Library): Promise<void> => {
  await userEvent.keyboard('{Escape}')
  await expect.element(screen.getByRole('menu')).not.toBeInTheDocument()
}

const openCategory = async (screen: Library, label: string): Promise<void> => {
  await openCategories(screen)
  await screen
    .getByRole('dialog', { name: CATEGORIES_DIALOG })
    .getByRole('button', { name: new RegExp(`^${label}`) })
    .click()
}

// Reading the category pane closes it again, because it is a modal panel and
// the pane behind it is what the rest of a case is about.
const categoryNames = async (screen: Library): Promise<string[]> => {
  await openCategories(screen)
  const dialog = screen.getByRole('dialog', { name: CATEGORIES_DIALOG })
  const names = dialog
    .getByRole('listitem')
    .elements()
    .map((entry) => beforeCount(entry.textContent ?? ''))
  await dialog.getByRole('button', { name: 'Close' }).click()
  await expect.element(dialog).not.toBeInTheDocument()
  return names
}

const openMenu = async (screen: Library, label: string): Promise<void> => {
  if (label.startsWith('Actions for category')) await openCategories(screen)
  await screen.getByRole('button', { name: label }).click()
}

// An open panel is modal, so the page behind it is hidden from assistive
// technology while it stands; a control read during that time needs a query
// that reaches hidden content.
const hiddenButton = (screen: Library, name: string | RegExp) =>
  screen.getByRole('button', { includeHidden: true, name })

// A field is filled inside the panel that asked for it: two panels can be
// standing, and both name their own field the same thing.
const fillField = async (
  scope: {
    getByLabelText: (label: string) => {
      fill: (value: string) => Promise<void>
    }
  },
  label: string,
  value: string,
): Promise<void> => {
  await scope.getByLabelText(label).fill(value)
}

describe('ListsView', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
    invalidateCatalogCache()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

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

    await openCategories(screen)
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
    await userEvent.keyboard('{ArrowDown}')
    await userEvent.keyboard('{Enter}')
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

    await openCategories(screen)
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

    await openCategories(screen)
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
