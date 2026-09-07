import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { forgetForecastObservations } from '@/entities/profile-composition/model/forecast'
import { useLocale } from '@/shared/i18n/useLocale'

import ProfileEditor from './ProfileEditor.vue'

const categories = [
  {
    custom: false,
    id: 'communication',
    lists: ['discord', 'telegram'],
    title: 'Общение',
  },
]

const lists = [
  { categories: ['communication'], id: 'discord', title: 'Discord' },
  { categories: ['communication'], id: 'telegram', title: 'Telegram' },
  { categories: [], id: 'youtube', title: 'YouTube' },
]

const outputs = [
  { id: 'output-1', targetID: 'keenetic', title: 'Keenetic' },
  { id: 'output-2', targetID: 'limited-fixture', title: 'Limited fixture' },
]

const forecastPayload = {
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
    return (preview ?? (() => Promise.resolve(json(forecastPayload))))()
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const mountEditor = (
  overrides: Partial<{
    outputs: typeof outputs
    busy: boolean
    exclusions: string[]
    priority: string[]
    listDomains: Record<string, string[]>
  }> = {},
) =>
  mount(ProfileEditor, {
    props: {
      busy: false,
      categories,
      exclusions: [],
      name: 'Chat and video',
      outputs,
      selected: ['youtube'],
      selectedCategories: ['communication'],
      listDomains: {},
      lists,
      ...overrides,
    },
    global: { stubs: { RvIcon: true } },
  })

const buttonByLabel = (
  wrapper: ReturnType<typeof mountEditor>,
  label: string,
) =>
  wrapper
    .findAll('button')
    .find((button) => button.attributes('aria-label') === label)

const buttonByText = (wrapper: ReturnType<typeof mountEditor>, text: string) =>
  wrapper.findAll('button').find((button) => button.text() === text)

const rowCopies = (wrapper: ReturnType<typeof mountEditor>) =>
  wrapper.findAll('.picker__row--selected .picker__name')

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
    const wrapper = mountEditor()
    await flushPromises()

    expect(rowCopies(wrapper).map((row) => row.text())).toEqual([
      'Discord',
      'Telegram',
      'YouTube',
    ])
    expect(wrapper.find('.picker__table').exists()).toBe(true)
    expect(wrapper.findAll('.picker__row--selected')).toHaveLength(3)
    expect(buttonByText(wrapper, 'Add lists')).toBeUndefined()
    wrapper.unmount()
  })

  it('does not recalculate when output objects refresh without changing the formats', async () => {
    const fetchMock = stubPreview()
    const wrapper = mountEditor()
    await vi.advanceTimersByTimeAsync(600)
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await wrapper.setProps({
      outputs: outputs.map((output) => ({ ...output })),
    })
    await vi.advanceTimersByTimeAsync(600)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await wrapper.setProps({ outputs: [outputs[1]!] })
    await vi.advanceTimersByTimeAsync(600)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('weighs each list in the first format the profile publishes', async () => {
    const fetchMock = stubPreview()
    vi.advanceTimersByTime(600)
    const wrapper = mountEditor()
    vi.advanceTimersByTime(600)
    await flushPromises()

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

    expect(
      wrapper
        .get('.picker__row[data-id="discord"] .picker__rules-column')
        .attributes('aria-label'),
    ).toBe('≈ 3 rules')
    expect(
      wrapper
        .get('.picker__row[data-id="youtube"] .picker__rules-column')
        .attributes('aria-label'),
    ).toBe('≈ 1 rule')

    // A format that would refuse the draft says so; saving stays available,
    // because a failed rebuild is already reported per connection.
    expect(wrapper.get('.editor__forecast').text()).toBe(
      'Limited fixture: ≈ 6 of 1 — will not fit',
    )
    wrapper.unmount()
  })

  // With nothing bound there is no format to weigh against, so the rows carry
  // no numbers at all rather than zeros.
  it('asks for no forecast when the profile publishes nowhere', async () => {
    const fetchMock = stubPreview()
    const wrapper = mountEditor({ outputs: [] })
    vi.advanceTimersByTime(600)
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.find('.editor__forecast').exists()).toBe(false)
    expect(wrapper.findAll('td[aria-label="No forecast"]')).toHaveLength(3)
    expect(wrapper.text()).toContain('Add an output to check overlaps')
    expect(wrapper.text()).not.toContain('Overlaps unknown')
    expect(buttonByText(wrapper, 'Retry')).toBeUndefined()
    wrapper.unmount()
  })

  // The editor asks the same question the composer does, so it hands over the
  // same material: every resolved profile, not just how many there are.
  it('reads every unobserved list of the draft, then asks again', async () => {
    let previews = 0
    const fetchMock = stubPreview(() => {
      previews += 1
      return Promise.resolve(
        previews === 1
          ? json({ error: 'nothing observed' }, 404)
          : json(forecastPayload),
      )
    })
    const wrapper = mountEditor()
    vi.advanceTimersByTime(600)
    await flushPromises()

    expect(
      fetchMock.mock.calls
        .map((call) => String(call[0]))
        .filter((path) => path.endsWith('/refresh')),
    ).toEqual([
      '/v1/lists/discord/refresh',
      '/v1/lists/telegram/refresh',
      '/v1/lists/youtube/refresh',
    ])
    expect(wrapper.get('.editor__forecast').text()).toBe(
      'Limited fixture: ≈ 6 of 1 — will not fit',
    )
    wrapper.unmount()
  })

  it('takes a list back out of the draft and saves what is left', async () => {
    stubPreview()
    const wrapper = mountEditor()
    await flushPromises()

    await wrapper.get('input[value="discord"]').setValue(false)
    expect(rowCopies(wrapper).map((row) => row.text())).not.toContain(
      expect.stringContaining('Discord'),
    )

    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')?.at(-1)).toEqual([
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
    wrapper.unmount()
  })

  // Save is offered when the stored profile and the draft say different things.
  // The stored profile arrives in the server's order and the draft is kept in a
  // normalised one, so comparing them as written offered a save for a profile
  // nobody had edited.
  it('offers a save for a different profile, not for a differently written one', async () => {
    stubPreview()
    const wrapper = mountEditor({
      exclusions: ['discord', 'telegram'],
      listDomains: { youtube: ['a.example', 'b.example'] },
    })
    await flushPromises()
    expect(buttonByText(wrapper, 'Cancel')).toBeUndefined()

    await wrapper.setProps({
      exclusions: ['telegram', 'discord'],
      listDomains: { youtube: ['b.example', 'a.example'] },
    })
    expect(buttonByText(wrapper, 'Cancel')).toBeUndefined()

    await wrapper.setProps({ exclusions: ['discord'] })
    expect(buttonByText(wrapper, 'Cancel')).toBeDefined()
    wrapper.unmount()
  })

  it('cancels the edited name and composition without saving', async () => {
    stubPreview()
    const wrapper = mountEditor()
    await flushPromises()
    const storedRows = wrapper
      .findAll('.picker__row--selected .picker__name')
      .map((row) => row.text())
    await wrapper.get('#editor-name').setValue('Unsaved name')
    await wrapper.get('input[value="discord"]').setValue(false)
    expect(rowCopies(wrapper).map((row) => row.text())).not.toEqual(storedRows)
    const cancel = buttonByText(wrapper, 'Cancel')
    expect(cancel).toBeDefined()
    await cancel!.trigger('click')
    expect(
      (wrapper.get('#editor-name').element as HTMLInputElement).value,
    ).toBe('Chat and video')
    expect(
      wrapper
        .findAll('.picker__row--selected .picker__name')
        .map((row) => row.text()),
    ).toEqual(storedRows)
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(buttonByText(wrapper, 'Cancel')).toBeUndefined()
    wrapper.unmount()
  })

  it('stops following a category when its row is removed', async () => {
    stubPreview()
    const wrapper = mountEditor()
    await flushPromises()

    wrapper
      .findComponent({ name: 'CategoryFilters' })
      .vm.$emit('update:modelValue', ['communication'])
    await wrapper.vm.$nextTick()
    wrapper.findComponent({ name: 'RvMenu' }).vm.$emit('select', 'manual')
    await wrapper.vm.$nextTick()

    wrapper
      .findComponent({ name: 'CategoryFilters' })
      .vm.$emit('update:modelValue', [])
    await wrapper.vm.$nextTick()
    expect(rowCopies(wrapper).map((row) => row.text())).toEqual(['YouTube'])
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')?.at(-1)?.[1]).toEqual({
      lists: ['youtube'],
      categories: [],
      exclusions: [],
      priority: ['youtube'],
      listDomains: {},
    })
    wrapper.unmount()
  })

  it('saves the priority changed with the keyboard drag handle', async () => {
    stubPreview()
    const wrapper = mountEditor()
    await flushPromises()

    const handle = buttonByLabel(
      wrapper,
      'Change priority of list Discord, position 1',
    )
    expect(handle).toBeDefined()
    await handle!.trigger('keydown', { key: 'ArrowDown' })
    await wrapper.get('form').trigger('submit')

    expect(wrapper.emitted('save')?.at(-1)?.[1]).toMatchObject({
      priority: ['telegram', 'discord', 'youtube'],
    })
    wrapper.unmount()
  })
})
