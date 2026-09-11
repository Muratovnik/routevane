import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, type RenderResult } from 'vitest-browser-vue'
import { nextTick } from 'vue'

import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

import ListDetailDialog from '@/entities/profile-composition/ui/ListDetailDialog.vue'

const CONTENTS = 'List contents'
const SEARCH_CONTENTS = 'Search the contents'
const REFRESH = /^Refresh from sources/
const CONFIGURE = 'Configure sources'
const DELETE = 'Delete the list'
const SAVING = 'Saving change…'
const NOT_APPLIED = 'The change was not applied.'
const SOURCES_READ = 'Sources read'

const DISCORD = {
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

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const contentsResponse = (
  observed = true,
  sources = [
    { id: 'itdoginfo', type: 'http', custom: false, enabled: true },
    { id: 'v2fly', type: 'http', custom: false, enabled: false },
  ],
  enabledOverrides: Record<string, boolean> = {},
  manual = false,
): Response =>
  json({
    list_id: 'discord',
    rows: [
      {
        value: 'discord.com',
        kind: 'domain',
        origin: 'catalog',
        enabled: enabledOverrides['discord.com'] ?? true,
      },
      {
        value: 'discord.gg',
        kind: 'domain',
        origin: 'itdoginfo',
        enabled: enabledOverrides['discord.gg'] ?? true,
      },
      {
        value: 'old.discord.media',
        kind: 'domain',
        origin: 'v2fly',
        enabled: enabledOverrides['old.discord.media'] ?? false,
      },
      {
        value: '198.51.100.7',
        kind: 'ip',
        origin: 'iplist',
        enabled: enabledOverrides['198.51.100.7'] ?? true,
      },
      ...(manual
        ? [
            {
              value: 'manual.discord.test',
              kind: 'domain',
              origin: 'manual',
              enabled: enabledOverrides['manual.discord.test'] ?? true,
            },
          ]
        : []),
    ],
    sources,
    observed,
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

// Which flow opened the card is not optional anywhere, tests included: the two
// modes are the component's contract rather than a default it can fall back to.
const renderCard = (
  props: Record<string, unknown> & { mode: 'compose' | 'library' },
) =>
  render(ListDetailDialog, {
    props: {
      disabled: false,
      list: DISCORD as typeof DISCORD | null,
      ...props,
    },
  })

// The contents are one named section of the card, so its rows are reached
// through that name rather than through the card's markup.
const contents = (screen: RenderResult<unknown>) =>
  screen.getByRole('region', { name: CONTENTS })

const entries = (screen: RenderResult<unknown>) =>
  contents(screen).getByRole('listitem')

const entry = (screen: RenderResult<unknown>, value: string) =>
  entries(screen).filter({ hasText: value })

// A row's switch is labelled by the row itself: the value first, then where the
// value came from, so the value anchors the name.
const named = (value: string): RegExp =>
  new RegExp(`^${value.replaceAll('.', '\\.')}`)

const entrySwitch = (screen: RenderResult<unknown>, value: string) =>
  screen.getByRole('checkbox', { name: named(value) })

// Whether the route being composed carries this list. One switch says it and
// sets it, so a case reads the state from the same control it presses.
const membership = (screen: RenderResult<unknown>) =>
  screen.getByRole('switch', { name: 'In this profile' })

const isChecked = (box: { element: () => Element }): boolean =>
  (box.element() as HTMLInputElement).checked

describe('ListDetailDialog', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
    invalidateCatalogCache()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('keeps the current card and its filter when a catalog reload replaces the same list object', async () => {
    const api = stubAPI({
      'GET /v1/lists/discord/contents': () => contentsResponse(),
    })
    const screen = await renderCard({ mode: 'library' })
    await screen.getByLabelText(SEARCH_CONTENTS).fill('discord.gg')
    await screen.rerender({
      list: { ...DISCORD, categories: [...DISCORD.categories] },
    })
    await expect
      .element(screen.getByLabelText(SEARCH_CONTENTS))
      .toHaveValue('discord.gg')
    await expect.element(entry(screen, 'discord.gg')).toBeVisible()
    expect(api.keys()).toEqual(['GET /v1/lists/discord/contents'])
  })

  it.each([200, 422])(
    'shows committed rows after a partial refresh (%s) without a success mark or an automatic retry loop',
    async (status) => {
      let refreshed = false
      const api = stubAPI({
        'GET /v1/lists/discord/contents': () =>
          refreshed
            ? contentsResponse(false, undefined, {}, true)
            : contentsResponse(false),
        'POST /v1/lists/discord/refresh': () => {
          refreshed = true
          if (status === 200) return json({ refresh: { failed_runs: 1 } })
          return json(
            { error: 'source unavailable', code: 'source_unavailable' },
            422,
          )
        },
      })
      const screen = await renderCard({ mode: 'library' })
      await expect.element(entry(screen, 'manual.discord.test')).toBeVisible()
      await expect
        .element(screen.getByRole('button', { name: 'Retry', exact: true }))
        .toBeVisible()
      await expect
        .element(screen.getByLabelText(SOURCES_READ))
        .not.toBeInTheDocument()
      expect(api.keys()).toEqual([
        'GET /v1/lists/discord/contents',
        'POST /v1/lists/discord/refresh',
        'GET /v1/lists/discord/contents',
      ])
    },
  )

  it('reports skipped source entries in both languages and clears them on recovery', async () => {
    const refresh = { failed: false, skipped: 2 }
    stubAPI({
      'GET /v1/lists/discord/contents': () => contentsResponse(),
      'POST /v1/lists/discord/refresh': () =>
        refresh.failed
          ? json({ error: 'operation failed', code: 'source_unavailable' }, 422)
          : json({ refresh: { skipped_entries: refresh.skipped } }),
    })
    const screen = await renderCard({ mode: 'library' })

    await screen.getByRole('button', { name: REFRESH }).click()
    await expect
      .element(screen.getByText('2 source entries skipped'))
      .toBeVisible()

    useLocale().setLocale('ru')
    await expect
      .element(screen.getByText('Пропущено записей источников: 2'))
      .toBeVisible()
    useLocale().setLocale('en')

    refresh.failed = true
    await screen.getByRole('button', { name: REFRESH }).click()
    await expect
      .element(screen.getByText('Sources unavailable. Entries kept.'))
      .toBeVisible()
    // A refused read keeps the entries it already had.
    expect(entries(screen).all()).toHaveLength(4)
    await expect
      .element(screen.getByText('source entries skipped', { exact: false }))
      .not.toBeInTheDocument()

    refresh.failed = false
    refresh.skipped = 0
    await screen.getByRole('button', { name: REFRESH }).click()
    await expect
      .element(screen.getByText('source entries skipped', { exact: false }))
      .not.toBeInTheDocument()
    await expect
      .element(
        screen.getByText('Refresh failed; entries kept', { exact: false }),
      )
      .not.toBeInTheDocument()
  })

  it('recovers an automatic source read in compose without changing membership', async () => {
    const attempts: string[] = []
    const { keys } = stubAPI({
      'GET /v1/lists/discord/contents': () =>
        contentsResponse(attempts.length > 1),
      'POST /v1/lists/discord/refresh': () => {
        attempts.push('refresh')
        return attempts.length === 1
          ? json({ error: 'source unavailable' }, 503)
          : json({ refresh: { skipped_entries: 0 } })
      },
    })
    const screen = await renderCard({
      included: false,
      mode: 'compose',
      pending: true,
    })

    // The initial contents remain in place when the automatic read fails, and
    // the same card now offers an in-context retry even though compose has no
    // source-editing controls.
    await expect
      .element(screen.getByText('Unknown refresh error. Entries kept.'))
      .toBeVisible()
    expect(entries(screen).all()).toHaveLength(4)
    await expect.element(screen.getByLabelText(SEARCH_CONTENTS)).toBeVisible()
    await expect
      .element(membership(screen))
      .toHaveAttribute('aria-checked', 'false')
    expect(screen.emitted('include')).toBeUndefined()

    await screen.getByRole('button', { name: 'Retry' }).click()

    await expect.element(screen.getByRole('alert')).not.toBeInTheDocument()
    expect(attempts).toHaveLength(2)
    expect(keys()).toEqual([
      'GET /v1/lists/discord/contents',
      'POST /v1/lists/discord/refresh',
      'GET /v1/lists/discord/contents',
      'POST /v1/lists/discord/refresh',
      'GET /v1/lists/discord/contents',
    ])
    expect(entries(screen).all()).toHaveLength(4)
    expect(screen.emitted('include')).toBeUndefined()
  })

  it('keeps manual refresh pending and does not claim a read for source-less lists', async () => {
    const held = Promise.withResolvers<Response>()
    const { keys } = stubAPI({
      'GET /v1/lists/discord/contents': () => contentsResponse(true),
      'POST /v1/lists/discord/refresh': () => held.promise,
    })
    const screen = await renderCard({ mode: 'library' })

    await screen.getByRole('button', { name: REFRESH }).click()
    await expect.element(screen.getByText('Refreshing…')).toBeVisible()
    // Nothing claims the sources were read while the read is still in flight.
    await expect
      .element(screen.getByRole('img', { name: SOURCES_READ }))
      .not.toBeInTheDocument()

    // Deleting waits for the work in flight rather than racing it.
    const remove = screen.getByRole('button', { name: DELETE })
    await expect.element(remove).toBeDisabled()
    expect(screen.emitted('remove')).toBeUndefined()

    held.resolve(json({ refresh: { skipped_entries: 0 } }))
    await expect
      .element(screen.getByRole('img', { name: SOURCES_READ }))
      .toBeVisible()
    expect(keys()).toEqual([
      'GET /v1/lists/discord/contents',
      'POST /v1/lists/discord/refresh',
      'GET /v1/lists/discord/contents',
    ])
  })

  it('claims no source read for a list that has no sources', async () => {
    stubAPI({
      'GET /v1/lists/discord/contents': () => contentsResponse(true, []),
    })
    const screen = await renderCard({
      list: { ...DISCORD, sourceCount: 0, sources: [] },
      mode: 'compose',
    })

    await expect
      .element(screen.getByText('No automatic sources', { exact: false }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('img', { name: SOURCES_READ }))
      .not.toBeInTheDocument()
  })

  it('ignores a late contents response after the card closes', async () => {
    const held = Promise.withResolvers<Response>()
    const fetchMock = vi.fn((input: unknown, init?: RequestInit) => {
      const key = `${init?.method ?? 'GET'} ${String(input)}`
      if (key === 'GET /v1/lists/discord/contents') return held.promise
      return Promise.resolve(json({ error: 'unrouted' }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)

    const screen = await renderCard({ included: false, mode: 'compose' })
    await screen.rerender({ list: null })
    held.resolve(contentsResponse())

    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  /**
   * Composing reads the list (ADR 0029) and writes nothing at all. A row is
   * the value and where it came from: the card has no way to take one entry
   * out of one profile, so it offers nothing that would claim it can, and the
   * single act it carries is the footer's.
   */
  it('reads the list and changes nothing but membership while composing', async () => {
    const { keys } = stubAPI({
      'GET /v1/lists/discord/contents': () => contentsResponse(),
    })
    const screen = await renderCard({ included: false, mode: 'compose' })

    // Every entry the list holds is drawn, whatever offered it and whether the
    // library still stands behind it.
    await expect.element(entries(screen).first()).toBeVisible()
    expect(entries(screen).all()).toHaveLength(4)
    await expect
      .element(entry(screen, '198.51.100.7'))
      .toMatchTextContent('iplist')
    const disabled = entry(screen, 'old.discord.media')
    await expect.element(disabled).toHaveClass('list-card__row--disabled')
    await expect.element(disabled).toMatchTextContent('Disabled in library')
    // Not one of them carries a control.
    expect(contents(screen).getByRole('checkbox').all()).toEqual([])
    // The count states what the list offers, never a selection this card made.
    await expect
      .element(screen.getByText('3 entries on', { exact: false }))
      .toBeVisible()

    await membership(screen).click()

    expect(screen.emitted('include')?.at(-1)).toEqual([true])
    // Reading the contents stays the only request a composing card makes.
    expect(keys()).toEqual(['GET /v1/lists/discord/contents'])
  })

  /**
   * The composing card reads the list and edits the profile. Every control that
   * would change the list itself belongs to the other flow, and the one fact
   * about the sources stays as a fact rather than becoming a control.
   */
  it('offers no way to edit the list while composing', async () => {
    stubAPI({ 'GET /v1/lists/discord/contents': () => contentsResponse() })
    const screen = await renderCard({ included: false, mode: 'compose' })

    await expect.element(screen.getByRole('dialog')).toBeVisible()
    for (const absent of ['Add entries', 'Rename', DELETE])
      await expect
        .element(screen.getByText(absent, { exact: false }))
        .not.toBeInTheDocument()
    // The fact is stated; nothing in the heading offers to change it.
    await expect
      .element(screen.getByText('Sources · 2', { exact: false }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: CONFIGURE }))
      .not.toBeInTheDocument()

    // The way to the other flow is a link, in a tab of its own, so the unsaved
    // draft behind this card survives it.
    const link = screen.getByRole('link', { name: 'Open in the library' })
    await expect
      .element(link)
      .toHaveAttribute('href', '/lists#category=communication&list=discord')
    await expect.element(link).toHaveAttribute('rel', 'noopener')
    await expect.element(link).toHaveAttribute('target', '_blank')
    // Membership is one switch that states where the list stands by its own
    // position, so nothing beside it repeats that in words.
    await expect
      .element(membership(screen))
      .toHaveAttribute('aria-checked', 'false')
  })

  /**
   * Curating the library is the other reach: the same switch records the
   * standing verdict every profile reads, and no footer asks about a profile,
   * because no profile is in question.
   */
  it('records a standing verdict in the library and states no membership', async () => {
    const { calls, keys } = stubAPI({
      'GET /v1/lists/discord/contents': () => contentsResponse(),
      'POST /v1/lists/discord/domains': () => contentsResponse(),
    })
    const screen = await renderCard({ mode: 'library' })

    await expect.element(entries(screen).first()).toBeVisible()
    await expect
      .element(screen.getByText('this profile', { exact: false }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByText('Add to profile'))
      .not.toBeInTheDocument()
    // Reading the sources is the frequent act, so it sits on the card itself.
    await expect
      .element(screen.getByRole('button', { name: REFRESH }))
      .toBeVisible()
    await expect.element(screen.getByText('Add entries')).toBeVisible()

    await entrySwitch(screen, 'discord.gg').click()

    await vi.waitFor(() => {
      expect(keys()).toEqual([
        'GET /v1/lists/discord/contents',
        'POST /v1/lists/discord/domains',
      ])
    })
    expect(calls[1]?.body).toBe(
      JSON.stringify({ values: ['discord.gg'], verdict: 'exclude' }),
    )
  })

  it('keeps entry switches optimistic and independent while writes settle', async () => {
    const held = Promise.withResolvers<Response>()
    const api = stubAPI({
      'GET /v1/lists/discord/contents': () => contentsResponse(),
      'POST /v1/lists/discord/domains': () => {
        const body = JSON.parse(api.calls.at(-1)?.body ?? '{}') as {
          values: string[]
        }
        return body.values[0] === 'discord.gg'
          ? held.promise
          : Promise.resolve(
              contentsResponse(true, undefined, {
                [body.values[0] ?? '']: false,
              }),
            )
      },
    })

    const screen = await renderCard({ mode: 'library' })
    await expect.element(entries(screen).first()).toBeVisible()

    await entrySwitch(screen, 'discord.gg').click()
    await entrySwitch(screen, 'discord.com').click()
    await nextTick()
    await expect.element(screen.getByText(SAVING).first()).toBeVisible()

    await vi.waitFor(() => {
      expect(api.keys()).toEqual([
        'GET /v1/lists/discord/contents',
        'POST /v1/lists/discord/domains',
        'POST /v1/lists/discord/domains',
      ])
    })
    expect(isChecked(entrySwitch(screen, 'discord.gg'))).toBe(false)
    expect(isChecked(entrySwitch(screen, 'discord.com'))).toBe(false)

    // The second row's write can complete without waiting for the first one.
    await expect.element(entrySwitch(screen, 'discord.com')).toBeEnabled()
    // Work that reaches the whole list waits for the rows to settle.
    await expect
      .element(screen.getByRole('button', { name: REFRESH }))
      .toBeDisabled()
    await expect
      .element(screen.getByRole('button', { name: DELETE }))
      .toBeDisabled()

    held.resolve(contentsResponse(true, undefined, { 'discord.gg': false }))
  })

  it('settles an authoritative entry response without reverting another queued intent', async () => {
    const manual = Promise.withResolvers<Response>()
    const api = stubAPI({
      'GET /v1/lists/discord/contents': () =>
        contentsResponse(true, undefined, {}, true),
      'POST /v1/lists/discord/domains': () => {
        const body = JSON.parse(api.calls.at(-1)?.body ?? '{}') as {
          values?: string[]
        }
        const value = body.values?.[0]
        return value === 'manual.discord.test'
          ? manual.promise
          : Promise.resolve(
              contentsResponse(true, undefined, { [value ?? '']: false }),
            )
      },
    })

    const screen = await renderCard({ mode: 'library' })
    await expect.element(entries(screen).first()).toBeVisible()

    await entrySwitch(screen, 'manual.discord.test').click()
    await vi.waitFor(() => {
      expect(api.keys()).toEqual([
        'GET /v1/lists/discord/contents',
        'POST /v1/lists/discord/domains',
      ])
    })

    // A newer intent for a different row is queued while the manual-row write
    // is in flight. Its optimistic value must survive the older response.
    await entrySwitch(screen, 'discord.com').click()
    expect(isChecked(entrySwitch(screen, 'discord.com'))).toBe(false)

    manual.resolve(contentsResponse(true, undefined, { 'discord.com': true }))

    // The authoritative answer drops the manual row and leaves the queued
    // intent for the other row exactly as the operator left it.
    await expect
      .element(entry(screen, 'manual.discord.test'))
      .not.toBeInTheDocument()
    expect(isChecked(entrySwitch(screen, 'discord.com'))).toBe(false)
    await expect.element(screen.getByText(SAVING).first()).toBeVisible()

    await vi.waitFor(() => {
      expect(api.keys()).toEqual([
        'GET /v1/lists/discord/contents',
        'POST /v1/lists/discord/domains',
        'POST /v1/lists/discord/domains',
      ])
    })
    expect(isChecked(entrySwitch(screen, 'discord.com'))).toBe(false)
  })

  it('rolls back one failed entry and exposes a row-level retry', async () => {
    const attempts: string[] = []
    const { keys } = stubAPI({
      'GET /v1/lists/discord/contents': () => contentsResponse(),
      'POST /v1/lists/discord/domains': () => {
        attempts.push('write')
        return attempts.length === 1
          ? Promise.resolve(json({ error: 'offline' }, 503))
          : Promise.resolve(
              contentsResponse(true, undefined, { 'discord.gg': false }),
            )
      },
    })
    const screen = await renderCard({ mode: 'library' })
    await expect.element(entries(screen).first()).toBeVisible()

    await entrySwitch(screen, 'discord.gg').click()

    const failed = entry(screen, 'discord.gg')
    await expect.element(failed).toMatchTextContent(NOT_APPLIED)
    expect(isChecked(entrySwitch(screen, 'discord.gg'))).toBe(true)

    await screen
      .getByRole('button', { name: 'Retry the change for discord.gg' })
      .click()

    await vi.waitFor(() => {
      expect(keys()).toEqual([
        'GET /v1/lists/discord/contents',
        'POST /v1/lists/discord/domains',
        'POST /v1/lists/discord/domains',
      ])
    })
    await expect
      .element(entry(screen, 'discord.gg'))
      .not.toMatchTextContent(NOT_APPLIED)
    expect(isChecked(entrySwitch(screen, 'discord.gg'))).toBe(false)
  })

  it('uses the same independent optimistic state for source switches', async () => {
    const held = Promise.withResolvers<Response>()
    stubAPI({
      'GET /v1/lists/discord/contents': () => contentsResponse(),
      'POST /v1/lists/discord/sources/itdoginfo/update': () => held.promise,
      'POST /v1/lists/discord/sources/v2fly/update': () =>
        Promise.resolve(contentsResponse()),
    })
    const screen = await renderCard({ mode: 'library' })

    await screen.getByRole('button', { name: CONFIGURE }).click()
    await expect
      .element(screen.getByRole('dialog', { name: 'Automatic sources' }))
      .toBeVisible()

    // The panel is modal, so the only switches a reader can reach are its own.
    const switches = screen.getByRole('checkbox')
    expect(switches.all()).toHaveLength(2)

    await switches.first().click()
    expect(isChecked(switches.first())).toBe(false)
    await expect.element(screen.getByText(SAVING).first()).toBeVisible()
    await expect.element(switches.first()).toBeEnabled()
    await expect.element(switches.last()).toBeEnabled()

    held.resolve(contentsResponse())
    await expect.element(screen.getByText(SAVING)).not.toBeInTheDocument()
  })

  // Deleting is confirmed by the flow that owns the library, so the card states
  // the request and lets one confirmation and one refusal serve both ways in.
  it('asks the library to delete the list rather than deleting it itself', async () => {
    const { keys } = stubAPI({
      'GET /v1/lists/discord/contents': () => contentsResponse(),
    })
    const screen = await renderCard({ mode: 'library' })
    await expect.element(entries(screen).first()).toBeVisible()

    await screen.rerender({ disabled: true })
    await expect
      .element(screen.getByRole('button', { name: DELETE }))
      .toBeDisabled()
    expect(screen.emitted('remove')).toBeUndefined()

    await screen.rerender({ disabled: false })
    await screen.getByRole('button', { name: DELETE }).click()

    expect(keys()).toEqual(['GET /v1/lists/discord/contents'])
    expect(screen.emitted('remove')?.at(-1)?.[0]).toMatchObject({
      id: 'discord',
    })
  })

  // The feeds themselves are still a rare edit behind their own panel.
  it('keeps the feeds behind their own dialog in the library', async () => {
    stubAPI({ 'GET /v1/lists/discord/contents': () => contentsResponse() })
    const screen = await renderCard({ mode: 'library' })

    await expect
      .element(screen.getByRole('dialog', { name: 'Discord' }))
      .toBeVisible()
    await screen.getByRole('button', { name: CONFIGURE }).click()

    // Its own dialog, named for its own subject rather than for the list.
    const panel = screen.getByRole('dialog', { name: 'Automatic sources' })
    await expect.element(panel).toBeVisible()
    await expect.element(panel).toMatchTextContent('itdoginfo')
    expect(screen.getByRole('checkbox').all()).toHaveLength(2)
  })
})
