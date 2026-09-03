import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

import ServiceDetailDialog from './ServiceDetailDialog.vue'

const discord = {
  categories: ['communication'],
  domains: [{ includeSubdomains: true, value: 'discord.com' }],
  id: 'discord',
  sources: [
    { id: 'itdoginfo', type: 'http' as const },
    { id: 'v2fly', type: 'http' as const },
  ],
  sourceCount: 2,
  title: 'Discord',
}

function json(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function contentsResponse(
  observed = true,
  sources = [
    { id: 'itdoginfo', type: 'http', custom: false, enabled: true },
    { id: 'v2fly', type: 'http', custom: false, enabled: false },
  ],
): Response {
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
    ],
    sources,
    observed,
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

// The card is portalled, so it is read on the document rather than in the
// wrapper's own subtree.
function card(): HTMLElement {
  const dialogs = document.body.querySelectorAll<HTMLElement>('[role="dialog"]')
  const panel = dialogs.item(dialogs.length - 1)
  expect(panel).not.toBeNull()
  return panel
}

function row(scope: HTMLElement, value: string): HTMLElement {
  const found = [
    ...scope.querySelectorAll<HTMLElement>('.service-card__rows li'),
  ].find(
    (item) => item.querySelector('.service-card__value')?.textContent === value,
  )
  expect(found, value).toBeDefined()
  return found as HTMLElement
}

function switchOf(scope: HTMLElement, value: string): HTMLInputElement {
  const input = row(scope, value).querySelector<HTMLInputElement>(
    'input[type="checkbox"]',
  )
  expect(input).not.toBeNull()
  return input as HTMLInputElement
}

function clickByText(scope: HTMLElement, text: string): void {
  const control = [...scope.querySelectorAll('button')].find((button) =>
    button.textContent?.includes(text),
  )
  expect(control, text).toBeDefined()
  control?.click()
}

// Which flow opened the card is not optional anywhere, tests included: the two
// modes are the component's contract rather than a default it can fall back to.
function mountCard(
  props: Record<string, unknown> & { mode: 'compose' | 'library' },
) {
  return mount(ServiceDetailDialog, {
    attachTo: document.body,
    props: { service: discord, ...props },
    global: { stubs: { RvIcon: true } },
  })
}

describe('ServiceDetailDialog', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
    invalidateCatalogCache()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('reports skipped source entries in both languages and clears them on recovery', async () => {
    let skipped = 2
    let failed = false
    stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(),
      'POST /v1/services/discord/refresh': () =>
        failed
          ? json({ error: 'unavailable' }, 422)
          : json({ refresh: { skipped_entries: skipped } }),
    })
    const wrapper = mountCard({ mode: 'library' })
    await flushPromises()
    clickByText(card(), 'Refresh from sources')
    await flushPromises()
    expect(card().textContent).toContain('2 source entries skipped')
    useLocale().setLocale('ru')
    await flushPromises()
    expect(card().textContent).toContain('Пропущено записей источников: 2')
    useLocale().setLocale('en')
    await flushPromises()
    failed = true
    clickByText(card(), 'Refresh from sources')
    await flushPromises()
    expect(card().textContent).toContain('Refresh failed; entries kept')
    expect(card().querySelectorAll('.service-card__rows li')).toHaveLength(4)
    expect(card().textContent).not.toContain('source entries skipped')
    failed = false
    skipped = 0
    clickByText(card(), 'Refresh from sources')
    await flushPromises()
    expect(card().textContent).not.toContain('source entries skipped')
    expect(card().textContent).not.toContain('Refresh failed; entries kept')
    wrapper.unmount()
  })

  it('recovers an automatic source read in compose without changing membership', async () => {
    let refreshAttempts = 0
    const { keys } = stubAPI({
      'GET /v1/services/discord/contents': () =>
        contentsResponse(refreshAttempts > 1),
      'POST /v1/services/discord/refresh': () => {
        refreshAttempts += 1
        return refreshAttempts === 1
          ? json({ error: 'source unavailable' }, 503)
          : json({ refresh: { skipped_entries: 0 } })
      },
    })
    const wrapper = mountCard({
      included: false,
      mode: 'compose',
      pending: true,
    })
    await flushPromises()

    const panel = card()
    // The initial contents remain in place when the automatic read fails, and
    // the same card now offers an in-context retry even though compose has no
    // source-editing controls.
    expect(panel.querySelectorAll('.service-card__rows li')).toHaveLength(4)
    expect(panel.querySelector('input[type="search"]')).not.toBeNull()
    expect(panel.querySelector('.service-card__membership')).not.toBeNull()
    expect(panel.textContent).toContain('Refresh failed; entries kept')
    expect(
      [...panel.querySelectorAll('button')].find((button) =>
        button.textContent?.includes('Retry'),
      ),
    ).not.toBeUndefined()
    expect(wrapper.emitted('include')).toBeUndefined()

    clickByText(panel, 'Retry')
    await flushPromises()

    expect(refreshAttempts).toBe(2)
    expect(keys()).toEqual([
      'GET /v1/services/discord/contents',
      'POST /v1/services/discord/refresh',
      'POST /v1/services/discord/refresh',
      'GET /v1/services/discord/contents',
    ])
    expect(panel.querySelector('[role="alert"]')).toBeNull()
    expect(panel.querySelectorAll('.service-card__rows li')).toHaveLength(4)
    expect(wrapper.emitted('include')).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps manual refresh pending and does not claim a read for source-less lists', async () => {
    let releaseRefresh!: () => void
    const pendingRefresh = new Promise<Response>((resolve) => {
      releaseRefresh = () => resolve(json({ refresh: { skipped_entries: 0 } }))
    })
    const { keys } = stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(true),
      'POST /v1/services/discord/refresh': () => pendingRefresh,
    })
    const wrapper = mountCard({ mode: 'library' })
    await flushPromises()
    const panel = card()
    clickByText(panel, 'Refresh from sources')
    await flushPromises()
    expect(panel.textContent).toContain('Refreshing…')
    expect(panel.textContent).not.toContain('Sources read')
    releaseRefresh()
    await flushPromises()
    expect(panel.textContent).toContain('Sources read')
    expect(keys()).toEqual([
      'GET /v1/services/discord/contents',
      'POST /v1/services/discord/refresh',
      'GET /v1/services/discord/contents',
    ])
    wrapper.unmount()

    const noSources = {
      ...discord,
      sourceCount: 0,
      sources: [],
    }
    stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(true, []),
    })
    const noSourceWrapper = mountCard({ mode: 'compose', service: noSources })
    await flushPromises()
    expect(card().textContent).toContain('No automatic sources')
    expect(card().textContent).not.toContain('Sources read')
    noSourceWrapper.unmount()
  })

  it('ignores a late contents response after the card closes', async () => {
    let resolveContents!: (response: Response) => void
    const pendingContents = new Promise<Response>((resolve) => {
      resolveContents = resolve
    })
    const fetchMock = vi.fn((input: unknown, init?: RequestInit) => {
      const key = `${init?.method ?? 'GET'} ${String(input)}`
      if (key === 'GET /v1/services/discord/contents') return pendingContents
      return Promise.resolve(json({ error: 'unrouted' }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountCard({ included: false, mode: 'compose' })
    await flushPromises()
    await wrapper.setProps({ service: null })
    await flushPromises()
    resolveContents(contentsResponse())
    await flushPromises()

    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  /**
   * Composing reads the list (ADR 0029) and writes nothing at all. A row is
   * the value and where it came from: the card has no way to take one entry
   * out of one route, so it offers nothing that would claim it can, and the
   * single act it carries is the footer's.
   */
  it('reads the list and changes nothing but membership while composing', async () => {
    const { keys } = stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(),
    })
    const wrapper = mountCard({ included: false, mode: 'compose' })
    await flushPromises()

    const panel = card()
    // Every entry the list holds is drawn, whatever offered it and whether the
    // library still stands behind it.
    expect(panel.querySelectorAll('.service-card__rows li')).toHaveLength(4)
    expect(row(panel, '198.51.100.7').textContent).toContain('iplist')
    // Not one of them carries a control.
    expect(
      panel.querySelectorAll('.service-card__rows input[type="checkbox"]'),
    ).toHaveLength(0)
    // The count states what the list offers, never a selection this card made.
    expect(panel.textContent).toContain('3 entries on')

    clickByText(panel, 'Add to route')
    await flushPromises()

    expect(wrapper.emitted('include')?.at(-1)).toEqual([true])
    // Reading the contents stays the only request a composing card makes.
    expect(keys()).toEqual(['GET /v1/services/discord/contents'])
    wrapper.unmount()
  })

  /**
   * The composing card reads the list and edits the route. Every control that
   * would change the list itself belongs to the other flow, and the one fact
   * about the sources stays as a fact rather than becoming a control.
   */
  it('offers no way to edit the list while composing', async () => {
    stubAPI({ 'GET /v1/services/discord/contents': () => contentsResponse() })
    const wrapper = mountCard({ included: false, mode: 'compose' })
    await flushPromises()

    const panel = card()
    for (const absent of [
      'Add entries',
      'Refresh from sources',
      'Rename',
      'Delete the list',
    ]) {
      expect(panel.textContent, absent).not.toContain(absent)
    }
    // The fact is stated; nothing in the heading offers to change it.
    expect(panel.textContent).toContain('Sources · 2')
    expect(
      [...panel.querySelectorAll('button')].some((button) =>
        button.textContent?.includes('Sources · 2'),
      ),
    ).toBe(false)

    // The way to the other flow is a link, in a tab of its own, so the unsaved
    // draft behind this card survives it.
    const link = panel.querySelector<HTMLAnchorElement>('a[target="_blank"]')
    expect(link?.getAttribute('href')).toBe(
      '/library#category=communication&list=discord',
    )
    expect(link?.getAttribute('rel')).toBe('noopener')
    expect(panel.querySelector('.service-card__membership')).not.toBeNull()
    wrapper.unmount()
  })

  /**
   * Curating the library is the other reach: the same switch records the
   * standing verdict every route reads, and no footer asks about a route,
   * because no route is in question.
   */
  it('records a standing verdict in the library and states no membership', async () => {
    const { calls, keys } = stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(),
      'POST /v1/services/discord/domains': () => contentsResponse(),
    })
    const wrapper = mountCard({ mode: 'library' })
    await flushPromises()

    const panel = card()
    expect(panel.querySelector('.service-card__membership')).toBeNull()
    expect(panel.textContent).not.toContain('Add to route')
    // Reading the sources is the frequent act, so it sits on the card itself.
    expect(panel.textContent).toContain('Refresh from sources')
    expect(panel.textContent).toContain('Add entries')

    switchOf(panel, 'discord.gg').click()
    await flushPromises()
    expect(keys()).toEqual([
      'GET /v1/services/discord/contents',
      'POST /v1/services/discord/domains',
    ])
    expect(calls[1]?.body).toBe(
      JSON.stringify({ values: ['discord.gg'], verdict: 'exclude' }),
    )
    wrapper.unmount()
  })

  // Deleting is confirmed by the flow that owns the library, so the card states
  // the request and lets one confirmation and one refusal serve both ways in.
  it('asks the library to delete the list rather than deleting it itself', async () => {
    const { keys } = stubAPI({
      'GET /v1/services/discord/contents': () => contentsResponse(),
    })
    const wrapper = mountCard({ mode: 'library' })
    await flushPromises()

    clickByText(card(), 'Delete the list')
    await flushPromises()

    expect(keys()).toEqual(['GET /v1/services/discord/contents'])
    expect(wrapper.emitted('remove')?.at(-1)?.[0]).toMatchObject({
      id: 'discord',
    })
    wrapper.unmount()
  })

  // The feeds themselves are still a rare edit behind their own panel.
  it('keeps the feeds behind their own dialog in the library', async () => {
    stubAPI({ 'GET /v1/services/discord/contents': () => contentsResponse() })
    const wrapper = mountCard({ mode: 'library' })
    await flushPromises()

    const opened = card()
    clickByText(opened, 'Sources · 2')
    await flushPromises()
    const panel = card()

    expect(panel).not.toBe(opened)
    expect(panel.textContent).toContain('Automatic sources')
    expect(panel.textContent).toContain('itdoginfo')
    expect(panel.querySelectorAll('.service-card__switch input')).toHaveLength(
      2,
    )
    wrapper.unmount()
  })
})
