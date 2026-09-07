import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'
import RvDialog from '@/shared/ui/RvDialog.vue'

import ListsView from './ListsView.vue'

const categories = [
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
const lists = [
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

function activeCategory(scope: Element): string {
  const selected = scope.querySelector(
    '.catalog-filters__categories [aria-pressed="true"]',
  )
  const trigger =
    selected ??
    scope.querySelector(
      '.catalog-filters__categories .rv-search-select__trigger',
    )
  expect(trigger).not.toBeNull()
  const copy = trigger!.cloneNode(true) as Element
  copy.querySelectorAll('small').forEach((count) => count.remove())
  return copy.textContent!.trim()
}

function json(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function catalogResponse(next = categories, nextLists = lists): Response {
  return json({
    lists: nextLists.map((list) => list.id),
    list_details: nextLists,
    categories: next,
    default_priority: nextLists.map((list) => list.id),
  })
}

function contentsResponse(listID: string): Response {
  return json({
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
}

function stubAPI(routes: Record<string, () => Response | Promise<Response>>) {
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
function catalogRoutes(after = categories): Record<string, () => Response> {
  let reads = 0
  return {
    'GET /v1/lists': () => {
      reads += 1
      return catalogResponse(reads === 1 ? categories : after)
    },
    'GET /v1/targets': () => json({ targets: [] }),
  }
}

function dialog(): HTMLElement {
  const panels = document.body.querySelectorAll<HTMLElement>('[role="dialog"]')
  const panel = panels.item(panels.length - 1)
  expect(panel).not.toBeNull()
  return panel
}

function menuItem(text: string): HTMLElement | undefined {
  const panels = document.body.querySelectorAll<HTMLElement>('[role="menu"]')
  const panel = panels.item(panels.length - 1)
  return [
    ...(panel?.querySelectorAll<HTMLElement>('[data-menu-key]') ?? []),
  ].find((item) => item.textContent?.includes(text))
}

function menuText(): string {
  const panels = document.body.querySelectorAll<HTMLElement>('[role="menu"]')
  return panels.item(panels.length - 1)?.textContent ?? ''
}

async function clickByText(scope: HTMLElement, text: string): Promise<void> {
  if (text === 'New category' && !scope.querySelector('.lists__collections')) {
    ;[...scope.querySelectorAll('button')]
      .find((button) => button.textContent?.trim() === 'Categories')
      ?.click()
    await flushPromises()
    scope = dialog()
  }
  const control = [...scope.querySelectorAll('button')].find(
    (button) =>
      button.textContent?.includes(text) ||
      button.getAttribute('aria-label') === text,
  )
  expect(control, text).toBeDefined()
  control?.click()
}

type Library = ReturnType<typeof mountLibrary>

function mountLibrary(hash = '') {
  vi.stubGlobal('useRoute', () => ({ hash }))
  vi.stubGlobal('useRouter', () => ({ replace: vi.fn() }))
  return mount(ListsView, {
    attachTo: document.body,
    global: { stubs: { RvIcon: true } },
  })
}

async function openCategories(wrapper: Library): Promise<void> {
  if (!document.querySelector('.lists__collections')) {
    await clickByText(wrapper.element as HTMLElement, 'Categories')
    await flushPromises()
  }
}
async function openCategory(wrapper: Library, label: string): Promise<void> {
  await openCategories(wrapper)
  const opener = [
    ...document.querySelectorAll<HTMLButtonElement>('.lists__category'),
  ].find((entry) => entry.textContent?.includes(label))
  expect(opener, label).toBeDefined()
  opener?.click()
  await flushPromises()
}
async function categoryText(wrapper: Library): Promise<string> {
  await openCategories(wrapper)
  const text = document.querySelector('.lists__groups')?.textContent ?? ''
  await clickByText(dialog(), 'Close')
  await flushPromises()
  return text
}
async function openMenu(wrapper: Library, label: string): Promise<void> {
  if (label.startsWith('Actions for category')) await openCategories(wrapper)
  const trigger = [
    ...document.querySelectorAll<HTMLButtonElement>('.rv-menu__trigger'),
  ].find((entry) => entry.getAttribute('aria-label') === label)
  expect(trigger, label).toBeDefined()
  trigger?.click()
  await flushPromises()
}

function openedDialogVariant(wrapper: Library): string | undefined {
  return wrapper
    .findAllComponents(RvDialog)
    .find((candidate) => candidate.props('open') === true)
    ?.props('variant')
}

describe('ListsView', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
    invalidateCatalogCache()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  // Every category, then the lists no category claims — the same column the
  // composer draws, without a checkbox, because nothing is being selected.
  it('lists every category and the lists none of them claims', async () => {
    stubAPI(catalogRoutes())
    const wrapper = mountLibrary()
    await flushPromises()

    await openCategories(wrapper)
    const rows = [...document.querySelectorAll('.lists__group')]
    expect(rows).toHaveLength(4)
    expect(rows.at(-1)?.textContent).toContain('Uncategorized')
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(0)

    await openCategory(wrapper, 'Uncategorized')
    expect(wrapper.get('.lists__pane-body').text()).toContain('Steam')
    wrapper.unmount()
  })

  it('offers category management and list creation through their owning actions', async () => {
    stubAPI(catalogRoutes())
    const wrapper = mountLibrary()
    await flushPromises()
    expect(wrapper.findAll('.lists__list-row')).toHaveLength(4)
    await clickByText(wrapper.element as HTMLElement, 'New category')
    await flushPromises()
    expect(openedDialogVariant(wrapper)).toBe('sheet')
    await clickByText(dialog(), 'Cancel')
    await flushPromises()
    await openCategory(wrapper, 'Communication')
    await clickByText(wrapper.element as HTMLElement, 'New list')
    await flushPromises()
    expect(openedDialogVariant(wrapper)).toBe('sheet')
    expect(dialog().textContent).toContain('Existing list')
    wrapper.unmount()
  })

  it('keeps category and list actions on their rows without a duplicate add action', async () => {
    stubAPI(catalogRoutes())
    const wrapper = mountLibrary()
    await flushPromises()

    // This menu belongs to an unselected built-in category. Built-in records
    // can be removed through the server overlay, but their catalog title is not
    // renamed and list creation stays at the pane footer.
    await openMenu(wrapper, 'Actions for category Video')
    expect(menuText()).toContain('Delete the category')
    expect(menuText()).not.toContain('Rename')
    expect(menuText()).not.toContain('Add a list')

    await openCategory(wrapper, 'Communication')
    await openMenu(wrapper, 'Actions for list Discord')
    const builtIn = menuText()
    expect(builtIn).toContain('Remove from the category')
    expect(builtIn).toContain('Delete the list')
    expect(builtIn).not.toContain('Rename')

    await openMenu(wrapper, 'Actions for list Telegram')
    const custom = menuText()
    expect(custom).toContain('Rename')
    expect(custom).toContain('Remove from the category')
    expect(custom).toContain('Delete the list')
    wrapper.unmount()
  })

  it('acts on an unselected category without moving the current pane', async () => {
    const remaining = categories.filter((category) => category.id !== 'video')
    const { keys } = stubAPI({
      ...catalogRoutes(remaining),
      'POST /v1/categories/video/remove': () =>
        new Response(null, { status: 204 }),
    })
    const wrapper = mountLibrary()
    await flushPromises()
    expect(activeCategory(wrapper.element)).toBe('All categories')

    await openMenu(wrapper, 'Actions for category Video')
    menuItem('Delete the category')?.click()
    await flushPromises()
    await clickByText(dialog(), 'Delete')
    await flushPromises()

    expect(keys().at(-3)).toBe('POST /v1/categories/video/remove')
    expect(activeCategory(wrapper.element)).toBe('All categories')
    wrapper.unmount()
  })

  it('opens a custom list rename directly from its row menu', async () => {
    stubAPI({
      ...catalogRoutes(),
      'GET /v1/lists/telegram/contents': () => contentsResponse('telegram'),
    })
    const wrapper = mountLibrary()
    await flushPromises()

    await openCategory(wrapper, 'Communication')
    await openMenu(wrapper, 'Actions for list Telegram')
    menuItem('Rename')?.click()
    await flushPromises()

    expect(dialog().textContent).toContain('Telegram')
    expect(dialog().querySelector<HTMLInputElement>('#list-title')?.value).toBe(
      'Telegram',
    )
    wrapper.unmount()
  })

  it('creates a category and opens it', async () => {
    const created = {
      custom: true,
      id: 'custom-1234567890abcdef',
      lists: [],
      title: 'Дом',
    }
    const { keys } = stubAPI({
      ...catalogRoutes([...categories, created]),
      'POST /v1/categories': () => json({ category: created }, 201),
    })
    const wrapper = mountLibrary()
    await flushPromises()

    await clickByText(wrapper.element as HTMLElement, 'New category')
    await flushPromises()
    const panel = dialog()
    const field = panel.querySelector<HTMLInputElement>('#lists-category-title')
    expect(field).not.toBeNull()
    field!.value = 'Дом'
    field!.dispatchEvent(new Event('input'))
    await clickByText(panel, 'Create')
    await flushPromises()

    expect(keys()).toEqual([
      'GET /v1/lists',
      'GET /v1/targets',
      'POST /v1/categories',
      'GET /v1/lists',
      'GET /v1/targets',
    ])
    // A category made to be filled has to be the one on screen.
    expect(activeCategory(wrapper.element)).toBe('Дом')
    wrapper.unmount()
  })

  // The server works the overlay out itself, so an edit states the whole
  // membership the operator wants rather than the one list that moved.
  it('adds a list to a category from the whole catalog', async () => {
    const widened = categories.map((category) =>
      category.id === 'video'
        ? { ...category, lists: ['youtube', 'steam'] }
        : category,
    )
    const { calls } = stubAPI({
      ...catalogRoutes(widened),
      'POST /v1/categories/video/update': () => json({ category: widened[1] }),
    })
    const wrapper = mountLibrary()
    await flushPromises()

    await openCategory(wrapper, 'Video')
    await clickByText(wrapper.element as HTMLElement, 'New list')
    await flushPromises()

    const panel = dialog()
    const existing = [
      ...panel.querySelectorAll<HTMLLabelElement>('.rv-segmented__option'),
    ].find((option) => option.textContent?.includes('Existing list'))
    expect(existing).toBeDefined()
    existing?.querySelector('input')?.click()
    await flushPromises()
    panel
      .querySelector<HTMLButtonElement>('.rv-search-select__trigger')!
      .click()
    await flushPromises()
    const field = document.querySelector<HTMLInputElement>(
      '.rv-search-select__search input',
    )
    expect(field).not.toBeNull()
    field!.focus()
    field!.value = 'Steam'
    field!.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    for (const key of ['ArrowDown', 'Enter']) {
      field!.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key }))
      await flushPromises()
    }
    await clickByText(dialog(), 'Add')
    await flushPromises()

    expect(calls.at(-3)?.key).toBe('POST /v1/categories/video/update')
    expect(calls.at(-3)?.body).toBe(
      JSON.stringify({ lists: ['youtube', 'steam'] }),
    )
    wrapper.unmount()
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
    const wrapper = mountLibrary()
    await flushPromises()
    await openCategory(wrapper, 'Uncategorized')
    await clickByText(wrapper.element as HTMLElement, 'New list')
    await flushPromises()

    const panel = dialog()
    expect(openedDialogVariant(wrapper)).toBe('sheet')
    const title = panel.querySelector<HTMLInputElement>('#library-list-title')
    const domains = panel.querySelector<HTMLTextAreaElement>(
      '#library-list-domains',
    )
    expect(title).not.toBeNull()
    expect(domains).not.toBeNull()
    title!.value = created.title
    title!.dispatchEvent(new Event('input', { bubbles: true }))
    domains!.value = created.domains.join('\n')
    domains!.dispatchEvent(new Event('input', { bubbles: true }))
    await clickByText(panel, 'Create')
    await flushPromises()

    expect(calls.at(-1)).toEqual({
      body: JSON.stringify({ title: created.title, domains: created.domains }),
      key: 'POST /v1/lists',
    })
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(wrapper.get('.lists__pane-body').text()).toContain(created.title)
    wrapper.unmount()
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
    const widened = categories.map((category) =>
      category.id === 'video'
        ? { ...category, lists: ['youtube', created.id] }
        : category,
    )
    let reads = 0
    let attachments = 0
    const { calls } = stubAPI({
      'GET /v1/lists': () => {
        reads += 1
        return catalogResponse(
          reads > 1 ? widened : categories,
          reads > 1 ? [...lists, catalogCreated] : lists,
        )
      },
      'GET /v1/targets': () => json({ targets: [] }),
      'POST /v1/lists': () => json({ list: created }, 201),
      'POST /v1/categories/video/update': () => {
        attachments += 1
        return attachments === 1
          ? json({ error: 'controlled attachment failure' }, 503)
          : json({ category: widened[1] })
      },
    })
    const wrapper = mountLibrary()
    await flushPromises()
    await openCategory(wrapper, 'Video')
    await clickByText(wrapper.element as HTMLElement, 'New list')
    await flushPromises()

    const panel = dialog()
    const title = panel.querySelector<HTMLInputElement>('#library-list-title')
    const domains = panel.querySelector<HTMLTextAreaElement>(
      '#library-list-domains',
    )
    expect(title).not.toBeNull()
    expect(domains).not.toBeNull()
    title!.value = created.title
    title!.dispatchEvent(new Event('input', { bubbles: true }))
    domains!.value = 'local.example'
    domains!.dispatchEvent(new Event('input', { bubbles: true }))
    await clickByText(panel, 'Create')
    await flushPromises()

    expect(dialog().textContent).toContain(
      'The list was created but was not added to the category.',
    )
    expect(
      dialog().querySelector<HTMLInputElement>('#library-list-title'),
    ).toBeNull()
    await clickByText(dialog(), 'Try adding again')
    await flushPromises()

    expect(calls.filter((call) => call.key === 'POST /v1/lists')).toHaveLength(
      1,
    )
    expect(
      calls.filter((call) => call.key === 'POST /v1/categories/video/update'),
    ).toHaveLength(2)
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(wrapper.get('.lists__pane-body').text()).toContain(created.title)
    wrapper.unmount()
  })

  it('saves the library default order without writing any profile', async () => {
    const saved = ['telegram', 'discord', 'youtube', 'steam']
    const { calls } = stubAPI({
      ...catalogRoutes(),
      'POST /v1/lists/priority': () => json({ default_priority: saved }),
    })
    const wrapper = mountLibrary()
    await flushPromises()

    const panel = wrapper.element as HTMLElement
    const handles = panel.querySelectorAll<HTMLButtonElement>('.lists__handle')
    expect(handles).toHaveLength(4)
    handles[0]?.dispatchEvent(
      new KeyboardEvent('keydown', { bubbles: true, key: 'ArrowDown' }),
    )
    await flushPromises()
    await clickByText(panel, 'Save order')
    await flushPromises()

    expect(calls.at(-1)).toEqual({
      body: JSON.stringify({ default_priority: saved }),
      key: 'POST /v1/lists/priority',
    })
    expect(calls.some((call) => call.key.includes('/v1/profiles'))).toBe(false)
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    wrapper.unmount()
  })

  it('keeps a failed default order available to retry', async () => {
    const { calls } = stubAPI({
      ...catalogRoutes(),
      'POST /v1/lists/priority': () =>
        json({ error: 'controlled failure' }, 503),
    })
    const wrapper = mountLibrary()
    await flushPromises()

    const panel = wrapper.element as HTMLElement
    panel
      .querySelector<HTMLButtonElement>('.lists__handle')
      ?.dispatchEvent(
        new KeyboardEvent('keydown', { bubbles: true, key: 'ArrowDown' }),
      )
    await flushPromises()
    await clickByText(panel, 'Save order')
    await flushPromises()

    expect(panel.isConnected).toBe(true)
    expect(panel.textContent).toContain('Order not saved')
    expect(
      calls.filter((call) => call.key.includes('/v1/profiles')),
    ).toHaveLength(0)
    wrapper.unmount()
  })

  /**
   * Deleting a category asks the one question it has to ask, and the answer is
   * the request: the lists move to «Без категории», or they go with it.
   */
  it.each([
    ['Move them to Uncategorized', 'detach'],
    ['Delete them with it', 'delete'],
  ])('deletes a category, %s', async (choice, disposition) => {
    const { calls, keys } = stubAPI({
      ...catalogRoutes(categories.filter((entry) => entry.custom !== true)),
      'POST /v1/categories/custom-home/remove': () =>
        new Response(null, { status: 204 }),
    })
    const wrapper = mountLibrary()
    await flushPromises()

    await openCategory(wrapper, 'Домашние')
    await openMenu(wrapper, 'Actions for category Домашние')
    menuItem('Delete the category')?.click()
    await flushPromises()

    const panel = dialog()
    expect(panel.textContent).toContain('What happens to its lists')
    const answer = [
      ...panel.querySelectorAll<HTMLLabelElement>('.rv-segmented__option'),
    ].find((option) => option.textContent?.includes(choice))
    expect(answer, choice).toBeDefined()
    answer?.querySelector('input')?.click()
    await flushPromises()
    await clickByText(panel, 'Delete')
    await flushPromises()

    expect(keys().at(-3)).toBe('POST /v1/categories/custom-home/remove')
    expect(calls.at(-3)?.body).toBe(JSON.stringify({ lists: disposition }))
    expect(await categoryText(wrapper)).not.toContain('Домашние')
    wrapper.unmount()
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
    const wrapper = mountLibrary()
    await flushPromises()

    await openCategory(wrapper, 'Домашние')
    await openMenu(wrapper, 'Actions for category Домашние')
    menuItem('Delete the category')?.click()
    await flushPromises()
    await clickByText(dialog(), 'Delete')
    await flushPromises()

    const panel = dialog()
    expect(panel.textContent).toContain('The category was not deleted')
    expect(panel.textContent).toContain('Дом, Офис')
    expect(await categoryText(wrapper)).toContain('Домашние')
    wrapper.unmount()
  })

  // The bin means deletion and nothing else now: leaving a category is a menu
  // item with words on it.
  it('takes a list out of its category from the row menu', async () => {
    const trimmed = categories.map((category) =>
      category.id === 'communication'
        ? { ...category, lists: ['telegram'] }
        : category,
    )
    const { calls } = stubAPI({
      ...catalogRoutes(trimmed),
      'POST /v1/categories/communication/update': () =>
        json({ category: trimmed[0] }),
    })
    const wrapper = mountLibrary()
    await flushPromises()

    await openCategory(wrapper, 'Communication')
    await openMenu(wrapper, 'Actions for list Discord')
    menuItem('Remove from the category')?.click()
    await flushPromises()

    expect(calls.at(-3)?.key).toBe('POST /v1/categories/communication/update')
    expect(calls.at(-3)?.body).toBe(JSON.stringify({ lists: ['telegram'] }))
    wrapper.unmount()
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
    const wrapper = mountLibrary()
    await flushPromises()

    await openCategory(wrapper, 'Communication')
    await openMenu(wrapper, 'Actions for list Telegram')
    menuItem('Delete the list')?.click()
    await flushPromises()
    const panel = dialog()
    expect(panel.textContent).toContain(
      'List “Telegram” and its entries are removed.',
    )
    await clickByText(panel, 'Delete')
    await flushPromises()

    expect(keys().at(-1)).toBe('POST /v1/lists/telegram/remove')
    expect(dialog().textContent).toContain('The list was not deleted')
    expect(dialog().textContent).toContain('Дом')

    // The one nothing holds goes, and the catalog is read back after it.
    await clickByText(dialog(), 'Cancel')
    await flushPromises()
    await openCategory(wrapper, 'Uncategorized')
    await openMenu(wrapper, 'Actions for list Steam')
    menuItem('Delete the list')?.click()
    await flushPromises()
    await clickByText(dialog(), 'Delete')
    await flushPromises()

    expect(calls.at(-3)?.key).toBe('POST /v1/lists/steam/remove')
    expect(keys().slice(-2)).toEqual(['GET /v1/lists', 'GET /v1/targets'])
    wrapper.unmount()
  })

  it('blocks every conflicting library action while a deletion is pending', async () => {
    let finish!: (response: Response) => void
    const pending = new Promise<Response>((resolve) => {
      finish = resolve
    })
    const { keys } = stubAPI({
      ...catalogRoutes(),
      'POST /v1/lists/telegram/remove': () => pending,
    })
    const wrapper = mountLibrary()
    await flushPromises()

    await openCategory(wrapper, 'Communication')
    await openMenu(wrapper, 'Actions for list Telegram')
    menuItem('Delete the list')?.click()
    await flushPromises()
    const panel = dialog()
    await clickByText(panel, 'Delete')
    await flushPromises()

    const submit = [
      ...panel.querySelectorAll<HTMLButtonElement>('button'),
    ].find((button) => button.textContent?.includes('Delete'))
    expect(submit?.disabled).toBe(true)
    expect(submit?.getAttribute('aria-busy')).toBe('true')
    expect(
      panel.querySelector<HTMLButtonElement>('.rv-dialog__close')?.disabled,
    ).toBe(true)
    expect(
      wrapper
        .findAll<HTMLButtonElement>('.rv-menu__trigger')
        .every((trigger) => trigger.element.disabled),
    ).toBe(true)
    submit?.click()
    expect(
      keys().filter((key) => key === 'POST /v1/lists/telegram/remove'),
    ).toHaveLength(1)

    finish(json({ error: 'controlled refusal' }, 503))
    await flushPromises()
    expect(submit?.disabled).toBe(false)
    expect(panel.isConnected).toBe(true)
    wrapper.unmount()
  })

  // The address states where the operator is, so the composing card's link
  // lands on the category and the list it named.
  it('opens the category and the list named in the address', async () => {
    stubAPI({
      ...catalogRoutes(),
      'GET /v1/lists/youtube/contents': () => contentsResponse('youtube'),
    })
    const wrapper = mountLibrary('#category=video&list=youtube')
    await flushPromises()

    expect(activeCategory(wrapper.element)).toBe('Video')
    const card = dialog()
    expect(card.textContent).toContain('YouTube')
    expect(card.textContent).toContain('youtube.example')
    // No profile is in question here, so the card asks about none.
    expect(card.querySelector('.list-card__membership')).toBeNull()
    wrapper.unmount()
  })

  it('library audit: keeps a committed create stale until a GET-only retry confirms its row', async () => {
    const created = {
      custom: true,
      id: 'custom-stale-create',
      lists: [],
      title: 'Saved while offline',
    }
    let reads = 0
    const { calls } = stubAPI({
      'GET /v1/lists': () => {
        reads += 1
        if (reads === 2) return json({ error: 'catalog unavailable' }, 503)
        return catalogResponse(
          reads >= 3 ? [...categories, created] : categories,
        )
      },
      'GET /v1/targets': () => json({ targets: [] }),
      'POST /v1/categories': () => json({ category: created }, 201),
    })
    const wrapper = mountLibrary()
    await flushPromises()

    await clickByText(wrapper.element as HTMLElement, 'New category')
    await flushPromises()
    const panel = dialog()
    const field = panel.querySelector<HTMLInputElement>('#lists-category-title')
    expect(field).not.toBeNull()
    field!.value = created.title
    field!.dispatchEvent(new Event('input', { bubbles: true }))
    await clickByText(panel, 'Create')
    await flushPromises()

    // The POST landed exactly once, but the retained copy cannot contain the
    // new row. The form is closed so its old input is not a resubmit prompt.
    expect(
      calls.filter((call) => call.key === 'POST /v1/categories'),
    ).toHaveLength(1)
    expect(panel.isConnected).toBe(false)
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(wrapper.text()).toContain('Saved, but the lists were not reread')
    expect(activeCategory(wrapper.element)).toBe('All categories')
    expect(wrapper.text()).not.toContain('Saved while offline')

    // Recovery is only GET. Once it confirms the category, the saved identity
    // completes selection; no second POST is sent.
    await clickByText(wrapper.element as HTMLElement, 'Refresh lists')
    await flushPromises()
    expect(
      calls.filter((call) => call.key === 'POST /v1/categories'),
    ).toHaveLength(1)
    expect(activeCategory(wrapper.element)).toBe('Saved while offline')
    expect(wrapper.text()).not.toContain('Saved, but the lists were not reread')
    wrapper.unmount()
  })

  it('library audit: preserves the category input when the write itself fails', async () => {
    const { calls } = stubAPI({
      ...catalogRoutes(),
      'POST /v1/categories': () =>
        json({ error: 'catalog unavailable while writing' }, 503),
    })
    const wrapper = mountLibrary()
    await flushPromises()

    await clickByText(wrapper.element as HTMLElement, 'New category')
    await flushPromises()
    const panel = dialog()
    const field = panel.querySelector<HTMLInputElement>('#lists-category-title')
    expect(field).not.toBeNull()
    field!.value = 'Keep this draft'
    field!.dispatchEvent(new Event('input', { bubbles: true }))
    await clickByText(panel, 'Create')
    await flushPromises()

    expect(
      calls.filter((call) => call.key === 'POST /v1/categories'),
    ).toHaveLength(1)
    expect(
      dialog().querySelector<HTMLInputElement>('#lists-category-title')?.value,
    ).toBe('Keep this draft')
    expect(wrapper.text()).not.toContain('Saved, but the lists were not reread')
    wrapper.unmount()
  })

  it('library audit: closes a committed stale rename and updates it on GET retry', async () => {
    const renamed = categories.map((category) =>
      category.id === 'custom-home'
        ? { ...category, title: 'Renamed later' }
        : category,
    )
    let reads = 0
    const { calls } = stubAPI({
      'GET /v1/lists': () => {
        reads += 1
        if (reads === 2) return json({ error: 'catalog unavailable' }, 503)
        return catalogResponse(reads >= 3 ? renamed : categories)
      },
      'GET /v1/targets': () => json({ targets: [] }),
      'POST /v1/categories/custom-home/update': () =>
        json({ category: renamed[2] }),
    })
    const wrapper = mountLibrary()
    await flushPromises()
    await openCategory(wrapper, 'Домашние')
    await openMenu(wrapper, 'Actions for category Домашние')
    menuItem('Rename')?.click()
    await flushPromises()
    const panel = dialog()
    const field = panel.querySelector<HTMLInputElement>('#lists-category-title')
    expect(field).not.toBeNull()
    field!.value = 'Renamed later'
    field!.dispatchEvent(new Event('input', { bubbles: true }))
    await clickByText(panel, 'Save')
    await flushPromises()

    expect(panel.isConnected).toBe(false)
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(activeCategory(wrapper.element)).toBe('Домашние')
    expect(wrapper.text()).toContain('Saved, but the lists were not reread')
    expect(
      calls.filter(
        (call) => call.key === 'POST /v1/categories/custom-home/update',
      ),
    ).toHaveLength(1)

    await clickByText(wrapper.element as HTMLElement, 'Refresh lists')
    await flushPromises()
    expect(activeCategory(wrapper.element)).toBe('Renamed later')
    expect(wrapper.text()).not.toContain('Saved, but the lists were not reread')
    expect(
      calls.filter(
        (call) => call.key === 'POST /v1/categories/custom-home/update',
      ),
    ).toHaveLength(1)
    wrapper.unmount()
  })

  it('library audit: closes a committed stale category deletion on a safe row', async () => {
    let reads = 0
    const remaining = categories.filter((entry) => entry.id !== 'custom-home')
    const { calls } = stubAPI({
      'GET /v1/lists': () => {
        reads += 1
        if (reads === 2) return json({ error: 'catalog unavailable' }, 503)
        return catalogResponse(reads >= 3 ? remaining : categories)
      },
      'GET /v1/targets': () => json({ targets: [] }),
      'POST /v1/categories/custom-home/remove': () =>
        new Response(null, { status: 204 }),
    })
    const wrapper = mountLibrary()
    await flushPromises()
    await openCategory(wrapper, 'Домашние')
    await openMenu(wrapper, 'Actions for category Домашние')
    menuItem('Delete the category')?.click()
    await flushPromises()
    const panel = dialog()
    expect(panel.isConnected).toBe(true)
    await clickByText(panel, 'Delete')
    await flushPromises()

    expect(panel.isConnected).toBe(false)
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(activeCategory(wrapper.element)).toBe('Uncategorized')
    expect(await categoryText(wrapper)).toContain('Домашние')
    expect(wrapper.text()).toContain('Saved, but the lists were not reread')
    expect(
      calls.filter(
        (call) => call.key === 'POST /v1/categories/custom-home/remove',
      ),
    ).toHaveLength(1)

    await clickByText(wrapper.element as HTMLElement, 'Refresh lists')
    await flushPromises()
    expect(activeCategory(wrapper.element)).toBe('Uncategorized')
    expect(await categoryText(wrapper)).not.toContain('Домашние')
    expect(wrapper.text()).not.toContain('Saved, but the lists were not reread')
    expect(
      calls.filter(
        (call) => call.key === 'POST /v1/categories/custom-home/remove',
      ),
    ).toHaveLength(1)
    wrapper.unmount()
  })

  it('library audit: keeps a stale detach visible until GET confirms membership', async () => {
    const detached = categories.map((category) =>
      category.id === 'communication'
        ? { ...category, lists: ['telegram'] }
        : category,
    )
    let reads = 0
    const { calls } = stubAPI({
      'GET /v1/lists': () => {
        reads += 1
        if (reads === 2) return json({ error: 'catalog unavailable' }, 503)
        return catalogResponse(reads >= 3 ? detached : categories)
      },
      'GET /v1/targets': () => json({ targets: [] }),
      'POST /v1/categories/communication/update': () =>
        json({ category: detached[0] }),
    })
    const wrapper = mountLibrary()
    await flushPromises()
    await openCategory(wrapper, 'Communication')
    await openMenu(wrapper, 'Actions for list Discord')
    menuItem('Remove from the category')?.click()
    await flushPromises()

    expect(wrapper.get('.lists__pane-body').text()).toContain('Discord')
    expect(wrapper.text()).toContain('Saved, but the lists were not reread')
    expect(
      calls.filter(
        (call) => call.key === 'POST /v1/categories/communication/update',
      ),
    ).toHaveLength(1)

    await clickByText(wrapper.element as HTMLElement, 'Refresh lists')
    await flushPromises()
    expect(wrapper.get('.lists__pane-body').text()).not.toContain('Discord')
    expect(wrapper.get('.lists__pane-body').text()).toContain('Telegram')
    expect(wrapper.text()).not.toContain('Saved, but the lists were not reread')
    expect(
      calls.filter(
        (call) => call.key === 'POST /v1/categories/communication/update',
      ),
    ).toHaveLength(1)
    wrapper.unmount()
  })
})
