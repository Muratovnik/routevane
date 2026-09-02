import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

import ServicePicker from './ServicePicker.vue'

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
// Steam belongs to no category, which is what puts the last row on the screen.
const services = [
  {
    categories: ['communication'],
    domains: [{ includeSubdomains: true, value: 'discord.com' }],
    id: 'discord',
    sources: [
      { id: 'itdoginfo', type: 'http' as const },
      { id: 'v2fly', type: 'http' as const },
    ],
    sourceCount: 5,
    title: 'Discord',
  },
  { categories: ['communication'], id: 'telegram', title: 'Telegram' },
  { categories: ['custom-home', 'video'], id: 'youtube', title: 'YouTube' },
  { categories: [], id: 'steam', title: 'Steam' },
]

const emptyComposition = {
  categories: [],
  exclusions: [],
  serviceDomains: {},
  services: [],
}

function json(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function contentsResponse(observed = true): Response {
  return json({
    service_id: 'discord',
    rows: [
      {
        value: 'discord.com',
        kind: 'domain',
        origin: 'catalog',
        enabled: true,
      },
      {
        value: 'discord.gg',
        kind: 'domain',
        origin: 'itdoginfo',
        enabled: true,
      },
      {
        value: 'old.discord.media',
        kind: 'domain',
        origin: 'v2fly',
        enabled: false,
      },
      { value: '198.51.100.7', kind: 'ip', origin: 'iplist', enabled: true },
      {
        value: '198.51.100.0/24',
        kind: 'prefix',
        origin: 'iplist',
        enabled: false,
      },
    ],
    sources: [
      { id: 'itdoginfo', type: 'http', custom: false, enabled: true },
      { id: 'v2fly', type: 'http', custom: false, enabled: false },
    ],
    observed,
  })
}

function acceptedResponse(): Response {
  return json({ refresh: {} })
}

/**
 * The picker's traffic, answered by route. Anything a test does not name is a
 * fault in that test rather than a silent default, so an unrouted call answers
 * with a refusal it will notice.
 */
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

// The card is a portalled modal, so it is read where it actually lands: on the
// document, not inside the picker's own subtree.
function openDialog(): HTMLElement {
  const dialogs = document.body.querySelectorAll<HTMLElement>('[role="dialog"]')
  const panel = dialogs.item(dialogs.length - 1)
  expect(panel).not.toBeNull()
  return panel
}

function clickByText(scope: HTMLElement, text: string): void {
  const control = [...scope.querySelectorAll('button')].find((button) =>
    button.textContent?.includes(text),
  )
  expect(control, text).toBeDefined()
  control?.click()
}

function mountPicker(props: Record<string, unknown> = {}) {
  return mount(ServicePicker, {
    attachTo: document.body,
    props: { categories, modelValue: emptyComposition, services, ...props },
    global: { stubs: { RvIcon: true } },
  })
}

describe('ServicePicker', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
    invalidateCatalogCache()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('edits one mixed category in a stable detail pane', async () => {
    const wrapper = mountPicker({
      modelValue: { ...emptyComposition, services: ['discord'] },
    })

    const category = wrapper.get<HTMLInputElement>(
      'input[value="communication"]',
    ).element
    expect(category.checked).toBe(false)
    expect(category.indeterminate).toBe(true)

    const configure = wrapper.findAll('.picker__configure')
    expect(configure[0]?.text()).toBe('')
    expect(configure[0]?.attributes('aria-current')).toBe('true')
    // The pane is the category, named by the category and nothing else.
    expect(wrapper.get('.picker__details h3').text()).toBe('Communication')
    expect(wrapper.text()).toContain('Discord')
    expect(wrapper.text()).not.toContain('Selected manually')
    expect(wrapper.findAll('.picker__service small')).toHaveLength(0)

    await configure[1]?.trigger('click')

    expect(wrapper.get('.picker__details h3').text()).toBe('Video')
    expect(wrapper.findAll('.picker__details')).toHaveLength(1)
    expect(wrapper.get('.picker__details').text()).toContain('YouTube')
    expect(wrapper.findAll('.picker__groups .picker__service')).toHaveLength(0)
    wrapper.unmount()
  })

  /**
   * A list in no category is not a second surface. It is the last row of the
   * same column, computed here, and it carries no membership controls because
   * there is no category to leave.
   */
  it('gives the lists no category claims the last row', async () => {
    const wrapper = mountPicker()

    const rows = wrapper.findAll('.picker__group')
    expect(rows).toHaveLength(4)
    expect(rows.at(-1)?.text()).toContain('Uncategorized')
    expect(wrapper.text()).not.toContain('Other services')

    await wrapper.findAll('.picker__configure').at(-1)?.trigger('click')
    const details = wrapper.get('.picker__details')
    expect(details.get('h3').text()).toBe('Uncategorized')
    expect(details.text()).toContain('Steam')
    // No reference to follow, so selecting the row selects its members.
    await wrapper
      .get('input[value="rv:uncategorized"]')
      .setValue(true as unknown as string)
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toMatchObject({
      categories: [],
      services: ['steam'],
    })
    wrapper.unmount()
  })

  /**
   * Selection is a checkbox and nothing else (ADR 0029). What a category holds,
   * what a list holds and whether either exists are the library's subject, so
   * this surface offers no control that would reach another route — and, having
   * none, writes nothing at all.
   */
  it('offers no control that reaches beyond this route', async () => {
    const { keys } = stubAPI({})
    const wrapper = mountPicker()

    for (const gone of [
      '.rv-menu__trigger',
      '.picker__service-remove',
      '.picker__collections-footer',
    ]) {
      expect(wrapper.findAll(gone), gone).toHaveLength(0)
    }
    const words = wrapper.text()
    for (const absent of ['Custom category', 'Custom list', 'Add a list'])
      expect(words, absent).not.toContain(absent)

    // Every control that is left: the search, the checkboxes, and the chevrons.
    await wrapper.get('input[value="communication"]').setValue(true)
    await wrapper.get('input[value="discord"]').setValue(false)
    await wrapper.findAll('.picker__configure')[1]?.trigger('click')
    await flushPromises()
    expect(keys()).toEqual([])
    wrapper.unmount()
  })

  // The card opens on the contents, and a row is named by its value and the
  // source that offered it — nothing repeats what the value already says, and
  // nothing on the row is a control: what a route takes from a list is the
  // whole list (ADR 0029).
  it('opens on the contents table with origins and no row controls', async () => {
    const { keys } = stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(),
    })
    const wrapper = mountPicker()

    await wrapper.findAll('.picker__service-info')[0]?.trigger('click')
    await flushPromises()
    const card = openDialog()

    expect(keys()[0]).toBe('GET /v1/services/discord/contents')
    expect(card.textContent).toContain('discord.com')
    expect(card.textContent).toContain('catalog')
    expect(card.textContent).toContain('discord.gg')
    expect(card.textContent).toContain('itdoginfo')
    expect(card.textContent).toContain('198.51.100.7')
    expect(card.textContent).toContain('198.51.100.0/24')
    expect(card.textContent).toContain('iplist')
    // The value already says what it is; the caption does not repeat it.
    expect(card.textContent).not.toContain('IP address')
    expect(card.textContent).not.toContain('from source')
    expect(card.textContent).toContain('3 entries on')
    expect(card.querySelectorAll('.service-card__rows li')).toHaveLength(5)
    expect(
      card.querySelectorAll('.service-card__rows input[type="checkbox"]'),
    ).toHaveLength(0)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  // The filter narrows what is drawn without touching what is stored, and an
  // empty result says so in one line.
  it('filters the contents by value and by origin', async () => {
    stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(),
    })
    const wrapper = mountPicker()

    await wrapper.findAll('.picker__service-info')[0]?.trigger('click')
    await flushPromises()
    const card = openDialog()
    const field = card.querySelector<HTMLInputElement>(
      '.service-card__filter-input',
    )
    expect(field).not.toBeNull()

    field!.value = 'iplist'
    field!.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(card.querySelectorAll('.service-card__rows li')).toHaveLength(2)
    expect(card.textContent).toContain('198.51.100.7')
    expect(card.textContent).not.toContain('discord.gg')

    field!.value = '198.51.100.0/24'
    field!.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(card.querySelectorAll('.service-card__rows li')).toHaveLength(1)

    field!.value = 'nothing-here'
    field!.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(card.querySelectorAll('.service-card__rows li')).toHaveLength(0)
    expect(card.textContent).toContain('Nothing found.')
    wrapper.unmount()
  })

  // Background a reader may want sits behind the informer, and the informer has
  // to work inside a modal: a focus trap that fought it would make the only
  // explanation on this surface unreachable.
  it('opens the informer inside the card', async () => {
    stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(),
    })
    const wrapper = mountPicker()

    await wrapper.findAll('.picker__service-info')[0]?.trigger('click')
    await flushPromises()
    const card = openDialog()
    const informer = card.querySelector<HTMLButtonElement>(
      '.rv-infotip__trigger',
    )
    expect(informer).not.toBeNull()

    informer?.click()
    await flushPromises()
    const panel = document.body.querySelector('.rv-infotip__panel')
    expect(panel?.textContent).toContain('Domains, IP addresses and networks.')
    wrapper.unmount()
  })

  // A card that shows only the permanent rows tells the operator nothing about
  // what the list currently reaches, so it reads the sources itself — once.
  // Reading a source changes no route and no list, which is why the composing
  // card still does it.
  it('reads the sources once when the contents arrive unobserved', async () => {
    let reads = 0
    const { keys } = stubAPI({
      'GET /v1/services/discord/contents': () => {
        reads += 1
        return contentsResponse(reads > 1)
      },
      'POST /v1/services/discord/refresh': () => acceptedResponse(),
    })
    const wrapper = mountPicker()

    await wrapper.findAll('.picker__service-info')[0]?.trigger('click')
    await flushPromises()
    await flushPromises()

    expect(keys()).toEqual([
      'GET /v1/services/discord/contents',
      'POST /v1/services/discord/refresh',
      'GET /v1/services/discord/contents',
    ])
    wrapper.unmount()
  })

  it('adds an open list to the route from the card footer', async () => {
    stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(),
    })
    const wrapper = mountPicker()

    await wrapper.findAll('.picker__service-info')[0]?.trigger('click')
    await flushPromises()
    clickByText(openDialog(), 'Add to route')

    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toMatchObject({
      services: ['discord'],
    })
    wrapper.unmount()
  })

  /**
   * The composing card edits one route and says which one — except while that
   * route is a draft nobody stored. The composer proposes the name from the
   * lists picked below, so naming it in the footer would read as if the card
   * were naming the list it is showing.
   */
  it('states membership without naming an unsaved draft', async () => {
    stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(),
    })
    const wrapper = mountPicker({ listName: 'Chat and video', pending: true })

    await wrapper.findAll('.picker__service-info')[0]?.trigger('click')
    await flushPromises()
    const card = openDialog()

    // No caption explains the reach of an edit, because no surface has two.
    expect(card.textContent).not.toContain('Edits here apply to every route.')
    const membership = card.querySelector('.service-card__membership')
    expect(membership?.textContent).toContain('Not in this route')
    expect(membership?.textContent).not.toContain('Chat and video')
    // A draft that was never stored says the membership is waiting on a save.
    expect(membership?.textContent).toContain('applies when the route is saved')
    wrapper.unmount()
  })

  // A stored route is named, because the name is the operator's own and the
  // card can speak for it.
  it('names the route once it is stored', async () => {
    stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(),
    })
    const wrapper = mountPicker({ listName: 'Chat and video' })

    await wrapper.findAll('.picker__service-info')[0]?.trigger('click')
    await flushPromises()

    expect(
      openDialog().querySelector('.service-card__membership')?.textContent,
    ).toContain('Not in route “Chat and video”')
    wrapper.unmount()
  })

  // A draft with no name yet still has a footer to label.
  it('names an unnamed draft as this route', async () => {
    stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(),
    })
    const wrapper = mountPicker({ listName: '   ' })

    await wrapper.findAll('.picker__service-info')[0]?.trigger('click')
    await flushPromises()
    const card = openDialog()

    expect(
      card.querySelector('.service-card__membership')?.textContent?.trim(),
    ).toBe('Not in this route')
    wrapper.unmount()
  })
})
