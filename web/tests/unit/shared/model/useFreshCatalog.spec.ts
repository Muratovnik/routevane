import { afterEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'

import { invalidateCatalogCache, loadCatalogCached } from '@/shared/api/catalog'
import { useFreshCatalog } from '@/shared/model/useFreshCatalog'

const json = (payload: unknown) =>
  new Response(JSON.stringify(payload), { status: 200 })
const catalog = (title: string) => ({
  lists: ['list'],
  list_details: [{ id: 'list', title, categories: [] }],
  categories: [],
})

afterEach(() => {
  vi.unstubAllGlobals()
  invalidateCatalogCache()
})

describe('catalog freshness lifecycle', () => {
  it('owes one refresh after busy focus and exposes read failure with a read-only retry', async () => {
    const busy = ref(true)
    const apply = vi.fn()
    const fetchMock = vi.fn(async (url: unknown) => {
      if (String(url) === '/v1/targets') return json({ targets: [] })
      if (
        fetchMock.mock.calls.filter(([input]) => input === '/v1/lists')
          .length === 1
      )
        throw new Error('offline')
      return json(catalog('Fresh list'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const scope = effectScope()
    const refresh = scope.run(() => useFreshCatalog(apply, () => busy.value))!
    window.dispatchEvent(new Event('focus'))
    window.dispatchEvent(new Event('focus'))
    await nextTick()
    expect(fetchMock).not.toHaveBeenCalled()
    busy.value = false
    await vi.waitFor(() => expect(refresh.state.value).toBe('stale'))
    expect(apply).not.toHaveBeenCalled()
    await refresh.retry()
    expect(refresh.state.value).toBe('idle')
    expect(apply.mock.calls[0]?.[0].listDetails[0].title).toBe('Fresh list')
    expect(
      fetchMock.mock.calls.filter(([input]) => input === '/v1/lists'),
    ).toHaveLength(2)
    scope.stop()
  })

  it('discards an old scope response, refreshes the new scope, and ignores unmounted responses', async () => {
    const held = Promise.withResolvers<Response>()
    const final = Promise.withResolvers<Response>()
    const identity = ref('profile-a')
    const apply = vi.fn()
    const fetchMock = vi.fn(async (url: unknown) => {
      if (String(url) === '/v1/targets') return json({ targets: [] })
      const reads = fetchMock.mock.calls.filter(
        ([input]) => input === '/v1/lists',
      ).length
      if (reads === 1) return held.promise
      if (reads === 2) return json(catalog('Current list'))
      return final.promise
    })
    vi.stubGlobal('fetch', fetchMock)
    const scope = effectScope()
    const refresh = scope.run(() =>
      useFreshCatalog(
        apply,
        () => false,
        () => identity.value,
      ),
    )!
    void refresh.retry()
    identity.value = 'profile-b'
    await nextTick()
    held.resolve(json(catalog('Old list')))
    await vi.waitFor(() => expect(apply).toHaveBeenCalledTimes(1))
    expect(apply.mock.calls[0]?.[0].listDetails[0].title).toBe('Current list')
    const pending = refresh.retry()
    scope.stop()
    final.resolve(json(catalog('Unmounted list')))
    await pending
    expect(apply).toHaveBeenCalledTimes(1)
  })

  it('does not let a read started before invalidation overwrite the new cached catalog', async () => {
    const old = Promise.withResolvers<Response>()
    const fetchMock = vi.fn(async (url: unknown) => {
      if (String(url) === '/v1/targets') return json({ targets: [] })
      return fetchMock.mock.calls.filter(([input]) => input === '/v1/lists')
        .length === 1
        ? old.promise
        : json(catalog('Current list'))
    })
    vi.stubGlobal('fetch', fetchMock)
    invalidateCatalogCache()
    const first = loadCatalogCached()
    invalidateCatalogCache()
    await loadCatalogCached()
    old.resolve(json(catalog('Old list')))
    await first
    expect((await loadCatalogCached()).listDetails[0]?.title).toBe(
      'Current list',
    )
  })
})
