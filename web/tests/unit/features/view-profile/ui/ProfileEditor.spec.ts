import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render, type RenderResult } from 'vitest-browser-vue'

import { forgetForecastObservations } from '@/entities/profile-composition/model/forecast'
import { useLocale } from '@/shared/i18n/useLocale'

import ProfileEditor from '@/features/view-profile/ui/ProfileEditor.vue'

const SAVE = 'Save and rebuild'
const CANCEL = 'Cancel'
const OVERFLOW = 'Limited fixture: ≈ 6 of 1 — will not fit'

// A row inside the composition has a priority handle that states its list and
// its position; a row outside it has one that says the list must be added
// first. The handles are therefore the composition, read by stated position
// because the rows are drawn in the order the operator arranged them.
const COMPOSED =
  /^Change priority of list (?<list>.+), position (?<position>\d+)$/

const CATEGORIES = [
  {
    custom: false,
    id: 'communication',
    lists: ['discord', 'telegram'],
    title: 'Общение',
  },
]

const LISTS = [
  { categories: ['communication'], id: 'discord', title: 'Discord' },
  { categories: ['communication'], id: 'telegram', title: 'Telegram' },
  { categories: [], id: 'youtube', title: 'YouTube' },
]

const OUTPUTS = [
  { id: 'output-1', targetID: 'keenetic', title: 'Keenetic' },
  { id: 'output-2', targetID: 'limited-fixture', title: 'Limited fixture' },
]

