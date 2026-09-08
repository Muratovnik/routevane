import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render, type RenderResult } from 'vitest-browser-vue'

import { forgetForecastObservations } from '@/entities/profile-composition/model/forecast'
import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

import CreateProfile from '@/features/create-profile/ui/CreateProfile.vue'

const SUBMIT = 'Create and prepare'
const TARGET_FIELD = 'Where to deliver the profile'
const SETTINGS = 'Profile settings'
const OVERFLOW_WARNING = 'Cannot hold this profile'

const CHOSEN =
  /^Change priority of list (?<list>.+), position (?<position>\d+)$/

const CATALOG_PAYLOAD = {
  lists: ['discord', 'limit-fixture'],
  default_priority: ['limit-fixture', 'discord'],
  list_details: [
    { id: 'discord', title: 'Discord', categories: [] },
    { id: 'limit-fixture', title: 'Limit fixture', categories: [] },
  ],
  categories: [],
}

const target = (
  id: string,
  title: string,
  kind: 'router' | 'app',
): Record<string, unknown> => ({
  id,
  title,
  kind,
  format_key: `${id}-v1`,
  renderer_id: 'keenetic-route-bat',
  file_extension: 'bat',
  manual_installation_hint: 'Install it by hand.',
})

const TARGETS_PAYLOAD = {
  targets: [
    target('keenetic', 'Keenetic', 'router'),
    target('limited-fixture', 'Limited fixture', 'app'),
    target('singbox', 'sing-box', 'app'),
  ],
}

// The one-rule format cannot hold the two rules this list needs; the other
// two can. These are the same numbers the browser fixture produces.
const FORECAST_PAYLOAD = {
  targets: [
    {
      target_id: 'keenetic',
      maximum_rules: 1024,
      projected_rules: 2,
      fits: true,
      per_list: [{ list_id: 'limit-fixture', rules: 2 }],
    },
    {
      target_id: 'limited-fixture',
      maximum_rules: 1,
      projected_rules: 2,
      fits: false,
      per_list: [{ list_id: 'limit-fixture', rules: 2 }],
    },
    {
      target_id: 'singbox',
      maximum_rules: 8192,
      projected_rules: 2,
      fits: true,
      per_list: [{ list_id: 'limit-fixture', rules: 2 }],
    },
  ],
}

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

// A composition nothing has ever observed is not malformed; the service simply
// cannot answer it yet, and says so with 404.
const unobserved = (): Response =>
  json({ error: 'nothing observed for this composition' }, 404)

type Network = {
  catalog?: () => Promise<Response> | Response
  preview?: () => Promise<Response>
  refresh?: () => Promise<Response>
}

