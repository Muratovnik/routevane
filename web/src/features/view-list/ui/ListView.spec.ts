import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { usePublishedRoute } from '@/entities/route-build/model/publishedRoute'
import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

import ListView from './ListView.vue'

const timestamp = '2026-09-03T09:00:00Z'
let routeStatus = 200
let fileStatus = 200
let diagnosticsStatus = 200
let published = true
let failedRebuild = false
let holdRoute: Promise<void> | undefined

function json(payload: unknown, status = 200) {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function routePayload() {
  return {
    list: {
      id: 'route-1',
      name: 'Known route',
      services: [],
      categories: [],
      exclusions: [],
      created_at: timestamp,
      updated_at: timestamp,
    },
    outputs: [
      {
        id: 'output-1',
        target_id: 'keenetic',
        target_title: 'Keenetic',
        created_at: timestamp,
        latest: published
          ? {
              id: 'artifact-1',
              snapshot_id: 'snapshot-1',
              size_bytes: 14,
              content_type: 'text/plain',
              content_created_at: timestamp,
            }
          : null,
        last_attempt: failedRebuild
          ? { status: 'failed', code: 'source_failed', completed_at: timestamp }
          : null,
      },
    ],
    resolved: [],
    missing_categories: [],
    schedule: { interval: '', effective: 'off', follows_default: true },
  }
}

const fetchMock = vi.fn(async (input: unknown) => {
  switch (String(input)) {
    case '/v1/lists/route-1':
      await holdRoute
      return json(
        routeStatus === 200 ? routePayload() : { error: 'controlled refusal' },
        routeStatus,
      )
    case '/v1/services':
      return json({ services: [], service_details: [], categories: [] })
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

function renderRoute() {
  return mount(ListView, {
    props: { listId: 'route-1' },
    global: {
      stubs: {
        ListEditor: true,
        OutputsPanel: true,
        RvMenu: true,
        RvInfoTip: true,
        RvSelect: true,
        NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
      },
    },
  })
}

beforeEach(() => {
  routeStatus = fileStatus = diagnosticsStatus = 200
  published = true
  failedRebuild = false
  holdRoute = undefined
  fetchMock.mockClear()
  invalidateCatalogCache()
  usePublishedRoute().clear()
  useLocale().setLocale('en')
  vi.stubGlobal('fetch', fetchMock)
  vi.stubGlobal('useRoute', () => ({ hash: '' }))
  vi.stubGlobal('useRouter', () => ({
    replace: vi.fn(async () => {}),
    push: vi.fn(async () => {}),
  }))
})

afterEach(() => vi.unstubAllGlobals())

describe('route screen read boundaries', () => {
  it('shows loading, then a failed read with retry, then the recovered route', async () => {
    let release!: () => void
    holdRoute = new Promise<void>((resolve) => {
      release = resolve
    })
    routeStatus = 503
    const wrapper = renderRoute()
    expect(wrapper.text()).toContain('Loading the route')
    release()
    await flushPromises()
    expect(wrapper.text()).toContain('Routes are unavailable')
    expect(wrapper.text()).not.toContain('Route not found')
    routeStatus = 200
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe('Known route')
    expect(wrapper.text()).not.toContain('Routes are unavailable')
    wrapper.unmount()
  })

  it('renders a true 404 without advertising a retry or editable route', async () => {
    routeStatus = 404
    const wrapper = renderRoute()
    await flushPromises()
    expect(wrapper.text()).toContain('Route not found')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.find('list-editor-stub').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps file and diagnostics failures distinct from an unpublished route', async () => {
    fileStatus = diagnosticsStatus = 503
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
    expect(wrapper.get('h1').text()).toBe('Known route')
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
        'This route has no published file.',
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
