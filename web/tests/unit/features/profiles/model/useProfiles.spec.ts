import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useProfiles } from '@/features/profiles/model/useProfiles'
import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

const cursor = 'b'.repeat(32)
const firstID = 'a'.repeat(32)
const secondID = 'c'.repeat(32)

const json = (payload: unknown): Response =>
  new Response(JSON.stringify(payload), {
    headers: { 'Content-Type': 'application/json' },
  })

const profile = (
  id: string,
  name: string,
  archivedAt = '',
): Record<string, unknown> => ({
  id,
  name,
  lists: ['example'],
  categories: [],
  exclusions: [],
  resolved: ['example'],
  missing_categories: [],
  archived_at: archivedAt,
  created_at: '2026-09-12T12:00:00Z',
  updated_at: '2026-09-12T12:00:00Z',
  outputs: [],
})

describe('useProfiles pagination', () => {
  beforeEach(() => {
    invalidateCatalogCache()
    useLocale().setLocale('en')
  })

  afterEach(() => vi.unstubAllGlobals())

  it('drops a delayed continuation after archiving a loaded profile', async () => {
    const continued = Promise.withResolvers<Response>()
    vi.stubGlobal(
      'fetch',
      vi.fn((input: string, init?: RequestInit) => {
        if (input === '/v1/profiles')
          return Promise.resolve(
            json({ profiles: [profile(firstID, 'Newest')], next: cursor }),
          )
        if (input === '/v1/lists')
          return Promise.resolve(
            json({ lists: [], list_details: [], categories: [] }),
          )
        if (input === '/v1/deployments/targets')
          return Promise.resolve(json({ targets: [] }))
        if (input === '/v1/export-formats')
          return Promise.resolve(json({ formats: [] }))
        if (input === `/v1/profile-pages/${cursor}`) return continued.promise
        if (
          input === `/v1/profiles/${firstID}/archive` &&
          init?.method === 'POST'
        )
          return Promise.resolve(
            json({
              profile: profile(firstID, 'Newest', '2026-09-12T13:00:00Z'),
            }),
          )
        return Promise.reject(new Error(`unexpected request ${input}`))
      }),
    )

    const library = useProfiles()
    await library.initialize()
    const continuation = library.loadMore()
    await library.setArchived(library.rows.value[0]!, true)
    continued.resolve(
      json({ profiles: [profile(secondID, 'Older')], next: '' }),
    )
    await expect(continuation).resolves.toBe(false)

    expect(library.rows.value).toEqual([])
    expect(library.archived.value.map((card) => card.id)).toEqual([firstID])
    expect(library.nextCursor.value).toBe(cursor)
  })
})
