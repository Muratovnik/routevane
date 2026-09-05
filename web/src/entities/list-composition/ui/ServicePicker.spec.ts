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

  it('bulk selection applies to filtered rows and preserves hidden and category selections', async () => {
    const wrapper = mountPicker({
      modelValue: {
        ...emptyComposition,
        categories: ['communication'],
        exclusions: ['telegram'],
        services: ['youtube'],
      },
    })
    const search = wrapper.get('input[type="search"]')
    await search.setValue('communication')
    const bulk = wrapper.get<HTMLInputElement>('thead input[type="checkbox"]')
    expect(bulk.element.indeterminate).toBe(true)
    await bulk.setValue(true)
    const selected = wrapper
      .emitted('update:modelValue')!
      .at(-1)![0] as typeof emptyComposition
    expect(selected).toMatchObject({
      categories: ['communication'],
      exclusions: [],
      services: ['youtube'],
    })
    await wrapper.setProps({ modelValue: selected })
    await bulk.setValue(false)
    expect(wrapper.emitted('update:modelValue')!.at(-1)![0]).toMatchObject({
      categories: ['communication'],
      exclusions: ['discord', 'telegram'],
      services: ['youtube'],
    })
    await search.setValue('no matching service')
    expect(bulk.element.disabled).toBe(true)
    wrapper.unmount()
  })

  it('bulk selects the union of category filters once and preserves hidden selections', async () => {
    const wrapper = mountPicker({
      modelValue: { ...emptyComposition, services: ['steam'] },
    })
    wrapper
      .findComponent({ name: 'CategoryFilters' })
      .vm.$emit('update:modelValue', ['communication', 'video', 'custom-home'])
    await wrapper.vm.$nextTick()
    expect(
      wrapper.findAll('.picker__row').map((row) => row.attributes('data-id')),
    ).toEqual(['discord', 'telegram', 'youtube'])
    const bulk = wrapper.get<HTMLInputElement>('thead input[type="checkbox"]')
    await bulk.setValue(true)
    const selected = wrapper
      .emitted('update:modelValue')!
      .at(-1)![0] as typeof emptyComposition
    expect([...selected.services].sort()).toEqual([
      'discord',
      'steam',
      'telegram',
      'youtube',
    ])
    await wrapper.setProps({ modelValue: selected })
    await bulk.setValue(false)
    expect(wrapper.emitted('update:modelValue')!.at(-1)![0]).toMatchObject({
      services: ['steam'],
    })
    wrapper.unmount()
  })

  it('keeps row order and search state while selection changes', async () => {
    const wrapper = mountPicker({
      modelValue: { ...emptyComposition, services: ['discord'] },
    })

    await wrapper.setProps({
      modelValue: { ...emptyComposition, services: ['steam', 'discord'] },
    })
    expect(
      wrapper.findAll('.picker__row .picker__name').map((name) => name.text()),
    ).toEqual(['Discord', 'Telegram', 'YouTube', 'Steam'])

    await wrapper.setProps({
      modelValue: { ...emptyComposition, services: ['discord'] },
    })
    const search = wrapper.get<HTMLInputElement>('input[type="search"]')
    await search.setValue('steam')
    await wrapper.get('input[type="checkbox"][value="steam"]').setValue(true)
    expect(search.element.value).toBe('steam')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toMatchObject({
      services: ['discord', 'steam'],
    })
    wrapper.unmount()
  })

  it('reorders from a filtered row without dropping hidden selections, and respects disabled state', async () => {
    const wrapper = mountPicker({
      modelValue: {
        ...emptyComposition,
        services: ['discord', 'telegram', 'youtube'],
        priority: ['discord', 'telegram', 'youtube'],
      },
    })
    await wrapper.get('input[type="search"]').setValue('telegram')
    await wrapper
      .get('.picker__handle')
      .trigger('keydown', { key: 'ArrowDown' })
    expect(wrapper.emitted('reorder')).toEqual([
      [['discord', 'youtube', 'telegram']],
    ])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ disabled: true })
    await wrapper.get('.picker__handle').trigger('keydown', { key: 'ArrowUp' })
    expect(wrapper.emitted('reorder')).toHaveLength(1)
    wrapper.unmount()
  })

  it('disambiguates repeated list titles in rows and overlap tags', () => {
    const repeated = [
      { categories: [], id: 'custom-alpha', title: 'Shared' },
      { categories: [], id: 'custom-beta', title: 'Shared' },
    ]
    const wrapper = mountPicker({
      forecast: {
        fits: true,
        maximumRules: 10,
        overlaps: {
          items: [],
          summary: [
            { serviceID: 'custom-alpha', overlaps: ['custom-beta'] },
            { serviceID: 'custom-beta', overlaps: ['custom-alpha'] },
          ],
          truncated: false,
        },
        perService: [],
        projectedRules: 2,
        targetID: 'target',
      },
      modelValue: {
        ...emptyComposition,
        priority: ['custom-alpha', 'custom-beta'],
        services: ['custom-alpha', 'custom-beta'],
      },
      services: repeated,
    })

    expect(wrapper.text()).toContain('Shared · custom-a…')
    expect(wrapper.text()).toContain('Shared · custom-b…')
    expect(wrapper.text()).toContain('Overlap: Shared · custom-a…')
    expect(wrapper.text()).toContain('Overlap: Shared · custom-b…')
    wrapper.unmount()
  })

  /**
   * A list in no category is not a second surface. It is the last row of the
   * same column, computed here, and it carries no membership controls because
   * there is no category to leave.
   */
  it('quick category filters narrow choices without changing route membership', async () => {
    const wrapper = mountPicker()
    wrapper
      .findComponent({ name: 'CategoryFilters' })
      .vm.$emit('update:modelValue', ['communication'])
    await wrapper.vm.$nextTick()
    expect(
      wrapper.findAll('.picker__row .picker__name').map((name) => name.text()),
    ).toEqual(['Discord', 'Telegram'])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'All categories')!
      .trigger('click')
    expect(wrapper.findAll('.picker__row')).toHaveLength(4)
    wrapper.unmount()
  })

  it('filters by category and exposes a separate live category reference', async () => {
    const wrapper = mountPicker()

    wrapper
      .findComponent({ name: 'CategoryFilters' })
      .vm.$emit('update:modelValue', ['communication'])
    await wrapper.vm.$nextTick()

    expect(wrapper.findAll('.picker__row').map((row) => row.text())).toEqual([
      expect.stringContaining('Discord'),
      expect.stringContaining('Telegram'),
    ])
    expect(wrapper.find('.picker__category-reference').exists()).toBe(false)
    const follow = wrapper.findComponent({ name: 'RvMenu' })
    expect(follow.props('triggerText')).toBe('New lists: manual')
    follow.vm.$emit('select', 'auto')
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toMatchObject({
      categories: ['communication'],
      services: [],
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

    await wrapper.get('input[value="discord"]').setValue(true)
    await flushPromises()
    expect(keys()).toEqual([])
    wrapper.unmount()
  })

  it('shows complete overlap tags and does not read an absent summary as none', async () => {
    const baseForecast = {
      fits: true,
      maximumRules: 100,
      perService: [
        { rules: 3, serviceID: 'discord' },
        { rules: 2, serviceID: 'telegram' },
      ],
      projectedRules: 5,
      targetID: 'keenetic',
    }
    const wrapper = mountPicker({
      forecast: {
        ...baseForecast,
        overlaps: {
          items: [],
          summary: [
            { overlaps: ['telegram'], serviceID: 'discord' },
            { overlaps: ['discord'], serviceID: 'telegram' },
          ],
          truncated: false,
        },
      },
      modelValue: {
        ...emptyComposition,
        priority: ['discord', 'telegram'],
        services: ['discord', 'telegram'],
      },
    })

    expect(wrapper.findAll('.picker__row--selected')).toHaveLength(2)
    expect(wrapper.get('td[aria-label="≈ 3 rules"]').text()).toBe('3')
    expect(wrapper.text()).toContain('Overlap: Telegram')
    expect(wrapper.text()).toContain('Overlap: Discord')
    await wrapper.setProps({
      forecast: {
        ...baseForecast,
        overlaps: { items: [], truncated: false },
      },
    })
    expect(wrapper.text()).toContain('Overlaps unknown')
    expect(wrapper.text()).not.toContain('None')
    wrapper.unmount()
  })

  // The card opens on the contents, and a row is named by its value and the
  // source that offered it — nothing repeats what the value already says, and
  // nothing on the row is a control: what a route takes from a list is the
  // whole list (ADR 0029).
  it('closes during a read and ignores its late result without starting source work', async () => {
    let release!: (response: Response) => void
    const { keys } = stubAPI({
      'GET /v1/services/discord/contents': () =>
        new Promise((resolve) => {
          release = resolve
        }),
    })
    const wrapper = mountPicker()
    await wrapper.findAll('.picker__open')[0]!.trigger('click')
    await flushPromises()
    const close =
      openDialog().querySelector<HTMLButtonElement>('.rv-dialog__close')!
    expect(close.disabled).toBe(false)
    close.click()
    await flushPromises()
    release(contentsResponse(false))
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(keys()).toEqual(['GET /v1/services/discord/contents'])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it('opens on the contents table with origins and no row controls', async () => {
    const { keys } = stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(),
    })
    const wrapper = mountPicker()

    await wrapper.findAll('.picker__open')[0]?.trigger('click')
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
    expect(
      card.querySelector('.service-card__rows')?.textContent,
    ).not.toContain('from source')
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

    await wrapper.findAll('.picker__open')[0]?.trigger('click')
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

    await wrapper.findAll('.picker__open')[0]?.trigger('click')
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

    await wrapper.findAll('.picker__open')[0]?.trigger('click')
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

    await wrapper.findAll('.picker__open')[0]?.trigger('click')
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

    await wrapper.findAll('.picker__open')[0]?.trigger('click')
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

    await wrapper.findAll('.picker__open')[0]?.trigger('click')
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

    await wrapper.findAll('.picker__open')[0]?.trigger('click')
    await flushPromises()
    const card = openDialog()

    expect(
      card.querySelector('.service-card__membership')?.textContent?.trim(),
    ).toBe('Not in this route')
    wrapper.unmount()
  })
})
