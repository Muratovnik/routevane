import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { forgetForecastObservations } from '@/entities/profile-composition/model/forecast'
import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

import CreateProfile from './CreateProfile.vue'

const catalogPayload = {
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

const targetsPayload = {
  targets: [
    target('keenetic', 'Keenetic', 'router'),
    target('limited-fixture', 'Limited fixture', 'app'),
    target('singbox', 'sing-box', 'app'),
  ],
}

// The one-rule format cannot hold the two rules this list needs; the other
// two can. These are the same numbers the browser fixture produces.
const forecastPayload = {
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
      return Promise.resolve(overrides.catalog?.() ?? json(catalogPayload))
    if (path === '/v1/targets') return Promise.resolve(json(targetsPayload))
    if (path === '/v1/deployments/targets')
      return Promise.resolve(json({ targets: [] }))
    if (path === '/v1/profiles/preview')
      return (
        overrides.preview ?? (() => Promise.resolve(json(forecastPayload)))
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

const mountComposer = () =>
  mount(CreateProfile, {
    attachTo: document.body,
    global: { stubs: { RvIcon: true } },
  })

const buttonWithText = (
  wrapper: ReturnType<typeof mountComposer>,
  text: string,
) => wrapper.findAll('button').find((button) => button.text() === text)

// The format list is a portalled overlay, so it is read on the document. It is
// opened to read it, which is also how an operator meets every format's size.
const openTargets = async (
  wrapper: ReturnType<typeof mountComposer>,
): Promise<HTMLElement> => {
  await wrapper.get('.rv-search-select__trigger--field').trigger('click')
  await flushPromises()
  const profile = document.body.querySelector<HTMLElement>('[role="listbox"]')
  expect(profile).not.toBeNull()
  return profile as HTMLElement
}

const chooseTarget = async (
  wrapper: ReturnType<typeof mountComposer>,
  label: string,
): Promise<void> => {
  const profile = await openTargets(wrapper)
  const option = [
    ...profile.querySelectorAll<HTMLElement>('[role="option"]'),
  ].find((candidate) => candidate.textContent?.includes(label))
  expect(option, label).toBeDefined()
  option?.click()
  await flushPromises()
}

const chosenTarget = (wrapper: ReturnType<typeof mountComposer>): string =>
  wrapper.get('#create-target').text()

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
    const wrapper = mountComposer()
    await flushPromises()

    // An empty draft has nothing to weigh, and the endpoint refuses it.
    expect(previewCalls(fetchMock)).toEqual([])

    await wrapper.get('input[value="limit-fixture"]').setValue(true)
    vi.advanceTimersByTime(400)
    await flushPromises()
    expect(previewCalls(fetchMock)).toEqual([])

    vi.advanceTimersByTime(200)
    await flushPromises()
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
    // bound, before one is chosen.
    const text = (await openTargets(wrapper)).textContent ?? ''
    // Four figures are read as a quantity, not as a serial number, so the
    // bound carries the group separator this locale uses.
    expect(text).toContain('≈ 2 of 1,024 rules')
    expect(text).toContain('≈ 2 of 1 rules')
    expect(text).toContain('Cannot hold this profile')
    wrapper.unmount()
  })

  // The profile is named before it is filled in. The name field asked last while
  // proposing itself from the catalog above it, which read as a summary of what
  // had been picked rather than as the first thing the form wants.
  it('keeps the editable proposed name in profile settings', async () => {
    stubNetwork()
    const wrapper = mountComposer()
    await flushPromises()

    expect(wrapper.find('.rv-composer__settings #create-name').exists()).toBe(
      true,
    )

    await wrapper.get('input[value="discord"]').setValue(true)
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('#create-name').element.value).toBe(
      'Discord',
    )
    wrapper.unmount()
  })

  it('applies library priority in the table and removes by checkbox', async () => {
    stubNetwork()
    const wrapper = mountComposer()
    await flushPromises()

    expect(wrapper.find('.rv-composer__settings').exists()).toBe(true)
    expect(wrapper.find('.priority-list').exists()).toBe(false)
    await wrapper.get('input[value="discord"]').setValue(true)
    await wrapper.get('input[value="limit-fixture"]').setValue(true)
    await flushPromises()
    expect(
      wrapper
        .findAll('.picker__row--selected')
        .sort(
          (a, b) =>
            Number(a.attributes('data-priority')) -
            Number(b.attributes('data-priority')),
        )
        .map((row) => row.get('.picker__name').text()),
    ).toEqual(['Limit fixture', 'Discord'])
    expect(wrapper.text()).toContain('Choose a format to check overlaps')
    expect(wrapper.text()).not.toContain('Overlaps unknown')
    expect(buttonWithText(wrapper, 'Retry')).toBeUndefined()
    expect(wrapper.findAll('.picker__handle')).toHaveLength(2)
    await wrapper.get('input[value="limit-fixture"]').setValue(false)
    expect(
      wrapper
        .findAll('.picker__row--selected')
        .sort(
          (a, b) =>
            Number(a.attributes('data-priority')) -
            Number(b.attributes('data-priority')),
        )
        .map((row) => row.get('.picker__name').text()),
    ).toEqual(['Discord'])
    wrapper.unmount()
  })

  it('adopts a reread library order until the profile order is edited', async () => {
    let priority = ['limit-fixture', 'discord']
    stubNetwork({
      catalog: () => json({ ...catalogPayload, default_priority: priority }),
    })
    const wrapper = mountComposer()
    await flushPromises()
    await wrapper.get('input[value="discord"]').setValue(true)
    await wrapper.get('input[value="limit-fixture"]').setValue(true)
    await flushPromises()
    expect(
      wrapper
        .findAll('.picker__row--selected')
        .sort(
          (a, b) =>
            Number(a.attributes('data-priority')) -
            Number(b.attributes('data-priority')),
        )
        .map((row) => row.get('.picker__name').text()),
    ).toEqual(['Limit fixture', 'Discord'])

    priority = ['discord', 'limit-fixture']
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(
      wrapper
        .findAll('.picker__row--selected')
        .sort(
          (a, b) =>
            Number(a.attributes('data-priority')) -
            Number(b.attributes('data-priority')),
        )
        .map((row) => row.get('.picker__name').text()),
    ).toEqual(['Discord', 'Limit fixture'])

    await wrapper
      .get('.picker__row[data-id="discord"] .picker__handle')
      .trigger('keydown', {
        key: 'ArrowDown',
      })
    expect(
      wrapper
        .findAll('.picker__row--selected')
        .sort(
          (a, b) =>
            Number(a.attributes('data-priority')) -
            Number(b.attributes('data-priority')),
        )
        .map((row) => row.get('.picker__name').text()),
    ).toEqual(['Limit fixture', 'Discord'])
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(
      wrapper
        .findAll('.picker__row--selected')
        .sort(
          (a, b) =>
            Number(a.attributes('data-priority')) -
            Number(b.attributes('data-priority')),
        )
        .map((row) => row.get('.picker__name').text()),
    ).toEqual(['Limit fixture', 'Discord'])
    wrapper.unmount()
  })

  // Several clicks in a row are one question, and only the answer to the last
  // one is allowed to land.
  it('collapses a burst of edits into a single read', async () => {
    const fetchMock = stubNetwork()
    const wrapper = mountComposer()
    await flushPromises()

    await wrapper.get('input[value="limit-fixture"]').setValue(true)
    vi.advanceTimersByTime(200)
    await wrapper.get('input[value="discord"]').setValue(true)
    vi.advanceTimersByTime(600)
    await flushPromises()

    expect(previewCalls(fetchMock)).toEqual([
      JSON.stringify({
        lists: ['discord', 'limit-fixture'],
        categories: [],
        exclusions: [],
        list_domains: {},
        priority: ['limit-fixture', 'discord'],
      }),
    ])
    wrapper.unmount()
  })

  it('refuses the overflowing pair and offers one that fits', async () => {
    stubNetwork()
    const wrapper = mountComposer()
    await flushPromises()
    await wrapper.get('input[value="limit-fixture"]').setValue(true)
    vi.advanceTimersByTime(600)
    await flushPromises()

    // A format that holds the profile is created without comment, and the chosen
    // one keeps its size on the screen after the profile closes over it.
    const submit = buttonWithText(wrapper, 'Create and prepare')
    await chooseTarget(wrapper, 'Keenetic')
    expect(submit?.attributes('disabled')).toBeUndefined()
    expect(chosenTarget(wrapper)).toBe('Keenetic')
    expect(wrapper.get('.create__forecast').text()).toBe('≈ 2 of 1,024 rules')

    await chooseTarget(wrapper, 'Limited fixture')
    expect(submit?.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain(
      'The profile does not fit Limited fixture.',
    )
    expect(wrapper.text()).toContain('sing-box would fit.')

    // The way out is one click, and it lands on the same choice the operator
    // would have had to find themselves.
    await buttonWithText(wrapper, 'Choose sing-box')?.trigger('click')
    await flushPromises()
    expect(chosenTarget(wrapper)).toBe('sing-box')
    expect(
      buttonWithText(wrapper, 'Create and prepare')?.attributes('disabled'),
    ).toBeUndefined()
    wrapper.unmount()
  })

  // A composition nothing has observed is the flagship case: a fresh install
  // where the operator picks «Видео» and the guard would otherwise stay silent
  // through exactly the pair it exists to refuse.
  it('observes an unread draft once and then asks again', async () => {
    let previews = 0
    const fetchMock = stubNetwork({
      preview: () => {
        previews += 1
        return Promise.resolve(
          previews === 1 ? unobserved() : json(forecastPayload),
        )
      },
    })
    const wrapper = mountComposer()
    await flushPromises()

    await wrapper.get('input[value="limit-fixture"]').setValue(true)
    vi.advanceTimersByTime(600)
    await flushPromises()

    expect(refreshCalls(fetchMock)).toEqual(['/v1/lists/limit-fixture/refresh'])
    expect(previewCalls(fetchMock)).toHaveLength(2)
    const listed = (await openTargets(wrapper)).textContent ?? ''
    expect(listed).toContain('≈ 2 of 1 rules')
    expect(listed).toContain('Cannot hold this profile')
    wrapper.unmount()
  })

  // Once per list and once per draft. A catalog that genuinely cannot be
  // forecast costs one round trip, never a loop.
  it('never reads the same list twice, whatever the operator does next', async () => {
    const fetchMock = stubNetwork({
      preview: () => Promise.resolve(unobserved()),
    })
    const wrapper = mountComposer()
    await flushPromises()

    await wrapper.get('input[value="limit-fixture"]').setValue(true)
    vi.advanceTimersByTime(600)
    await flushPromises()

    // One refusal, one read, one re-ask — and the second refusal is final.
    expect(refreshCalls(fetchMock)).toEqual(['/v1/lists/limit-fixture/refresh'])
    expect(previewCalls(fetchMock)).toHaveLength(2)

    // A second list joins the draft: only the one never read is read.
    await wrapper.get('input[value="discord"]').setValue(true)
    vi.advanceTimersByTime(600)
    await flushPromises()

    expect(refreshCalls(fetchMock)).toEqual([
      '/v1/lists/limit-fixture/refresh',
      '/v1/lists/discord/refresh',
    ])
    expect(previewCalls(fetchMock)).toHaveLength(4)

    // Taking it back out asks once more and reads nothing at all.
    await wrapper.get('input[value="discord"]').setValue(false)
    vi.advanceTimersByTime(600)
    await flushPromises()

    expect(refreshCalls(fetchMock)).toHaveLength(2)
    expect(previewCalls(fetchMock)).toHaveLength(5)
    expect((await openTargets(wrapper)).textContent).not.toContain(
      'Cannot hold this profile',
    )
    wrapper.unmount()
  })

  it('lets a fresh edit supersede a retry that is still reading', async () => {
    let release!: () => void
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    const fetchMock = stubNetwork({
      preview: () => Promise.resolve(unobserved()),
      refresh: () => held.then(() => json({ refresh: {} })),
    })
    const wrapper = mountComposer()
    await flushPromises()

    await wrapper.get('input[value="limit-fixture"]').setValue(true)
    vi.advanceTimersByTime(600)
    await flushPromises()
    expect(previewCalls(fetchMock)).toHaveLength(1)

    // The draft moves on while the sources are still being read. The answer
    // that read was going to fetch no longer describes anything on screen.
    await wrapper.get('input[value="discord"]').setValue(true)
    release()
    await flushPromises()

    expect(previewCalls(fetchMock)).toHaveLength(1)
    wrapper.unmount()
  })

  // The forecast is a guard, not a gate on availability: a refused preview
  // leaves the screen saying nothing and creating still possible.
  it('says nothing and blocks nothing when the forecast is refused', async () => {
    const fetchMock = vi.fn((input: unknown) => {
      const path = String(input)
      if (path === '/v1/lists') return Promise.resolve(json(catalogPayload))
      if (path === '/v1/targets') return Promise.resolve(json(targetsPayload))
      if (path === '/v1/deployments/targets')
        return Promise.resolve(json({ targets: [] }))
      return Promise.reject(new Error('offline'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mountComposer()
    await flushPromises()

    await wrapper.get('input[value="limit-fixture"]').setValue(true)
    await chooseTarget(wrapper, 'Limited fixture')
    vi.advanceTimersByTime(600)
    await flushPromises()

    expect(wrapper.text()).not.toContain('Cannot hold this profile')
    expect(
      buttonWithText(wrapper, 'Create and prepare')?.attributes('disabled'),
    ).toBeUndefined()
    wrapper.unmount()
  })
})
