import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { usePublishedProfile } from '@/entities/profile-build/model/publishedProfile'
import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

import ProfileView from '@/features/view-profile/ui/ProfileView.vue'

const TIMESTAMP = '2026-09-03T09:00:00Z'
let routeStatus = 200
let fileStatus = 200
let diagnosticsStatus = 200
let published = true
let failedRebuild = false
let holdRoute: Promise<void> | undefined

const json = (payload: unknown, status = 200) =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const routePayload = () => ({
  profile: {
    id: 'profile-1',
    name: 'Known profile',
    lists: [],
    categories: [],
    exclusions: [],
    created_at: TIMESTAMP,
    updated_at: TIMESTAMP,
  },
  outputs: [
    {
      id: 'output-1',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      created_at: TIMESTAMP,
      latest: published
        ? {
            id: 'artifact-1',
            snapshot_id: 'snapshot-1',
            size_bytes: 14,
            content_type: 'text/plain',
            content_created_at: TIMESTAMP,
          }
        : null,
      last_attempt: failedRebuild
        ? { status: 'failed', code: 'source_failed', completed_at: TIMESTAMP }
        : null,
    },
  ],
  resolved: [],
  missing_categories: [],
  schedule: { interval: '', effective: 'off', follows_default: true },
})

const fetchMock = vi.fn(async (input: unknown) => {
  switch (String(input)) {
    case '/v1/profiles/profile-1':
      await holdRoute
      return json(
        routeStatus === 200 ? routePayload() : { error: 'controlled refusal' },
        routeStatus,
      )
    case '/v1/lists':
      return json({ lists: [], list_details: [], categories: [] })
    case '/v1/targets':
    case '/v1/deployments/targets':
      return json({ targets: [] })
    case '/v1/export-formats':
      return json({ formats: [] })
    case '/v1/devices':
      return json({ devices: [], secret_store_available: false })
    case '/v1/artifacts/artifact-1':
      return new Response('verified bytes', { status: fileStatus })
    case '/v1/snapshots/snapshot-1':
      return json(
        { routing_plan: { rules: [], excluded: [] } },
        diagnosticsStatus,
      )
    default:
      throw new Error(`Unexpected request ${String(input)}`)
  }
})

const renderRoute = () =>
  mount(ProfileView, {
    props: { profileId: 'profile-1' },
    global: {
      stubs: {
        ProfileEditor: true,
        OutputsPanel: true,
        RvMenu: true,
        RvInfoTip: true,
        RvSelect: true,
        NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
      },
    },
  })

beforeEach(() => {
  routeStatus = 200
  fileStatus = 200
  diagnosticsStatus = 200
  published = true
  failedRebuild = false
  holdRoute = undefined
  fetchMock.mockClear()
  invalidateCatalogCache()
  usePublishedProfile().clear()
  useLocale().setLocale('en')
  vi.stubGlobal('fetch', fetchMock)
  vi.stubGlobal('useRoute', () => ({ hash: '' }))
  vi.stubGlobal('useRouter', () => ({
    replace: vi.fn(async () => {}),
    push: vi.fn(async () => {}),
  }))
})

afterEach(() => vi.unstubAllGlobals())

describe('profile screen read boundaries', () => {
  it('shows loading, then a failed read with retry, then the recovered profile', async () => {
    let release!: () => void
    holdRoute = new Promise<void>((resolve) => {
      release = resolve
    })
    routeStatus = 503
    const wrapper = renderRoute()
    expect(wrapper.text()).toContain('Loading the profile')
    expect(wrapper.get('.profile__loading').attributes('aria-busy')).toBe(
      'true',
    )
    expect(wrapper.find('.rv-notice').exists()).toBe(false)
    release()
    await flushPromises()
    expect(wrapper.text()).toContain('Profiles are unavailable')
    expect(wrapper.text()).not.toContain('Profile not found')
    routeStatus = 200
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe('Known profile')
    expect(wrapper.text()).not.toContain('Profiles are unavailable')
    wrapper.unmount()
  })

  it('renders a true 404 without advertising a retry or editable profile', async () => {
    routeStatus = 404
    const wrapper = renderRoute()
    await flushPromises()
    expect(wrapper.text()).toContain('Profile not found')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.find('profile-editor-stub').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps file and diagnostics failures distinct from an unpublished profile', async () => {
    fileStatus = 503
    diagnosticsStatus = 503
    const wrapper = renderRoute()
    await flushPromises()
    await wrapper.get('#rv-tab-file').trigger('click')
    await flushPromises()
    expect(wrapper.get('#rv-panel-file').text()).toContain(
      'The file was not read',
    )
    expect(wrapper.get('#rv-panel-file').text()).not.toContain(
      'no published file',
    )
    await wrapper.get('#rv-tab-diagnostics').trigger('click')
    await flushPromises()
    expect(wrapper.get('#rv-panel-diagnostics').text()).toContain(
      'Diagnostics unavailable',
    )
    expect(wrapper.get('h1').text()).toBe('Known profile')
    wrapper.unmount()
  })

  it('states that an unpublished connection has no file or diagnostics without fetching them', async () => {
    published = false
    const wrapper = renderRoute()
    await flushPromises()
    for (const tab of ['file', 'diagnostics']) {
      await wrapper.get(`#rv-tab-${tab}`).trigger('click')
      await flushPromises()
      expect(wrapper.get(`#rv-panel-${tab}`).text()).toContain(
        'This profile has no published file.',
      )
    }
    expect(
      fetchMock.mock.calls.some(
        ([url]) =>
          String(url).startsWith('/v1/artifacts/') ||
          String(url).startsWith('/v1/snapshots/'),
      ),
    ).toBe(false)
    wrapper.unmount()
  })

  it('labels a retained verified file as stale after a failed rebuild and can still read it', async () => {
    failedRebuild = true
    const wrapper = renderRoute()
    await flushPromises()
    expect(wrapper.text()).toContain('Showing the previous verified file')
    await wrapper.get('#rv-tab-file').trigger('click')
    await flushPromises()
    expect(wrapper.get('#rv-panel-file').text()).toContain('verified bytes')
    await wrapper.get('#rv-tab-diagnostics').trigger('click')
    await flushPromises()
    expect(wrapper.get('#rv-panel-diagnostics').text()).toContain(
      'No exclusions: every rule entered the file.',
    )
    wrapper.unmount()
  })
})