const FORECAST_PAYLOAD = {
  targets: [
    {
      target_id: 'keenetic',
      maximum_rules: 1024,
      projected_rules: 6,
      fits: true,
      per_list: [
        { list_id: 'discord', rules: 3 },
        { list_id: 'telegram', rules: 2 },
        { list_id: 'youtube', rules: 1 },
      ],
    },
    {
      target_id: 'limited-fixture',
      maximum_rules: 1,
      projected_rules: 6,
      fits: false,
      per_list: [],
    },
  ],
}

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const stubPreview = (preview?: () => Promise<Response>) => {
  const fetchMock = vi.fn((input: unknown) => {
    if (String(input).endsWith('/refresh'))
      return Promise.resolve(json({ refresh: {} }))
    return (preview ?? (() => Promise.resolve(json(FORECAST_PAYLOAD))))()
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const renderEditor = (
  overrides: Partial<{
    outputs: typeof OUTPUTS
    busy: boolean
    exclusions: string[]
    priority: string[]
    listDomains: Record<string, string[]>
  }> = {},
) =>
  render(ProfileEditor, {
    props: {
      busy: false,
      categories: CATEGORIES,
      exclusions: [],
      name: 'Chat and video',
      outputs: OUTPUTS,
      selected: ['youtube'],
      selectedCategories: ['communication'],
      listDomains: {},
      lists: LISTS,
      ...overrides,
    },
  })

const composition = (screen: RenderResult<unknown>): string[] =>
  screen
    .getByRole('button', { name: COMPOSED })
    .elements()
    .map(
      (handle) =>
        COMPOSED.exec(handle.getAttribute('aria-label') ?? '')?.groups ?? {},
    )
    .map((groups) => ({
      list: groups.list ?? '',
      position: Number(groups.position ?? 0),
    }))
    .sort((first, second) => first.position - second.position)
    .map((row) => row.list)

const yieldToBrowser = (): Promise<void> =>
  new Promise((resolve) => {
    const channel = new MessageChannel()
    channel.port1.addEventListener('message', () => resolve(), { once: true })
    channel.port1.start()
    channel.port2.postMessage(null)
  })

const settle = async (): Promise<void> => {
  await yieldToBrowser()
  await vi.advanceTimersByTimeAsync(0)
  await yieldToBrowser()
  await vi.advanceTimersByTimeAsync(0)
}

describe('ProfileEditor', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    forgetForecastObservations()
    useLocale().setLocale('en')
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('keeps the full catalog and ordered composition visible together', async () => {
    stubPreview()
    const screen = await renderEditor()
    await settle()

    expect(composition(screen)).toEqual(['Discord', 'Telegram', 'YouTube'])
    // Catalog and composition are one table rather than two panes.
    expect(screen.getByRole('table').all()).toHaveLength(1)
    expect(screen.getByRole('row').all()).toHaveLength(LISTS.length + 1)
    expect(screen.getByRole('button', { name: 'Add lists' }).all()).toEqual([])
  })

  // The rail states what the profile already publishes, one format per line.
  // It is a fact about the stored profile, so it is captioned as the formats
  // themselves and not with the create form's question about where a new
  // profile should go. What saving does travels with the control that
  // does it, rather than standing above it as a sentence of its own.
  it('names the formats it publishes and attaches the effect to Save', async () => {
    stubPreview()
    const screen = await renderEditor()
    await settle()

    await expect
      .element(screen.getByText('Formats', { exact: true }))
      .toBeVisible()
    await expect
      .element(screen.getByText('Where to deliver the profile'))
      .not.toBeInTheDocument()
    for (const output of OUTPUTS)
      await expect
        .element(screen.getByText(output.title, { exact: true }))
        .toBeVisible()

    await expect
      .element(
        screen.getByText('Saving rebuilds every connection of this profile.'),
      )
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: 'What happens when saving' }))
      .toBeVisible()
  })

  it('does not recalculate when output objects refresh without changing the formats', async () => {
    const fetchMock = stubPreview()
    const screen = await renderEditor()
    await vi.advanceTimersByTimeAsync(600)
    await settle()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await screen.rerender({
      outputs: OUTPUTS.map((output) => ({ ...output })),
    })
    await vi.advanceTimersByTimeAsync(600)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await screen.rerender({ outputs: [OUTPUTS[1]!] })
    await vi.advanceTimersByTimeAsync(600)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('weighs each list in the first format the profile publishes', async () => {
    const fetchMock = stubPreview()
    await vi.advanceTimersByTimeAsync(600)
    const screen = await renderEditor()
    await vi.advanceTimersByTimeAsync(600)
    await settle()

    expect(fetchMock).toHaveBeenCalledWith('/v1/profiles/preview', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Routevane-Request': '1',
      },
      body: JSON.stringify({
        lists: ['youtube'],
        categories: ['communication'],
        exclusions: [],
        list_domains: {},
        priority: [],
        targets: ['keenetic', 'limited-fixture'],
      }),
    })

    // Each row states its own weight where a reader hears it.
    await expect
      .element(screen.getByRole('cell', { name: '≈ 3 rules' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('cell', { name: '≈ 1 rule' }))
      .toBeVisible()

    // A format that would refuse the draft says so; saving stays available,
    // because a failed rebuild is already reported per connection.
    await expect.element(screen.getByText(OVERFLOW)).toBeVisible()
  })

  // With nothing bound there is no format to weigh against, so the rows carry
  // no numbers at all rather than zeros.
  it('asks for no forecast when the profile publishes nowhere', async () => {
    const fetchMock = stubPreview()
    const screen = await renderEditor({ outputs: [] })
    await vi.advanceTimersByTimeAsync(600)
    await settle()

    expect(fetchMock).not.toHaveBeenCalled()
    await expect
      .element(screen.getByText('will not fit', { exact: false }))
      .not.toBeInTheDocument()
    expect(
      screen.getByRole('cell', { name: 'No forecast' }).all(),
    ).toHaveLength(3)
    await expect
      .element(screen.getByText('Add an output to check overlaps').first())
      .toBeVisible()
    await expect
      .element(screen.getByText('Overlaps unknown'))
      .not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' }).all()).toEqual([])
  })

  // The editor asks the same question the composer does, so it hands over the
  // same material: every resolved profile, not just how many there are.
  it('reads every unobserved list of the draft, then asks again', async () => {
    const fetchMock = stubPreview(() =>
      Promise.resolve(
        fetchMock.mock.calls.filter(
          (call) => !String(call[0]).endsWith('/refresh'),
        ).length === 1
          ? json({ error: 'nothing observed' }, 404)
          : json(FORECAST_PAYLOAD),
      ),
    )
    const screen = await renderEditor()
    await vi.advanceTimersByTimeAsync(600)
    await settle()

    expect(
      fetchMock.mock.calls
        .map((call) => String(call[0]))
        .filter((path) => path.endsWith('/refresh')),
    ).toEqual([
      '/v1/lists/discord/refresh',
      '/v1/lists/telegram/refresh',
      '/v1/lists/youtube/refresh',
    ])
    await expect.element(screen.getByText(OVERFLOW)).toBeVisible()
  })

  it('takes a list back out of the draft and saves what is left', async () => {
    stubPreview()
    const screen = await renderEditor()
    await settle()

    await screen
      .getByRole('checkbox', { name: 'Remove Discord from the profile' })
      .click()
    expect(composition(screen)).toEqual(['Telegram', 'YouTube'])

    await screen.getByRole('button', { name: SAVE }).click()
    expect(screen.emitted('save')?.at(-1)).toEqual([
      'Chat and video',
      {
        lists: ['youtube'],
        categories: ['communication'],
        // The category still carries Discord, so taking it out is recorded as
        // a standing exception rather than by freezing the members.
        exclusions: ['discord'],
        priority: ['telegram', 'youtube'],
        listDomains: {},
      },
    ])
  })

  // Save is offered when the stored profile and the draft say different things.
  // The stored profile arrives in the server's order and the draft is kept in a
  // normalised one, so comparing them as written offered a save for a profile
  // nobody had edited.
  it('offers a save for a different profile, not for a differently written one', async () => {
    stubPreview()
    const screen = await renderEditor({
      exclusions: ['discord', 'telegram'],
      listDomains: { youtube: ['a.example', 'b.example'] },
    })
    await settle()
    await expect
      .element(screen.getByRole('button', { name: CANCEL }))
      .not.toBeInTheDocument()

    await screen.rerender({
      exclusions: ['telegram', 'discord'],
      listDomains: { youtube: ['b.example', 'a.example'] },
    })
    await expect
      .element(screen.getByRole('button', { name: CANCEL }))
      .not.toBeInTheDocument()

    await screen.rerender({ exclusions: ['discord'] })
    await expect
      .element(screen.getByRole('button', { name: CANCEL }))
      .toBeVisible()
  })

  it('cancels the edited name and composition without saving', async () => {
    stubPreview()
    const screen = await renderEditor()
    await settle()
    const stored = composition(screen)

    const name = screen.getByLabelText('Name')
    await name.fill('Unsaved name')
    await screen
      .getByRole('checkbox', { name: 'Remove Discord from the profile' })
      .click()
    expect(composition(screen)).not.toEqual(stored)

    await screen.getByRole('button', { name: CANCEL }).click()

    await expect.element(name).toHaveValue('Chat and video')
    expect(composition(screen)).toEqual(stored)
    expect(screen.emitted('save')).toBeUndefined()
    await expect
      .element(screen.getByRole('button', { name: CANCEL }))
      .not.toBeInTheDocument()
  })

  it('stops following a category when its row is removed', async () => {
    stubPreview()
    const screen = await renderEditor()
    await settle()

    // Filter down to the category, stop following it, then clear the filter.
    await screen.getByRole('button', { name: /^Communication/ }).click()
    await screen.getByRole('button', { name: 'Follow “Communication”' }).click()
    await screen
      .getByRole('menuitem', { name: 'Select new lists manually' })
      .click()
    await screen.getByRole('button', { name: 'All categories' }).click()

    expect(composition(screen)).toEqual(['YouTube'])
    await screen.getByRole('button', { name: SAVE }).click()
    expect(screen.emitted('save')?.at(-1)?.[1]).toEqual({
      lists: ['youtube'],
      categories: [],
      exclusions: [],
      priority: ['youtube'],
      listDomains: {},
    })
  })

  it('saves the priority changed with the keyboard drag handle', async () => {
    stubPreview()
    const screen = await renderEditor()
    await settle()

    const handle = screen.getByRole('button', {
      name: 'Change priority of list Discord, position 1',
    })
    handle.element().focus()
    await userEvent.keyboard('{ArrowDown}')
    await screen.getByRole('button', { name: SAVE }).click()

    expect(screen.emitted('save')?.at(-1)?.[1]).toMatchObject({
      priority: ['telegram', 'discord', 'youtube'],
    })
  })
})
