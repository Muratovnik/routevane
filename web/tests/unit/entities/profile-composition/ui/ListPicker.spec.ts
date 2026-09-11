import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render, type RenderResult } from 'vitest-browser-vue'

import { invalidateCatalogCache } from '@/shared/api/catalog'
import type { ListDetail } from '@/shared/api/catalog'
import type { ProfileComposition, TargetForecast } from '@/shared/api/profiles'
import { useLocale } from '@/shared/i18n/useLocale'

import ListPicker from '@/entities/profile-composition/ui/ListPicker.vue'

const BULK = 'Select or clear all visible lists'
const CONTENTS = 'List contents'
const FIND_A_LIST = 'Find a list'
const SEARCH_CONTENTS = 'Search the contents'

// The priority handle of a row names its list whether the list is in the
// profile or not, so the handles are the rows: one per list, in row order.
const HANDLE =
  /^(?:Change priority of list (?<chosen>.+), position \d+|Priority of (?<offered>.+): add it to the profile first)$/

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
// Steam belongs to no category, which is what puts the last row on the screen.
const LISTS = [
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

const EMPTY_COMPOSITION: ProfileComposition = {
  categories: [],
  exclusions: [],
  listDomains: {},
  lists: [],
}

// Every prop any case below sets, so a case that changes one states the change
// rather than introducing a property the first render never declared.
const PICKER_PROPS = {
  categories: CATEGORIES,
  disabled: false,
  forecast: null as TargetForecast | null,
  lists: LISTS as ListDetail[],
  modelValue: EMPTY_COMPOSITION,
  pending: false,
  profileName: '',
}

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const contentsResponse = (observed = true): Response =>
  json({
    list_id: 'discord',
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

const acceptedResponse = (): Response => json({ refresh: {} })

/**
 * The picker's traffic, answered by profile. Anything a test does not name is a
 * fault in that test rather than a silent default, so an unrouted call answers
 * with a refusal it will notice.
 */
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

const renderPicker = (props: Partial<typeof PICKER_PROPS> = {}) =>
  render(ListPicker, { props: { ...PICKER_PROPS, ...props } })

const rowNames = (screen: RenderResult<unknown>): string[] =>
  screen
    .getByRole('button', { name: HANDLE })
    .elements()
    .map((handle) => {
      const groups = HANDLE.exec(
        handle.getAttribute('aria-label') ?? '',
      )?.groups
      return groups?.chosen ?? groups?.offered ?? ''
    })

// One choice per row carries that row's list id; the header's own box is named
// for every visible list rather than for one, so it is not a row.
const rowIDs = (screen: RenderResult<unknown>): string[] =>
  screen
    .getByRole('checkbox')
    .elements()
    .filter((box) => box.getAttribute('aria-label') !== BULK)
    .map((box) => (box as HTMLInputElement).value)

const chosenCount = (screen: RenderResult<unknown>): number =>
  screen
    .getByRole('checkbox')
    .elements()
    .filter(
      (box) =>
        box.getAttribute('aria-label') !== BULK &&
        (box as HTMLInputElement).checked,
    ).length

const isIndeterminate = (box: { element: () => Element }): boolean =>
  (box.element() as HTMLInputElement).indeterminate

// The card is a portalled modal, and its contents are one named list, so both
// are reached through their accessible names rather than through the picker's
// own subtree.
const contents = (screen: RenderResult<unknown>) =>
  screen.getByRole('list', { name: CONTENTS })

const lastModel = (screen: RenderResult<unknown>): ProfileComposition =>
  screen.emitted('update:modelValue')!.at(-1)![0] as ProfileComposition

describe('ListPicker', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
    invalidateCatalogCache()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('bulk selection applies to filtered rows and preserves hidden and category selections', async () => {
    const screen = await renderPicker({
      modelValue: {
        ...EMPTY_COMPOSITION,
        categories: ['communication'],
        exclusions: ['telegram'],
        lists: ['youtube'],
      },
    })
    const search = screen.getByLabelText(FIND_A_LIST)
    await search.fill('communication')

    const bulk = screen.getByRole('checkbox', { name: BULK })
    expect(isIndeterminate(bulk)).toBe(true)

    await bulk.click()
    const selected = lastModel(screen)
    expect(selected).toMatchObject({
      categories: ['communication'],
      exclusions: [],
      lists: ['youtube'],
    })

    await screen.rerender({ modelValue: selected })
    await bulk.click()
    expect(lastModel(screen)).toMatchObject({
      categories: ['communication'],
      exclusions: ['discord', 'telegram'],
      lists: ['youtube'],
    })

    await search.fill('no matching list')
    await expect.element(bulk).toBeDisabled()
  })

  it('bulk selects the union of category filters once and preserves hidden selections', async () => {
    const screen = await renderPicker({
      modelValue: { ...EMPTY_COMPOSITION, lists: ['steam'] },
    })

    for (const chip of [/^Communication/, /^Video/, /^Домашние/])
      await screen.getByRole('button', { name: chip }).click()

    expect(rowIDs(screen)).toEqual(['discord', 'telegram', 'youtube'])

    const bulk = screen.getByRole('checkbox', { name: BULK })
    await bulk.click()
    const selected = lastModel(screen)
    expect([...selected.lists].sort()).toEqual([
      'discord',
      'steam',
      'telegram',
      'youtube',
    ])

    await screen.rerender({ modelValue: selected })
    await bulk.click()
    expect(lastModel(screen)).toMatchObject({ lists: ['steam'] })
  })

  it('keeps row order and search state while selection changes', async () => {
    const screen = await renderPicker({
      modelValue: { ...EMPTY_COMPOSITION, lists: ['discord'] },
    })

    await screen.rerender({
      modelValue: { ...EMPTY_COMPOSITION, lists: ['steam', 'discord'] },
    })
    expect(rowNames(screen)).toEqual([
      'Discord',
      'Telegram',
      'YouTube',
      'Steam',
    ])

    await screen.rerender({
      modelValue: { ...EMPTY_COMPOSITION, lists: ['discord'] },
    })
    const search = screen.getByLabelText(FIND_A_LIST)
    await search.fill('steam')
    await screen
      .getByRole('checkbox', { name: 'Add Steam to the profile' })
      .click()

    await expect.element(search).toHaveValue('steam')
    expect(lastModel(screen)).toMatchObject({ lists: ['discord', 'steam'] })
  })

  it('reorders from a filtered row without dropping hidden selections, and respects disabled state', async () => {
    const screen = await renderPicker({
      modelValue: {
        ...EMPTY_COMPOSITION,
        lists: ['discord', 'telegram', 'youtube'],
        priority: ['discord', 'telegram', 'youtube'],
      },
    })
    await screen.getByLabelText(FIND_A_LIST).fill('telegram')

    const handle = screen.getByRole('button', {
      name: 'Change priority of list Telegram, position 2',
    })
    handle.element().focus()
    await userEvent.keyboard('{ArrowDown}')

    expect(screen.emitted('reorder')).toEqual([
      [['discord', 'youtube', 'telegram']],
    ])
    expect(screen.emitted('update:modelValue')).toBeUndefined()

    // A disabled picker takes no reorder at all: the handle stops being a
    // control rather than staying one that quietly does nothing.
    await screen.rerender({ disabled: true })
    await expect
      .element(
        screen.getByRole('button', {
          name: 'Change priority of list Telegram, position 2',
        }),
      )
      .toBeDisabled()
    expect(screen.emitted('reorder')).toHaveLength(1)
  })

  it('disambiguates repeated list titles in rows and overlap tags', async () => {
    const repeated = [
      { categories: [], id: 'custom-alpha', title: 'Shared' },
      { categories: [], id: 'custom-beta', title: 'Shared' },
    ]
    const screen = await renderPicker({
      forecast: {
        fits: true,
        maximumRules: 10,
        overlaps: {
          items: [],
          summary: [
            { listID: 'custom-alpha', overlaps: ['custom-beta'] },
            { listID: 'custom-beta', overlaps: ['custom-alpha'] },
          ],
          truncated: false,
        },
        perList: [],
        projectedRules: 2,
        targetID: 'target',
      },
      modelValue: {
        ...EMPTY_COMPOSITION,
        priority: ['custom-alpha', 'custom-beta'],
        lists: ['custom-alpha', 'custom-beta'],
      },
      lists: repeated,
    })

    expect(rowNames(screen)).toEqual([
      'Shared · custom-a…',
      'Shared · custom-b…',
    ])
    await expect
      .element(
        screen.getByRole('button', { name: 'Overlap: Shared · custom-a…' }),
      )
      .toBeVisible()
    await expect
      .element(
        screen.getByRole('button', { name: 'Overlap: Shared · custom-b…' }),
      )
      .toBeVisible()
  })

  /**
   * A list in no category is not a second surface. It is the last row of the
   * same column, computed here, and it carries no membership controls because
   * there is no category to leave.
   */
  it('quick category filters narrow choices without changing profile membership', async () => {
    const screen = await renderPicker()

    await screen.getByRole('button', { name: /^Communication/ }).click()
    expect(rowNames(screen)).toEqual(['Discord', 'Telegram'])
    expect(screen.emitted('update:modelValue')).toBeUndefined()

    await screen.getByRole('button', { name: 'All categories' }).click()
    expect(rowIDs(screen)).toHaveLength(4)
  })

  it('filters by category and exposes a separate live category reference', async () => {
    const screen = await renderPicker()

    await screen.getByRole('button', { name: /^Communication/ }).click()
    expect(rowNames(screen)).toEqual(['Discord', 'Telegram'])

    // Following the category is a standing instruction, stated on its own
    // control rather than mixed into the rows.
    const follow = screen.getByRole('button', {
      name: 'Follow “Communication”',
    })
    await expect.element(follow).toMatchTextContent('New lists: manual')

    await follow.click()
    await screen
      .getByRole('menuitem', {
        name: 'Automatically include new lists in this category',
      })
      .click()

    expect(lastModel(screen)).toMatchObject({
      categories: ['communication'],
      lists: [],
    })
  })

  /**
   * Selection is a checkbox and nothing else (ADR 0029). What a category holds,
   * what a list holds and whether either exists are the library's subject, so
   * this surface offers no control that would reach another profile — and, having
   * none, writes nothing at all.
   */
  it('offers no control that reaches beyond this profile', async () => {
    const { keys } = stubAPI({})
    const screen = await renderPicker()

    // Nothing here opens a menu of library actions: with no category filter
    // standing there is no standing instruction to change either.
    expect(
      screen
        .getByRole('button')
        .elements()
        .filter((control) => control.getAttribute('aria-haspopup') === 'menu'),
    ).toEqual([])
    for (const absent of ['Custom category', 'Custom list', 'Add a list'])
      await expect
        .element(screen.getByText(absent, { exact: false }))
        .not.toBeInTheDocument()

    await screen
      .getByRole('checkbox', { name: 'Add Discord to the profile' })
      .click()
    expect(keys()).toEqual([])
  })

  it('shows complete overlap tags and does not read an absent summary as none', async () => {
    const baseForecast = {
      fits: true,
      maximumRules: 100,
      perList: [
        { rules: 3, listID: 'discord' },
        { rules: 2, listID: 'telegram' },
      ],
      projectedRules: 5,
      targetID: 'keenetic',
    }
    const screen = await renderPicker({
      forecast: {
        ...baseForecast,
        overlaps: {
          items: [],
          summary: [
            { overlaps: ['telegram'], listID: 'discord' },
            { overlaps: ['discord'], listID: 'telegram' },
          ],
          truncated: false,
        },
      },
      modelValue: {
        ...EMPTY_COMPOSITION,
        priority: ['discord', 'telegram'],
        lists: ['discord', 'telegram'],
      },
    })

    expect(chosenCount(screen)).toBe(2)
    await expect
      .element(screen.getByRole('cell', { name: '≈ 3 rules' }))
      .toHaveTextContent('3')
    await expect
      .element(screen.getByRole('button', { name: 'Overlap: Telegram' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: 'Overlap: Discord' }))
      .toBeVisible()

    await screen.rerender({
      forecast: { ...baseForecast, overlaps: { items: [], truncated: false } },
    })
    await expect
      .element(screen.getByText('Overlaps unknown').first())
      .toBeVisible()
    await expect
      .element(screen.getByText('None', { exact: false }))
      .not.toBeInTheDocument()
  })

  // The card opens on the contents, and a row is named by its value and the
  // source that offered it — nothing repeats what the value already says, and
  // nothing on the row is a control: what a profile takes from a list is the
  // whole list (ADR 0029).
  it('closes during a read and ignores its late result without starting source work', async () => {
    const held = Promise.withResolvers<Response>()
    const { keys } = stubAPI({
      'GET /v1/lists/discord/contents': () => held.promise,
    })
    const screen = await renderPicker()

    await screen
      .getByRole('button', { name: 'Open the contents of list Discord' })
      .click()
    const close = screen.getByRole('button', { name: 'Close' })
    await expect.element(close).toBeEnabled()

    await close.click()
    held.resolve(contentsResponse(false))

    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    expect(keys()).toEqual(['GET /v1/lists/discord/contents'])
    expect(screen.emitted('update:modelValue')).toBeUndefined()
  })

  it('opens on the contents table with origins and no row controls', async () => {
    const { keys } = stubAPI({
      'GET /v1/lists/discord/contents': () => contentsResponse(),
    })
    const screen = await renderPicker()

    await screen
      .getByRole('button', { name: 'Open the contents of list Discord' })
      .click()

    const card = screen.getByRole('dialog')
    await expect.element(card).toBeVisible()
    expect(keys()[0]).toBe('GET /v1/lists/discord/contents')
    for (const shown of [
      'discord.com',
      'catalog',
      'discord.gg',
      'itdoginfo',
      '198.51.100.7',
      '198.51.100.0/24',
      'iplist',
      '3 entries on',
    ])
      await expect.element(card).toMatchTextContent(shown)
    // The value already says what it is; the caption does not repeat it.
    await expect.element(card).not.toMatchTextContent('IP address')
    await expect.element(contents(screen)).not.toMatchTextContent('from source')
    expect(contents(screen).getByRole('listitem').all()).toHaveLength(5)
    expect(contents(screen).getByRole('checkbox').all()).toEqual([])
    expect(screen.emitted('update:modelValue')).toBeUndefined()
  })

  // The filter narrows what is drawn without touching what is stored, and an
  // empty result says so in one line.
  it('filters the contents by value and by origin', async () => {
    stubAPI({ 'GET /v1/lists/discord/contents': () => contentsResponse() })
    const screen = await renderPicker()

    await screen
      .getByRole('button', { name: 'Open the contents of list Discord' })
      .click()
    const field = screen.getByLabelText(SEARCH_CONTENTS)

    await field.fill('iplist')
    expect(contents(screen).getByRole('listitem').all()).toHaveLength(2)
    await expect.element(screen.getByText('198.51.100.7')).toBeVisible()
    await expect.element(screen.getByText('discord.gg')).not.toBeInTheDocument()

    await field.fill('198.51.100.0/24')
    expect(contents(screen).getByRole('listitem').all()).toHaveLength(1)

    await field.fill('nothing-here')
    expect(contents(screen).getByRole('listitem').all()).toEqual([])
    await expect.element(screen.getByText('Nothing found.')).toBeVisible()
  })

  // Background a reader may want sits behind the informer, and the informer has
  // to work inside a modal: a focus trap that fought it would make the only
  // explanation on this surface unreachable.
  it('opens the informer inside the card', async () => {
    stubAPI({ 'GET /v1/lists/discord/contents': () => contentsResponse() })
    const screen = await renderPicker()

    await screen
      .getByRole('button', { name: 'Open the contents of list Discord' })
      .click()
    await screen.getByRole('button', { name: 'What this table holds' }).click()

    await expect
      .element(
        screen.getByText('Domains, IP addresses and networks.', {
          exact: false,
        }),
      )
      .toBeVisible()
  })

  // A card that shows only the permanent rows tells the operator nothing about
  // what the list currently reaches, so it reads the sources itself — once.
  // Reading a source changes no profile and no list, which is why the composing
  // card still does it.
  it('reads the sources once when the contents arrive unobserved', async () => {
    const reads: string[] = []
    const { keys } = stubAPI({
      'GET /v1/lists/discord/contents': () => {
        reads.push('read')
        return contentsResponse(reads.length > 1)
      },
      'POST /v1/lists/discord/refresh': () => acceptedResponse(),
    })
    const screen = await renderPicker()

    await screen
      .getByRole('button', { name: 'Open the contents of list Discord' })
      .click()

    await vi.waitFor(() => {
      expect(keys()).toEqual([
        'GET /v1/lists/discord/contents',
        'POST /v1/lists/discord/refresh',
        'GET /v1/lists/discord/contents',
      ])
    })
  })

  it('adds an open list to the profile from the card switch', async () => {
    stubAPI({ 'GET /v1/lists/discord/contents': () => contentsResponse() })
    const screen = await renderPicker()

    await screen
      .getByRole('button', { name: 'Open the contents of list Discord' })
      .click()
    await screen.getByRole('switch', { name: 'In this profile' }).click()

    expect(lastModel(screen)).toMatchObject({ lists: ['discord'] })
  })

  /**
   * The composing card edits one profile and says which one — except while that
   * profile is a draft nobody stored. The composer proposes the name from the
   * lists picked below, so naming it on the switch would read as if the card
   * were naming the list it is showing.
   */
  it('states membership without naming an unsaved draft', async () => {
    stubAPI({ 'GET /v1/lists/discord/contents': () => contentsResponse() })
    const screen = await renderPicker({
      profileName: 'Chat and video',
      pending: true,
    })

    await screen
      .getByRole('button', { name: 'Open the contents of list Discord' })
      .click()

    // No caption explains the reach of an edit, because no surface has two.
    await expect
      .element(screen.getByText('Edits here apply to every profile.'))
      .not.toBeInTheDocument()
    // The switch is off and says so by its own position, without a second
    // sentence stating the same thing.
    await expect
      .element(screen.getByRole('switch', { name: 'In this profile' }))
      .toHaveAttribute('aria-checked', 'false')
    await expect
      .element(screen.getByText('Chat and video', { exact: false }))
      .not.toBeInTheDocument()
    // A draft that was never stored says the membership is waiting on a save.
    await expect
      .element(screen.getByText('applies when the profile is saved'))
      .toBeVisible()
  })

  // A stored profile is named, because the name is the operator's own and the
  // card can speak for it.
  it('names the profile once it is stored', async () => {
    stubAPI({ 'GET /v1/lists/discord/contents': () => contentsResponse() })
    const screen = await renderPicker({ profileName: 'Chat and video' })

    await screen
      .getByRole('button', { name: 'Open the contents of list Discord' })
      .click()

    await expect
      .element(
        screen.getByRole('switch', { name: 'In profile “Chat and video”' }),
      )
      .toBeVisible()
  })

  // A draft with no name yet still has a switch to label.
  it('names an unnamed draft as this profile', async () => {
    stubAPI({ 'GET /v1/lists/discord/contents': () => contentsResponse() })
    const screen = await renderPicker({ profileName: '   ' })

    await screen
      .getByRole('button', { name: 'Open the contents of list Discord' })
      .click()

    await expect
      .element(screen.getByRole('switch', { name: 'In this profile' }))
      .toBeVisible()
    await expect
      .element(screen.getByText('applies when the profile is saved'))
      .not.toBeInTheDocument()
  })
})
