import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick } from 'vue'

import { forgetForecastObservations } from '@/entities/profile-composition/model/forecast'
import { useCreateProfile } from '@/features/create-profile/model/useCreateProfile'
import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

const PROFILE_ID = 'a'.repeat(32)

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const stubService = () => {
  const fetchMock = vi.fn((input: unknown, init?: RequestInit) => {
    const path = String(input)
    if (path === '/v1/lists')
      return Promise.resolve(
        json({
          lists: ['discord'],
          list_details: [{ id: 'discord', title: 'Discord', categories: [] }],
          categories: [],
        }),
      )
    if (path === '/v1/targets')
      return Promise.resolve(
        json({
          targets: [
            {
              id: 'keenetic',
              title: 'Keenetic',
              kind: 'router',
              format_key: 'keenetic-v1',
              renderer_id: 'keenetic-route-bat',
              file_extension: 'bat',
              manual_installation_hint: 'Install it by hand.',
            },
          ],
        }),
      )
    if (path === '/v1/deployments/targets')
      return Promise.resolve(json({ targets: [] }))
    // An unanswered forecast leaves creating available.
    if (path === '/v1/profiles/preview')
      return Promise.resolve(json({ error: 'nothing observed' }, 404))
    if (path === '/v1/profiles' && init?.method === 'POST')
      return Promise.resolve(
        json({
          profile: {
            id: PROFILE_ID,
            name: 'Discord',
            lists: ['discord'],
            categories: [],
            exclusions: [],
            created_at: '2026-09-12T12:00:00Z',
            updated_at: '2026-09-12T12:00:00Z',
          },
        }),
      )
    return Promise.reject(new Error(`unexpected request ${path}`))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const creations = (fetchMock: ReturnType<typeof stubService>): number =>
  fetchMock.mock.calls.filter(
    ([input, init]) =>
      String(input) === '/v1/profiles' &&
      (init as RequestInit | undefined)?.method === 'POST',
  ).length

describe('useCreateProfile', () => {
  beforeEach(() => {
    invalidateCatalogCache()
    forgetForecastObservations()
    useLocale().setLocale('en')
  })

  afterEach(() => vi.unstubAllGlobals())

  // Creating hands the new profile to the caller, which navigates to it. Until
  // the screen is gone, a second activation must not store a second profile.
  it('stays busy after a profile is created so a repeated create stores nothing', async () => {
    const fetchMock = stubService()
    const scope = effectScope()
    const setup = scope.run(() => useCreateProfile())!
    await setup.initialize()
    setup.composition.value = {
      lists: ['discord'],
      categories: [],
      exclusions: [],
      listDomains: {},
      priority: ['discord'],
    }
    setup.setTarget('keenetic')
    await nextTick()
    expect(setup.canCreate.value).toBe(true)

    await expect(setup.create()).resolves.toEqual({
      profileID: PROFILE_ID,
      targetID: 'keenetic',
    })
    expect(setup.busy.value).toBe(true)
    expect(setup.canCreate.value).toBe(false)

    await expect(setup.create()).resolves.toBeNull()
    expect(creations(fetchMock)).toBe(1)
    scope.stop()
  })
})
