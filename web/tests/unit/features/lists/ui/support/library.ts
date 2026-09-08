/**
 * Rendering the library pane and reading it the way an operator does.
 *
 * The `ListsView.*.spec.ts` files beside this one each cover one area of the
 * surface — what it draws, creating, ordering, removing, and recovering an
 * interrupted write. The catalog they all start from, the stubbed API and the
 * queries that locate a category, a row or a menu live here, so an area states
 * only the behaviour it is about.
 */
import { expect, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render, type RenderResult } from 'vitest-browser-vue'

import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

import ListsView from '@/features/lists/ui/ListsView.vue'

/** Registered by each spec: English, and a catalog nothing has read yet. */
export const resetLibrary = (): void => {
  useLocale().setLocale('en')
  invalidateCatalogCache()
}

/** Registered by each spec: the globals a case stubbed go back as they were. */
export const restoreGlobals = (): void => {
  vi.unstubAllGlobals()
}

export const CATEGORIES_DIALOG = 'Categories'
export const NEW_CATEGORY = 'New category'
export const NEW_LIST = 'New list'
export const REFRESH = 'Refresh lists'
export const STALE = 'Saved, but the lists were not reread'
export const ALL_CATEGORIES = 'All categories'
export const UNCATEGORIZED = 'Uncategorized'
export const CATEGORY_NAME_FIELD = 'Name'

export const CATEGORIES = [
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
export const LISTS = [
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

export const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

export const catalogResponse = (
  next = CATEGORIES,
  nextLists = LISTS,
): Response =>
  json({
    lists: nextLists.map((list) => list.id),
    list_details: nextLists,
    categories: next,
    default_priority: nextLists.map((list) => list.id),
  })

export const contentsResponse = (listID: string): Response =>
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

export const stubAPI = (
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
export const catalogRoutes = (
  after = CATEGORIES,
): Record<string, () => Response> => {
  const reads: string[] = []
  return {
    'GET /v1/lists': () => {
      reads.push('read')
      return catalogResponse(reads.length === 1 ? CATEGORIES : after)
    },
    'GET /v1/targets': () => json({ targets: [] }),
  }
}

export const renderLibrary = (hash = '') => {
  vi.stubGlobal('useRoute', () => ({ hash }))
  vi.stubGlobal('useRouter', () => ({ replace: vi.fn() }))
  return render(ListsView)
}

export type Library = RenderResult<unknown>

// A category is drawn as its name followed by how many lists it holds, and the
// count is not part of the name, so the name is what stands before the figure.
export const beforeCount = (text: string): string => {
  const figure = text.search(/\d/)
  return (figure < 0 ? text : text.slice(0, figure)).trim()
}

// Which category the pane is showing. Exactly one filter chip is pressed at a
// time, and that chip is the answer.
export const activeCategory = (screen: Library): string =>
  beforeCount(
    screen.getByRole('button', { pressed: true }).element().textContent ?? '',
  )

export const rowNames = (screen: Library): string[] =>
  screen
    .getByRole('rowheader')
    .elements()
    .map((header) => header.textContent?.trim() ?? '')

export const openCategories = async (screen: Library): Promise<void> => {
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
export const closeMenu = async (screen: Library): Promise<void> => {
  await userEvent.keyboard('{Escape}')
  await expect.element(screen.getByRole('menu')).not.toBeInTheDocument()
}

export const openCategory = async (
  screen: Library,
  label: string,
): Promise<void> => {
  await openCategories(screen)
  await screen
    .getByRole('dialog', { name: CATEGORIES_DIALOG })
    .getByRole('button', { name: new RegExp(`^${label}`) })
    .click()
}

// Reading the category pane closes it again, because it is a modal panel and
// the pane behind it is what the rest of a case is about.
export const categoryNames = async (screen: Library): Promise<string[]> => {
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

export const openMenu = async (
  screen: Library,
  label: string,
): Promise<void> => {
  if (label.startsWith('Actions for category')) await openCategories(screen)
  await screen.getByRole('button', { name: label }).click()
}

// An open panel is modal, so the page behind it is hidden from assistive
// technology while it stands; a control read during that time needs a query
// that reaches hidden content.
export const hiddenButton = (screen: Library, name: string | RegExp) =>
  screen.getByRole('button', { includeHidden: true, name })

// A field is filled inside the panel that asked for it: two panels can be
// standing, and both name their own field the same thing.
export const fillField = async (
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
