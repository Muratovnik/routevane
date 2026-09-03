import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { forgetForecastObservations } from '@/entities/list-composition/model/forecast'
import { useLocale } from '@/shared/i18n/useLocale'

import ListEditor from './ListEditor.vue'

const categories = [
  {
    custom: false,
    id: 'communication',
    services: ['discord', 'telegram'],
    title: 'Общение',
  },
]

const services = [
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
      per_service: [
        { service_id: 'discord', rules: 3 },
        { service_id: 'telegram', rules: 2 },
        { service_id: 'youtube', rules: 1 },
      ],
    },
    {
      target_id: 'limited-fixture',
      maximum_rules: 1,
      projected_rules: 6,
      fits: false,
      per_service: [],
    },
  ],
}

function json(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function stubPreview(preview?: () => Promise<Response>) {
  const fetchMock = vi.fn((input: unknown) => {
    if (String(input).endsWith('/refresh'))
      return Promise.resolve(json({ refresh: {} }))
    return (preview ?? (() => Promise.resolve(json(forecastPayload))))()
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function mountEditor(
  overrides: Partial<{
    outputs: typeof outputs
    busy: boolean
    exclusions: string[]
    serviceDomains: Record<string, string[]>
  }> = {},
) {
  return mount(ListEditor, {
    props: {
      busy: false,
      categories,
      exclusions: [],
      name: 'Chat and video',
      outputs,
      selected: ['youtube'],
      selectedCategories: ['communication'],
      serviceDomains: {},
      services,
      ...overrides,
    },
    global: { stubs: { RvIcon: true } },
  })
}

function buttonByLabel(wrapper: ReturnType<typeof mountEditor>, label: string) {
  return wrapper
    .findAll('button')
    .find((button) => button.attributes('aria-label') === label)
}

function buttonByText(wrapper: ReturnType<typeof mountEditor>, text: string) {
  return wrapper.findAll('button').find((button) => button.text() === text)
}

describe('ListEditor', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    forgetForecastObservations()
    useLocale().setLocale('en')
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('states what the route holds before offering the catalog', async () => {
    stubPreview()
    const wrapper = mountEditor()
    await flushPromises()

    // The category is a reason some lists are here, so it is named above them
    // rather than mixed in with them.
    const rows = wrapper.findAll('.editor__row')
    expect(rows.map((row) => row.get('.editor__row-copy').text())).toEqual([
      'Communication2 lists',
      'Discord',
      'Telegram',
      'YouTube',
    ])

    // The catalog is a way to add, not the way to read what is already here.
    expect(wrapper.get('.editor__services').attributes('style')).toBe(
      'display: none;',
    )
    await buttonByText(wrapper, 'Add lists')?.trigger('click')
    expect(wrapper.get('.editor__services').attributes('style')).toBeUndefined()
    expect(wrapper.find('.picker').exists()).toBe(true)
    expect(buttonByText(wrapper, 'Add lists')).toBeUndefined()

    // What opened says so where it opened, and it is put away from the same
    // place. Closing it changes nothing about the draft it was opened over.
    const before = wrapper
      .findAll('.editor__row')
      .map((row) => row.get('.editor__row-copy').text())
    expect(wrapper.get('.editor__picker-title').text()).toBe('Add lists')
    await buttonByLabel(wrapper, 'Hide the catalog')?.trigger('click')

    expect(wrapper.get('.editor__services').attributes('style')).toBe(
      'display: none;',
    )
    expect(buttonByText(wrapper, 'Add lists')).toBeDefined()
    expect(
      wrapper
        .findAll('.editor__row')
        .map((row) => row.get('.editor__row-copy').text()),
    ).toEqual(before)
    wrapper.unmount()
  })

  it('weighs each list in the first format the route publishes', async () => {
    const fetchMock = stubPreview()
    vi.advanceTimersByTime(600)
    const wrapper = mountEditor()
    vi.advanceTimersByTime(600)
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/v1/lists/preview', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Routevane-Request': '1',
      },
      body: JSON.stringify({
        services: ['youtube'],
        categories: ['communication'],
        exclusions: [],
        service_domains: {},
        targets: ['keenetic', 'limited-fixture'],
      }),
    })

    const rows = wrapper.findAll('.editor__row')
    expect(rows[1]?.text()).toContain('≈ 3 rules')
    expect(rows[3]?.text()).toContain('≈ 1 rule')

    // A format that would refuse the draft says so; saving stays available,
    // because a failed rebuild is already reported per connection.
    expect(wrapper.get('.editor__forecast').text()).toBe(
      'Limited fixture: ≈ 6 of 1 — will not fit',
    )
    wrapper.unmount()
  })

  // With nothing bound there is no format to weigh against, so the rows carry
  // no numbers at all rather than zeros.
  it('asks for no forecast when the route publishes nowhere', async () => {
    const fetchMock = stubPreview()
    const wrapper = mountEditor({ outputs: [] })
    vi.advanceTimersByTime(600)
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.find('.editor__forecast').exists()).toBe(false)
    expect(wrapper.findAll('.editor__row-copy small')).toHaveLength(1)
    wrapper.unmount()
  })

  // The editor asks the same question the composer does, so it hands over the
  // same material: every resolved list, not just how many there are.
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
      '/v1/services/discord/refresh',
      '/v1/services/telegram/refresh',
      '/v1/services/youtube/refresh',
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

    await buttonByLabel(wrapper, 'Remove Discord from the route')?.trigger(
      'click',
    )
    expect(
      wrapper.findAll('.editor__row').map((row) => row.text()),
    ).not.toContain(expect.stringContaining('Discord'))

    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')?.at(-1)).toEqual([
      'Chat and video',
      {
        services: ['youtube'],
        categories: ['communication'],
        // The category still carries Discord, so taking it out is recorded as
        // a standing exception rather than by freezing the members.
        exclusions: ['discord'],
        serviceDomains: {},
      },
    ])
    wrapper.unmount()
  })

  // Save is offered when the stored route and the draft say different things.
  // The stored route arrives in the server's order and the draft is kept in a
  // normalised one, so comparing them as written offered a save for a route
  // nobody had edited.
  it('offers a save for a different route, not for a differently written one', async () => {
    stubPreview()
    const wrapper = mountEditor({
      exclusions: ['discord', 'telegram'],
      serviceDomains: { youtube: ['a.example', 'b.example'] },
    })
    await flushPromises()
    expect(buttonByText(wrapper, 'Cancel')).toBeUndefined()

    await wrapper.setProps({
      exclusions: ['telegram', 'discord'],
      serviceDomains: { youtube: ['b.example', 'a.example'] },
    })
    expect(buttonByText(wrapper, 'Cancel')).toBeUndefined()

    await wrapper.setProps({ exclusions: ['discord'] })
    expect(buttonByText(wrapper, 'Cancel')).toBeDefined()
    wrapper.unmount()
  })

  it('cancels the edited name and composition without saving and closes the picker', async () => {
    stubPreview()
    const wrapper = mountEditor()
    await flushPromises()
    const storedRows = wrapper
      .findAll('.editor__row-copy')
      .map((row) => row.text())
    await wrapper.get('#editor-name').setValue('Unsaved name')
    const remove = buttonByLabel(wrapper, 'Remove Communication from the route')
    expect(remove).toBeDefined()
    await remove!.trigger('click')
    await buttonByText(wrapper, 'Add lists')?.trigger('click')
    expect(
      wrapper.findAll('.editor__row-copy').map((row) => row.text()),
    ).not.toEqual(storedRows)
    expect(wrapper.get('.editor__services').attributes('style')).toBeUndefined()
    const cancel = buttonByText(wrapper, 'Cancel')
    expect(cancel).toBeDefined()
    await cancel!.trigger('click')
    expect(
      (wrapper.get('#editor-name').element as HTMLInputElement).value,
    ).toBe('Chat and video')
    expect(
      wrapper.findAll('.editor__row-copy').map((row) => row.text()),
    ).toEqual(storedRows)
    expect(wrapper.get('.editor__services').attributes('style')).toBe(
      'display: none;',
    )
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(buttonByText(wrapper, 'Cancel')).toBeUndefined()
    wrapper.unmount()
  })

  it('stops following a category when its row is removed', async () => {
    stubPreview()
    const wrapper = mountEditor()
    await flushPromises()

    await buttonByLabel(
      wrapper,
      'Remove Communication from the route',
    )?.trigger('click')

    expect(
      wrapper.findAll('.editor__row-copy').map((row) => row.text()),
    ).toEqual(['YouTube'])
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')?.at(-1)?.[1]).toEqual({
      services: ['youtube'],
      categories: [],
      exclusions: [],
      serviceDomains: {},
    })
    wrapper.unmount()
  })
})
