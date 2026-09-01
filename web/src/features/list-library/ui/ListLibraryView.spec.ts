import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

import ListLibraryView from './ListLibraryView.vue'

const categories = [
  {
    custom: false,
    id: 'communication',
    services: ['discord', 'telegram'],
    title: 'Общение',
  },
  { custom: false, id: 'video', services: ['youtube'], title: 'Видео' },
  { custom: true, id: 'custom-home', services: ['youtube'], title: 'Домашние' },
]

// Steam belongs to no category, which is what fills the last row.
const services = [
  { categories: ['communication'], id: 'discord', title: 'Discord' },
  { categories: ['communication'], id: 'telegram', title: 'Telegram' },
  { categories: ['custom-home', 'video'], id: 'youtube', title: 'YouTube' },
  { categories: [], id: 'steam', title: 'Steam' },
]

function json(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function catalogResponse(next = categories): Response {
  return json({
    services: services.map((service) => service.id),
    service_details: services.map((service) => ({
      categories: service.categories,
      id: service.id,
      title: service.title,
    })),
    categories: next,
  })
}

function contentsResponse(serviceID: string): Response {
  return json({
    service_id: serviceID,
    rows: [
      {
        value: `${serviceID}.example`,
        kind: 'domain',
        origin: 'catalog',
        enabled: true,
      },
    ],
    sources: [],
    observed: true,
  })
}

function stubAPI(routes: Record<string, () => Response>) {
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
    'GET /v1/services': () => {
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

function clickByText(scope: HTMLElement, text: string): void {
  const control = [...scope.querySelectorAll('button')].find((button) =>
    button.textContent?.includes(text),
  )
  expect(control, text).toBeDefined()
  control?.click()
}

type Library = ReturnType<typeof mountLibrary>

function mountLibrary(hash = '') {
  vi.stubGlobal('useRoute', () => ({ hash }))
  vi.stubGlobal('useRouter', () => ({ replace: vi.fn() }))
  return mount(ListLibraryView, {
    attachTo: document.body,
    global: { stubs: { RvIcon: true } },
  })
}

async function openCategory(wrapper: Library, label: string): Promise<void> {
  const opener = wrapper
    .findAll('.lists__category')
    .find((entry) => entry.text().includes(label))
  expect(opener, label).toBeDefined()
  await opener?.trigger('click')
  await flushPromises()
}

async function openMenu(wrapper: Library, label: string): Promise<void> {
  const trigger = wrapper
    .findAll('.rv-menu__trigger')
    .find((entry) => entry.attributes('aria-label') === label)
  expect(trigger, label).toBeDefined()
  await trigger?.trigger('click')
  await flushPromises()
}

describe('ListLibraryView', () => {
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

    const rows = wrapper.findAll('.lists__group')
    expect(rows).toHaveLength(4)
    expect(rows.at(-1)?.text()).toContain('Uncategorized')
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(0)

    await openCategory(wrapper, 'Uncategorized')
    expect(wrapper.get('.lists__pane-body').text()).toContain('Steam')
    wrapper.unmount()
  })

  it('creates a category and opens it', async () => {
    const created = {
      custom: true,
      id: 'custom-1234567890abcdef',
      services: [],
      title: 'Дом',
    }
    const { keys } = stubAPI({
      ...catalogRoutes([...categories, created]),
      'POST /v1/categories': () => json({ category: created }, 201),
    })
    const wrapper = mountLibrary()
    await flushPromises()

    clickByText(wrapper.element as HTMLElement, 'Custom category')
    await flushPromises()
    const panel = dialog()
    const field = panel.querySelector<HTMLInputElement>('#lists-category-title')
    expect(field).not.toBeNull()
    field!.value = 'Дом'
    field!.dispatchEvent(new Event('input'))
    clickByText(panel, 'Create')
    await flushPromises()

    expect(keys()).toEqual([
      'GET /v1/services',
      'GET /v1/targets',
      'POST /v1/categories',
      'GET /v1/services',
      'GET /v1/targets',
    ])
    // A category made to be filled has to be the one on screen.
    expect(wrapper.get('.lists__details-title').text()).toBe('Дом')
    wrapper.unmount()
  })

  // The server works the overlay out itself, so an edit states the whole
  // membership the operator wants rather than the one list that moved.
  it('adds a list to a category from the whole catalog', async () => {
    const widened = categories.map((category) =>
      category.id === 'video'
        ? { ...category, services: ['youtube', 'steam'] }
        : category,
    )
    const { calls } = stubAPI({
      ...catalogRoutes(widened),
      'POST /v1/categories/video/update': () => json({ category: widened[1] }),
    })
    const wrapper = mountLibrary()
    await flushPromises()

    await openCategory(wrapper, 'Video')
    await openMenu(wrapper, 'Actions for category Video')
    menuItem('Add a list')?.click()
    await flushPromises()

    const panel = dialog()
    const field = panel.querySelector<HTMLInputElement>('.rv-combobox__input')
    expect(field).not.toBeNull()
    field!.focus()
    field!.value = 'Steam'
    field!.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    for (const key of ['ArrowDown', 'Enter']) {
      field!.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key }))
      await flushPromises()
    }
    clickByText(dialog(), 'Add')
    await flushPromises()

    expect(calls.at(-3)?.key).toBe('POST /v1/categories/video/update')
    expect(calls.at(-3)?.body).toBe(
      JSON.stringify({ services: ['youtube', 'steam'] }),
    )
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
    clickByText(panel, 'Delete')
    await flushPromises()

    expect(keys().at(-3)).toBe('POST /v1/categories/custom-home/remove')
    expect(calls.at(-3)?.body).toBe(JSON.stringify({ lists: disposition }))
    expect(wrapper.get('.lists__groups').text()).not.toContain('Домашние')
    wrapper.unmount()
  })

  // A category a route still names is kept, and the refusal names the routes
  // standing in the way — beside the act that was refused.
  it('keeps a category a route still holds and names those routes', async () => {
    stubAPI({
      ...catalogRoutes(),
      'POST /v1/categories/custom-home/remove': () =>
        json(
          {
            error: 'category in use',
            lists: [
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
    clickByText(dialog(), 'Delete')
    await flushPromises()

    const panel = dialog()
    expect(panel.textContent).toContain('The category was not deleted')
    expect(panel.textContent).toContain('Дом, Офис')
    expect(wrapper.get('.lists__groups').text()).toContain('Домашние')
    wrapper.unmount()
  })

  // The bin means deletion and nothing else now: leaving a category is a menu
  // item with words on it.
  it('takes a list out of its category from the row menu', async () => {
    const trimmed = categories.map((category) =>
      category.id === 'communication'
        ? { ...category, services: ['telegram'] }
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
    expect(calls.at(-3)?.body).toBe(JSON.stringify({ services: ['telegram'] }))
    wrapper.unmount()
  })

  it('deletes a list, and keeps one a route still holds', async () => {
    const { calls, keys } = stubAPI({
      ...catalogRoutes(),
      'POST /v1/services/discord/remove': () =>
        json(
          {
            error: 'list in use',
            lists: [{ id: 'a'.repeat(32), title: 'Дом' }],
          },
          409,
        ),
      'POST /v1/services/telegram/remove': () =>
        new Response(null, { status: 204 }),
    })
    const wrapper = mountLibrary()
    await flushPromises()

    await openCategory(wrapper, 'Communication')
    await openMenu(wrapper, 'Actions for list Discord')
    menuItem('Delete the list')?.click()
    await flushPromises()
    const panel = dialog()
    expect(panel.textContent).toContain(
      'List “Discord” and its entries are removed.',
    )
    clickByText(panel, 'Delete')
    await flushPromises()

    expect(keys().at(-1)).toBe('POST /v1/services/discord/remove')
    expect(dialog().textContent).toContain('The list was not deleted')
    expect(dialog().textContent).toContain('Дом')

    // The one nothing holds goes, and the catalog is read back after it.
    clickByText(dialog(), 'Cancel')
    await flushPromises()
    await openMenu(wrapper, 'Actions for list Telegram')
    menuItem('Delete the list')?.click()
    await flushPromises()
    clickByText(dialog(), 'Delete')
    await flushPromises()

    expect(calls.at(-3)?.key).toBe('POST /v1/services/telegram/remove')
    expect(keys().slice(-2)).toEqual(['GET /v1/services', 'GET /v1/targets'])
    wrapper.unmount()
  })

  // The address states where the operator is, so the composing card's link
  // lands on the category and the list it named.
  it('opens the category and the list named in the address', async () => {
    stubAPI({
      ...catalogRoutes(),
      'GET /v1/services/youtube/contents': () => contentsResponse('youtube'),
    })
    const wrapper = mountLibrary('#category=video&list=youtube')
    await flushPromises()

    expect(wrapper.get('.lists__details-title').text()).toBe('Video')
    const card = dialog()
    expect(card.textContent).toContain('YouTube')
    expect(card.textContent).toContain('youtube.example')
    // No route is in question here, so the card asks about none.
    expect(card.querySelector('.service-card__membership')).toBeNull()
    wrapper.unmount()
  })
})
