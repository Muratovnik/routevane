import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useLocale } from '@/shared/i18n/useLocale'

import SendPage from './[output].vue'

const pageModel = () => ({
  catalog: ref(null),
  initialize: vi.fn(async () => {}),
  profile: ref({ name: 'Example profile' }),
  outputs: ref([
    { id: 'first', latest: { id: 'artifact-1' }, targetID: 'keenetic' },
    { id: 'chosen', latest: { id: 'artifact-2' }, targetID: 'keenetic' },
  ] as { id: string; latest: { id: string } | null; targetID: string }[]),
  selectOutput: vi.fn(),
  state: ref('failed'),
})

let model: ReturnType<typeof pageModel>

vi.mock('@/features/view-profile/model/useProfileView', () => ({
  useProfileView: () => model,
}))

const renderPage = () =>
  mount(SendPage, {
    global: {
      stubs: {
        AppShell: { template: '<main><slot /></main>' },
        NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
        SendPanel: {
          props: ['artifactId'],
          template:
            '<section><h1>Send to device</h1><p>{{ artifactId }}</p></section>',
        },
      },
    },
  })

beforeEach(() => {
  model = pageModel()
  useLocale().setLocale('en')
  vi.stubGlobal('useRoute', () => ({
    params: { id: 'profile-1', output: 'chosen' },
  }))
  vi.stubGlobal('useHead', vi.fn())
})

afterEach(() => vi.unstubAllGlobals())

describe('send entry read states', () => {
  it('keeps a failed read distinct from a missing profile and retries the requested connection', async () => {
    const wrapper = renderPage()
    await flushPromises()
    expect(wrapper.text()).toContain('Profile is unavailable')
    expect(wrapper.text()).not.toContain('Profile not found')
    expect(wrapper.findAll('h1')).toHaveLength(1)
    model.initialize.mockImplementation(async () => {
      model.state.value = 'ready'
    })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(model.initialize).toHaveBeenCalledTimes(2)
    expect(model.selectOutput).toHaveBeenLastCalledWith('chosen')
    expect(wrapper.text()).toContain('artifact-2')
    expect(wrapper.findAll('h1')).toHaveLength(1)
    wrapper.unmount()
  })

  it.each(['loading', 'missing'])(
    'keeps a heading and the correct %s state without offering delivery',
    async (state) => {
      model.state.value = state
      const wrapper = renderPage()
      await flushPromises()
      expect(wrapper.findAll('h1')).toHaveLength(1)
      expect(wrapper.text()).toContain(
        state === 'missing' ? 'Profile not found' : 'Loading the profile',
      )
      expect(wrapper.text()).not.toContain('artifact-2')
      expect(wrapper.find('button').exists()).toBe(false)
      wrapper.unmount()
    },
  )

  it('names a missing connection, not a missing profile, and returns to that profile', async () => {
    model.state.value = 'ready'
    model.outputs.value = []
    const wrapper = renderPage()
    await flushPromises()
    expect(wrapper.text()).toContain('Connection not found')
    expect(wrapper.text()).not.toContain('Profile not found')
    expect(wrapper.get('a').attributes('href')).toBe('/profiles/profile-1')
    expect(wrapper.findAll('h1')).toHaveLength(1)
    wrapper.unmount()
  })

  it('keeps a connection without a file distinct from a missing connection', async () => {
    model.state.value = 'ready'
    model.outputs.value[1]!.latest = null
    const wrapper = renderPage()
    await flushPromises()
    expect(wrapper.text()).not.toContain('not found')
    expect(wrapper.get('a').attributes('href')).toBe('/profiles/profile-1')
    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('artifact-2')
    wrapper.unmount()
  })
})