const stubNetwork = (overrides: Network = {}) => {
  const fetchMock = vi.fn((input: unknown) => {
    const path = String(input)
    if (path === '/v1/lists')
      return Promise.resolve(overrides.catalog?.() ?? json(CATALOG_PAYLOAD))
    if (path === '/v1/targets') return Promise.resolve(json(TARGETS_PAYLOAD))
    if (path === '/v1/deployments/targets')
      return Promise.resolve(json({ targets: [] }))
    if (path === '/v1/profiles/preview')
      return (
        overrides.preview ?? (() => Promise.resolve(json(FORECAST_PAYLOAD)))
      )()
    if (path.endsWith('/refresh'))
      return (
        overrides.refresh ?? (() => Promise.resolve(json({ refresh: {} })))
      )()
    return Promise.reject(new Error(`unexpected request ${path}`))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const previewCalls = (fetchMock: { mock: { calls: unknown[][] } }): string[] =>
  fetchMock.mock.calls
    .filter((call) => String(call[0]) === '/v1/profiles/preview')
    .map((call) => String((call[1] as RequestInit | undefined)?.body ?? ''))

const refreshCalls = (fetchMock: { mock: { calls: unknown[][] } }): string[] =>
  fetchMock.mock.calls
    .map((call) => String(call[0]))
    .filter((path) => path.endsWith('/refresh'))

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

const renderComposer = () => render(CreateProfile)

// A row states its own priority in its handle. The rows are drawn in the order
// the operator arranged them rather than in priority order, so the composition
// is read back through that stated position.
const composition = (screen: RenderResult<unknown>): string[] =>
  screen
    .getByRole('button', { name: CHOSEN })
    .elements()
    .map(
      (handle) =>
        CHOSEN.exec(handle.getAttribute('aria-label') ?? '')?.groups ?? {},
    )
    .map((groups) => ({
      list: groups.list ?? '',
      position: Number(groups.position ?? 0),
    }))
    .sort((first, second) => first.position - second.position)
    .map((row) => row.list)

const chooseList = (screen: RenderResult<unknown>, title: string) =>
  screen.getByRole('checkbox', { name: `Add ${title} to the profile` }).click()

const dropList = (screen: RenderResult<unknown>, title: string) =>
  screen
    .getByRole('checkbox', { name: `Remove ${title} from the profile` })
    .click()

// The format list is a portalled overlay opened to be read, which is also how
// an operator meets every format's size.
const openTargets = async (screen: RenderResult<unknown>): Promise<void> => {
  await screen.getByLabelText(TARGET_FIELD).click()
  await expect.element(screen.getByRole('listbox')).toBeVisible()
}

const chooseTarget = async (
  screen: RenderResult<unknown>,
  label: string,
): Promise<void> => {
  await openTargets(screen)
  await screen.getByRole('option', { name: new RegExp(`^${label}`) }).click()
}

describe('CreateProfile forecast', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    invalidateCatalogCache()
    forgetForecastObservations()
    useLocale().setLocale('en')
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('asks once the draft settles and states each format against its bound', async () => {
    const fetchMock = stubNetwork()
    const screen = await renderComposer()
    await settle()

    // An empty draft has nothing to weigh, and the endpoint refuses it.
    expect(previewCalls(fetchMock)).toEqual([])

    await chooseList(screen, 'Limit fixture')
    await vi.advanceTimersByTimeAsync(400)
    await settle()
    expect(previewCalls(fetchMock)).toEqual([])

    await vi.advanceTimersByTimeAsync(200)
    await settle()
    expect(previewCalls(fetchMock)).toEqual([
      JSON.stringify({
        lists: ['limit-fixture'],
        categories: [],
        exclusions: [],
        list_domains: {},
        priority: ['limit-fixture'],
      }),
    ])

    // Every format states what this draft would weigh in it, against its own
    // bound, before one is chosen. Four figures are read as a quantity, not as
    // a serial number, so the bound carries this locale's group separator.
    await openTargets(screen)
    await expect
      .element(screen.getByText('≈ 2 of 1,024 rules', { exact: false }))
      .toBeVisible()
    await expect
      .element(screen.getByText('≈ 2 of 1 rules', { exact: false }))
      .toBeVisible()
    await expect
      .element(screen.getByText(OVERFLOW_WARNING, { exact: false }).first())
      .toBeVisible()
  })

  // The profile is named before it is filled in. The name field asked last while
  // proposing itself from the catalog above it, which read as a summary of what
  // had been picked rather than as the first thing the form wants.
  it('keeps the editable proposed name in profile settings', async () => {
    stubNetwork()
    const screen = await renderComposer()
    await settle()

    const settings = screen.getByRole('complementary', { name: SETTINGS })
    await expect.element(settings.getByLabelText('Name')).toBeVisible()

    await chooseList(screen, 'Discord')
    await expect.element(screen.getByLabelText('Name')).toHaveValue('Discord')
  })

  it('applies library priority in the table and removes by checkbox', async () => {
    stubNetwork()
    const screen = await renderComposer()
    await settle()

    await expect
      .element(screen.getByRole('complementary', { name: SETTINGS }))
      .toBeVisible()
    // The composition is the picker's own table rather than a second list
    // beside it.
    await expect
      .element(screen.getByRole('region', { name: 'In the profile' }))
      .not.toBeInTheDocument()

    await chooseList(screen, 'Discord')
    await chooseList(screen, 'Limit fixture')
    expect(composition(screen)).toEqual(['Limit fixture', 'Discord'])
    await expect
      .element(screen.getByText('Choose a format to check overlaps').first())
      .toBeVisible()
    await expect
      .element(screen.getByText('Overlaps unknown'))
      .not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' }).all()).toEqual([])

    await dropList(screen, 'Limit fixture')
    expect(composition(screen)).toEqual(['Discord'])
  })

  it('adopts a reread library order until the profile order is edited', async () => {
    const library = { priority: ['limit-fixture', 'discord'] }
    stubNetwork({
      catalog: () =>
        json({ ...CATALOG_PAYLOAD, default_priority: library.priority }),
    })
    const screen = await renderComposer()
    await settle()
    await chooseList(screen, 'Discord')
    await chooseList(screen, 'Limit fixture')
    expect(composition(screen)).toEqual(['Limit fixture', 'Discord'])

    // Landing on a control focuses this window, which is itself a reason to
    // re-read: let those reads finish before the library changes underneath.
    await settle()
    await settle()

    library.priority = ['discord', 'limit-fixture']
    window.dispatchEvent(new Event('focus'))
    // Returning to the window re-reads the catalog, which is a round trip
    // through the browser's own queue rather than a tick of the fake clock.
    // Returning to the window re-reads the catalog, which is a round trip
    // through the browser's own queue rather than a tick of the fake clock.
    await vi.waitFor(async () => {
      await settle()
      expect(composition(screen)).toEqual(['Discord', 'Limit fixture'])
    })

    // An edited order is the operator's own, so a later reread leaves it alone.
    const handle = screen.getByRole('button', {
      name: 'Change priority of list Discord, position 1',
    })
    handle.element().focus()
    await userEvent.keyboard('{ArrowDown}')
    expect(composition(screen)).toEqual(['Limit fixture', 'Discord'])

    window.dispatchEvent(new Event('focus'))
    await settle()
    expect(composition(screen)).toEqual(['Limit fixture', 'Discord'])
  })

  // Several clicks in a row are one question, and only the answer to the last
  // one is allowed to land.
  it('collapses a burst of edits into a single read', async () => {
    const fetchMock = stubNetwork()
    const screen = await renderComposer()
    await settle()

    await chooseList(screen, 'Limit fixture')
    await vi.advanceTimersByTimeAsync(200)
    await chooseList(screen, 'Discord')
    await vi.advanceTimersByTimeAsync(600)
    await settle()

    expect(previewCalls(fetchMock)).toEqual([
      JSON.stringify({
        lists: ['discord', 'limit-fixture'],
        categories: [],
        exclusions: [],
        list_domains: {},
        priority: ['limit-fixture', 'discord'],
      }),
    ])
  })

  it('refuses the overflowing pair and offers one that fits', async () => {
    stubNetwork()
    const screen = await renderComposer()
    await settle()
    await chooseList(screen, 'Limit fixture')
    await vi.advanceTimersByTimeAsync(600)
    await settle()

    // A format that holds the profile is created without comment, and the
    // chosen one keeps its size on the screen after the profile closes over it.
    const submit = screen.getByRole('button', { name: SUBMIT })
    await chooseTarget(screen, 'Keenetic')
    await expect.element(submit).toBeEnabled()
    await expect
      .element(screen.getByLabelText(TARGET_FIELD))
      .toHaveTextContent('Keenetic')
    await expect.element(screen.getByText('≈ 2 of 1,024 rules')).toBeVisible()

    await chooseTarget(screen, 'Limited fixture')
    await expect.element(submit).toBeDisabled()
    await expect
      .element(screen.getByText('The profile does not fit Limited fixture.'))
      .toBeVisible()
    await expect
      .element(screen.getByText('sing-box would fit.', { exact: false }))
      .toBeVisible()

    // The way out is one click, and it lands on the same choice the operator
    // would have had to find themselves.
    await screen.getByRole('button', { name: 'Choose sing-box' }).click()
    await expect
      .element(screen.getByLabelText(TARGET_FIELD))
      .toHaveTextContent('sing-box')
    await expect.element(submit).toBeEnabled()
  })

  // A composition nothing has observed is the flagship case: a fresh install
  // where the operator picks «Видео» and the guard would otherwise stay silent
  // through exactly the pair it exists to refuse.
  it('observes an unread draft once and then asks again', async () => {
    const previews: string[] = []
    const fetchMock = stubNetwork({
      preview: () => {
        previews.push('preview')
        return Promise.resolve(
          previews.length === 1 ? unobserved() : json(FORECAST_PAYLOAD),
        )
      },
    })
    const screen = await renderComposer()
    await settle()

    await chooseList(screen, 'Limit fixture')
    await vi.advanceTimersByTimeAsync(600)
    await settle()

    expect(refreshCalls(fetchMock)).toEqual(['/v1/lists/limit-fixture/refresh'])
    expect(previewCalls(fetchMock)).toHaveLength(2)

    await openTargets(screen)
    await expect
      .element(screen.getByText('≈ 2 of 1 rules', { exact: false }))
      .toBeVisible()
    await expect
      .element(screen.getByText(OVERFLOW_WARNING, { exact: false }).first())
      .toBeVisible()
  })

  // Once per list and once per draft. A catalog that genuinely cannot be
  // forecast costs one round trip, never a loop.
  it('never reads the same list twice, whatever the operator does next', async () => {
    const fetchMock = stubNetwork({
      preview: () => Promise.resolve(unobserved()),
    })
    const screen = await renderComposer()
    await settle()

    await chooseList(screen, 'Limit fixture')
    await vi.advanceTimersByTimeAsync(600)
    await settle()

    // One refusal, one read, one re-ask — and the second refusal is final.
    expect(refreshCalls(fetchMock)).toEqual(['/v1/lists/limit-fixture/refresh'])
    expect(previewCalls(fetchMock)).toHaveLength(2)

    // A second list joins the draft: only the one never read is read.
    await chooseList(screen, 'Discord')
    await vi.advanceTimersByTimeAsync(600)
    await settle()

    expect(refreshCalls(fetchMock)).toEqual([
      '/v1/lists/limit-fixture/refresh',
      '/v1/lists/discord/refresh',
    ])
    expect(previewCalls(fetchMock)).toHaveLength(4)

    // Taking it back out asks once more and reads nothing at all.
    await dropList(screen, 'Discord')
    await vi.advanceTimersByTimeAsync(600)
    await settle()

    expect(refreshCalls(fetchMock)).toHaveLength(2)
    expect(previewCalls(fetchMock)).toHaveLength(5)

    await openTargets(screen)
    await expect
      .element(screen.getByText(OVERFLOW_WARNING, { exact: false }))
      .not.toBeInTheDocument()
  })

  it('lets a fresh edit supersede a retry that is still reading', async () => {
    const held = Promise.withResolvers<undefined>()
    const fetchMock = stubNetwork({
      preview: () => Promise.resolve(unobserved()),
      refresh: () => held.promise.then(() => json({ refresh: {} })),
    })
    const screen = await renderComposer()
    await settle()

    await chooseList(screen, 'Limit fixture')
    await vi.advanceTimersByTimeAsync(600)
    await settle()
    expect(previewCalls(fetchMock)).toHaveLength(1)

    // The draft moves on while the sources are still being read. The answer
    // that read was going to fetch no longer describes anything on screen.
    await chooseList(screen, 'Discord')
    held.resolve(undefined)
    await settle()

    expect(previewCalls(fetchMock)).toHaveLength(1)
  })

  // The forecast is a guard, not a gate on availability: a refused preview
  // leaves the screen saying nothing and creating still possible.
  it('says nothing and blocks nothing when the forecast is refused', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: unknown) => {
        const path = String(input)
        if (path === '/v1/lists') return Promise.resolve(json(CATALOG_PAYLOAD))
        if (path === '/v1/targets')
          return Promise.resolve(json(TARGETS_PAYLOAD))
        if (path === '/v1/deployments/targets')
          return Promise.resolve(json({ targets: [] }))
        return Promise.reject(new Error('offline'))
      }),
    )
    const screen = await renderComposer()
    await settle()

    await chooseList(screen, 'Limit fixture')
    await chooseTarget(screen, 'Limited fixture')
    await vi.advanceTimersByTimeAsync(600)
    await settle()

    await expect
      .element(screen.getByText(OVERFLOW_WARNING, { exact: false }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: SUBMIT }))
      .toBeEnabled()
  })
  it('shows a stale library read, retries in place, and keeps the chosen draft', async () => {
    const catalogRead = vi
      .fn()
      .mockReturnValueOnce(json(CATALOG_PAYLOAD))
      .mockReturnValueOnce(json({ error: 'offline' }, 503))
      .mockReturnValue(json(CATALOG_PAYLOAD))
    stubNetwork({ catalog: catalogRead })
    const screen = await renderComposer()
    await settle()
    await chooseList(screen, 'Discord')
    await screen.getByLabelText('Name').fill('My unsaved profile')
    window.dispatchEvent(new Event('focus'))
    await settle()
    await expect
      .element(screen.getByText('The library could not be refreshed'))
      .toBeVisible()
    await screen.getByRole('button', { name: 'Retry', exact: true }).click()
    await settle()
    await expect
      .element(screen.getByText('The library could not be refreshed'))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByLabelText('Name'))
      .toHaveValue('My unsaved profile')
    await expect
      .element(
        screen.getByRole('checkbox', {
          name: 'Remove Discord from the profile',
        }),
      )
      .toBeChecked()
    expect(catalogRead).toHaveBeenCalledTimes(3)
  })
})
