/**
 * What each stored record carries, field by field: a profile, the catalog and
 * its categories, a list and its contents, a device, a source preview — and
 * the one refresh rule all three of a list, a schedule and the settings state.
 */
import { beforeEach, describe, expect, it } from 'vitest'

import {
  categoryInUse,
  createCategory,
  loadCatalog,
  loadListContents,
  previewList,
  removeCategory,
  removeList,
  saveDefaultPriority,
  listInUse,
  updateCategory,
} from '@/shared/api/catalog'
import { loadDevices } from '@/shared/api/devices'
import { RoutevaneAPIError } from '@/shared/api/http'
import {
  loadProfiles,
  refreshInterval,
  saveProfileRefreshInterval,
} from '@/shared/api/profiles'
import { loadSettings } from '@/shared/api/settings'

import {
  answer,
  CUSTOM_CATEGORY_ID,
  fetchMock,
  outputID,
  profileID,
  profilePayload,
  stubFetch,
} from './support/fixtures'

beforeEach(stubFetch)

describe('a stored refresh rule', () => {
  // An unrecognised rule reads as off; a rule the reply never stated reads as
  // the empty rule, which is the list deferring to the service-wide default.
  it('separates an unstated rule from an unrecognised one', () => {
    expect(refreshInterval('daily')).toBe('daily')
    expect(refreshInterval('weekly')).toBe('weekly')
    expect(refreshInterval('off')).toBe('off')
    expect(refreshInterval('')).toBe('')
    expect(refreshInterval(undefined)).toBe('')
    expect(refreshInterval(null)).toBe('')
    expect(refreshInterval(42)).toBe('')
    expect(refreshInterval('hourly')).toBe('off')
  })

  it('reads the same rule out of a list, a schedule and the settings', async () => {
    answer(profilePayload())
    await expect(loadProfiles()).resolves.toMatchObject([
      { refreshInterval: '' },
    ])

    answer(profilePayload({ refresh_interval: 'hourly' }))
    await expect(loadProfiles()).resolves.toMatchObject([
      { refreshInterval: 'off' },
    ])

    answer({
      schedule: {
        interval: 'daily',
        effective: 'daily',
        follows_default: false,
        next_refresh_at: '2026-08-21T12:00:00Z',
      },
    })
    await expect(
      saveProfileRefreshInterval(profileID, 'daily'),
    ).resolves.toEqual({
      interval: 'daily',
      effective: 'daily',
      followsDefault: false,
      lastRefreshedAt: '',
      lastRefreshFailed: false,
      nextRefreshAt: '2026-08-21T12:00:00Z',
    })

    answer({ refresh_interval: 'weekly' })
    await expect(loadSettings()).resolves.toEqual({ refreshInterval: 'weekly' })
  })
})

describe('the server states its fields, the screen reads its own', () => {
  it('renames every field a list card carries', async () => {
    answer(
      profilePayload({
        priority: ['discord'],
        list_domains: { discord: ['discord.com'] },
        last_refreshed_at: '2026-08-20T11:00:00Z',
        last_refresh_failed: true,
        archived_at: '2026-08-20T12:30:00Z',
        missing_categories: ['gone'],
      }),
    )
    await expect(loadProfiles()).resolves.toEqual([
      {
        id: profileID,
        name: 'Video',
        lists: ['discord'],
        categories: [],
        exclusions: [],
        priority: ['discord'],
        listDomains: { discord: ['discord.com'] },
        refreshInterval: '',
        lastRefreshedAt: '2026-08-20T11:00:00Z',
        lastRefreshFailed: true,
        archivedAt: '2026-08-20T12:30:00Z',
        createdAt: '2026-08-20T12:00:00Z',
        updatedAt: '2026-08-20T12:00:00Z',
        resolved: ['discord'],
        missingCategories: ['gone'],
        outputs: [],
      },
    ])
  })

  it('reads an absent domain map as an empty one and refuses a wrong one', async () => {
    answer(profilePayload({ list_domains: null }))
    await expect(loadProfiles()).resolves.toMatchObject([{ listDomains: {} }])

    for (const value of [[], { discord: 'discord.com' }, { discord: [''] }]) {
      answer(profilePayload({ list_domains: value }))
      await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)
    }
  })

  it('renames every field the catalog carries', async () => {
    answer({
      lists: ['discord'],
      list_details: [
        {
          id: 'discord',
          title: 'Discord',
          categories: ['communication'],
          domains: [{ value: 'discord.com', include_subdomains: false }],
          sources: [{ id: 'itdoginfo', type: 'http' }],
          source_count: 5,
          custom: true,
        },
      ],
      categories: [
        {
          id: 'communication',
          title: 'Communication',
          lists: ['discord'],
          custom: false,
        },
      ],
    })
    answer({
      targets: [
        {
          id: 'mikrotik',
          title: 'MikroTik',
          title_en: 'MikroTik router',
          kind: 'router',
          format_key: 'mikrotik-address-list-v1',
          renderer_id: 'mikrotik-address-list-rsc',
          file_extension: 'rsc',
          manual_installation_hint: 'выполните /import',
          manual_installation_hint_en: 'run /import',
        },
      ],
    })
    await expect(loadCatalog()).resolves.toEqual({
      lists: ['discord'],
      listDetails: [
        {
          id: 'discord',
          title: 'Discord',
          categories: ['communication'],
          domains: [{ value: 'discord.com', includeSubdomains: false }],
          sources: [{ id: 'itdoginfo', type: 'http' }],
          sourceCount: 5,
          custom: true,
        },
      ],
      categories: [
        {
          id: 'communication',
          title: 'Communication',
          lists: ['discord'],
          custom: false,
        },
      ],
      targets: [
        {
          id: 'mikrotik',
          title: 'MikroTik',
          titleEn: 'MikroTik router',
          kind: 'router',
          formatKey: 'mikrotik-address-list-v1',
          rendererID: 'mikrotik-address-list-rsc',
          fileExtension: 'rsc',
          manualInstallationHint: 'выполните /import',
          manualInstallationHintEn: 'run /import',
        },
      ],
    })
  })

  it('reads and saves the complete library default priority', async () => {
    answer({
      lists: ['discord', 'youtube'],
      list_details: [],
      categories: [],
      default_priority: ['youtube', 'discord'],
    })
    answer({ targets: [] })
    await expect(loadCatalog()).resolves.toMatchObject({
      defaultPriority: ['youtube', 'discord'],
    })

    answer({ default_priority: ['discord', 'youtube'] })
    await expect(saveDefaultPriority(['discord', 'youtube'])).resolves.toEqual([
      'discord',
      'youtube',
    ])
    expect(fetchMock.mock.calls[2]?.[0]).toBe('/v1/lists/priority')
    expect((fetchMock.mock.calls[2]?.[1] as RequestInit).body).toBe(
      JSON.stringify({ default_priority: ['discord', 'youtube'] }),
    )
  })

  // Ownership is not optional. A category with no `custom` leaves the surface
  // guessing which controls it may offer, so the reply is refused instead.
  it('refuses a category that does not state who owns it', async () => {
    const detail = { id: 'discord', title: 'Discord', categories: [] }
    for (const category of [
      { id: 'communication', title: 'Communication', lists: ['discord'] },
      {
        id: 'communication',
        title: 'Communication',
        lists: ['discord'],
        custom: 'yes',
      },
      { id: '', title: 'Communication', lists: [], custom: false },
      { id: 'communication', title: '', lists: [], custom: false },
      {
        id: 'communication',
        title: 'Communication',
        lists: [''],
        custom: false,
      },
    ]) {
      answer({
        lists: ['discord'],
        list_details: [detail],
        categories: [category],
      })
      answer({ targets: [] })
      await expect(loadCatalog()).rejects.toBeInstanceOf(RoutevaneAPIError)
    }
  })

  it('reads a written category back and refuses an envelope without one', async () => {
    const written = {
      id: CUSTOM_CATEGORY_ID,
      title: 'Мои списки',
      lists: ['discord'],
      custom: true,
    }
    answer({ category: written }, 201)
    await expect(createCategory('Мои списки', ['discord'])).resolves.toEqual({
      id: CUSTOM_CATEGORY_ID,
      title: 'Мои списки',
      lists: ['discord'],
      custom: true,
    })
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/v1/categories')
    expect((fetchMock.mock.calls[0]?.[1] as RequestInit).body).toBe(
      JSON.stringify({ lists: ['discord'], title: 'Мои списки' }),
    )

    // Creating with no membership states no membership rather than an empty
    // one, because the two are different requests everywhere else.
    answer({ category: { ...written, lists: [] } }, 201)
    await createCategory('Мои списки')
    expect((fetchMock.mock.calls[1]?.[1] as RequestInit).body).toBe(
      JSON.stringify({ title: 'Мои списки' }),
    )

    for (const payload of [
      {},
      { category: null },
      { category: [written] },
      { category: { ...written, custom: undefined } },
      { error: 'operation failed' },
    ]) {
      answer(payload, 201)
      await expect(createCategory('Мои списки')).rejects.toBeInstanceOf(
        RoutevaneAPIError,
      )
    }
  })

  // `lists` is the whole membership the operator wants; a field left out
  // means "leave this alone", which the body has to carry as an absence.
  it('sends only the fields a category edit states', async () => {
    const written = {
      id: CUSTOM_CATEGORY_ID,
      title: 'Мои списки',
      lists: ['discord'],
      custom: true,
    }
    answer({ category: written })
    await updateCategory(CUSTOM_CATEGORY_ID, { lists: ['discord'] })
    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      `/v1/categories/${CUSTOM_CATEGORY_ID}/update`,
    )
    expect((fetchMock.mock.calls[0]?.[1] as RequestInit).body).toBe(
      JSON.stringify({ lists: ['discord'] }),
    )

    answer({ category: written })
    await updateCategory(CUSTOM_CATEGORY_ID, { title: 'Мои списки' })
    expect((fetchMock.mock.calls[1]?.[1] as RequestInit).body).toBe(
      JSON.stringify({ title: 'Мои списки' }),
    )

    // An empty membership is a request, not an absent field.
    answer({ category: { ...written, lists: [] } })
    await updateCategory(CUSTOM_CATEGORY_ID, { lists: [] })
    expect((fetchMock.mock.calls[2]?.[1] as RequestInit).body).toBe(
      JSON.stringify({ lists: [] }),
    )

    answer({ error: 'not found' }, 404)
    await expect(updateCategory(CUSTOM_CATEGORY_ID, {})).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })

  /**
   * Removal answers with nothing but its status, so an empty body is the
   * success and reading it as JSON would turn a completed removal into a
   * contract failure. What becomes of the lists the category held is the one
   * question the request has to state (ADR 0029). The one refusal that carries
   * objects names the profiles holding the category; every other refusal names
   * none.
   */
  it('completes a removal with no body and names the profiles that refuse one', async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }))
    await expect(
      removeCategory(CUSTOM_CATEGORY_ID, 'detach'),
    ).resolves.toBeUndefined()
    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      `/v1/categories/${CUSTOM_CATEGORY_ID}/remove`,
    )
    expect((fetchMock.mock.calls[0]?.[1] as RequestInit).headers).toMatchObject(
      {
        'X-Routevane-Request': '1',
      },
    )
    expect((fetchMock.mock.calls[0]?.[1] as RequestInit).body).toBe(
      JSON.stringify({ lists: 'detach' }),
    )

    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }))
    await removeCategory(CUSTOM_CATEGORY_ID, 'delete')
    expect((fetchMock.mock.calls[1]?.[1] as RequestInit).body).toBe(
      JSON.stringify({ lists: 'delete' }),
    )

    answer(
      {
        error: 'category in use',
        profiles: [
          { id: profileID, title: 'Дом' },
          { id: outputID, title: 'Офис' },
        ],
      },
      409,
    )
    const refused = await removeCategory(CUSTOM_CATEGORY_ID, 'detach').catch(
      (reason: unknown) => reason,
    )
    expect(refused).toBeInstanceOf(RoutevaneAPIError)
    expect(categoryInUse(refused)).toEqual([
      { id: profileID, title: 'Дом' },
      { id: outputID, title: 'Офис' },
    ])

    // Anything that is not that refusal names nobody rather than guessing.
    for (const [payload, status] of [
      [{ error: 'not found' }, 404],
      [{ error: 'operation failed' }, 422],
      [{ error: 'category in use' }, 409],
      [{ error: 'category in use', profiles: [{ id: profileID }] }, 409],
      [{ error: 'in use', profiles: [] }, 409],
    ] as const) {
      answer(payload, status)
      const other = await removeCategory(CUSTOM_CATEGORY_ID, 'detach').catch(
        (reason: unknown) => reason,
      )
      expect(other).toBeInstanceOf(RoutevaneAPIError)
      expect(categoryInUse(other)).toBeNull()
    }
    expect(categoryInUse(new Error('offline'))).toBeNull()
  })

  // Removing a list is the same shape of act and the same shape of refusal, and
  // it answers with its own word for it: the API still says `list` where the
  // surface says list, and `lists` for the profiles standing in the way.
  it('removes a list and names the profiles that refuse it', async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }))
    await expect(removeList('discord')).resolves.toBeUndefined()
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/v1/lists/discord/remove')
    expect((fetchMock.mock.calls[0]?.[1] as RequestInit).headers).toMatchObject(
      {
        'Content-Type': 'application/json',
        'X-Routevane-Request': '1',
      },
    )

    answer(
      { error: 'list in use', profiles: [{ id: profileID, title: 'Дом' }] },
      409,
    )
    const refused = await removeList('discord').catch(
      (reason: unknown) => reason,
    )
    expect(refused).toBeInstanceOf(RoutevaneAPIError)
    expect(listInUse(refused)).toEqual([{ id: profileID, title: 'Дом' }])
    // The two refusals are not interchangeable: each reads only its own.
    expect(categoryInUse(refused)).toBeNull()

    for (const [payload, status] of [
      [{ error: 'not found' }, 404],
      [{ error: 'list in use' }, 409],
      [
        {
          error: 'category in use',
          profiles: [{ id: profileID, title: 'Дом' }],
        },
        409,
      ],
    ] as const) {
      answer(payload, status)
      const other = await removeList('discord').catch(
        (reason: unknown) => reason,
      )
      expect(other).toBeInstanceOf(RoutevaneAPIError)
      expect(listInUse(other)).toBeNull()
    }
  })

  // A count the server states wins over one this tab could derive: the server
  // may know of sources this reply does not carry.
  it('counts sources itself only when the reply states no count', async () => {
    const detail = {
      id: 'discord',
      title: 'Discord',
      categories: [],
      sources: [
        { id: 'itdoginfo', type: 'http' },
        { id: 'v2fly', type: 'http' },
      ],
    }
    answer({ lists: ['discord'], list_details: [detail], categories: [] })
    answer({ targets: [] })
    await expect(loadCatalog()).resolves.toMatchObject({
      listDetails: [{ sourceCount: 2, domains: [] }],
    })

    answer({
      lists: ['discord'],
      list_details: [{ ...detail, source_count: 'many' }],
      categories: [],
    })
    answer({ targets: [] })
    await expect(loadCatalog()).rejects.toBeInstanceOf(RoutevaneAPIError)
  })

  it('renames every field a device carries', async () => {
    answer({
      devices: [
        {
          id: 'device-1',
          target_id: 'keenetic',
          target_title: 'Keenetic',
          name: 'Home router',
          address: '192.168.1.1',
          account: 'admin',
          interface: 'Wireguard0',
          auto_deliver: true,
          deployable: true,
        },
      ],
      secret_store_available: true,
    })
    await expect(loadDevices()).resolves.toEqual({
      devices: [
        {
          id: 'device-1',
          targetID: 'keenetic',
          targetTitle: 'Keenetic',
          name: 'Home router',
          address: '192.168.1.1',
          account: 'admin',
          interfaceName: 'Wireguard0',
          autoDeliver: true,
          deployable: true,
        },
      ],
      secretStoreAvailable: true,
    })
  })

  it('renames every field a list preview carries', async () => {
    answer({
      list_id: 'google-ai',
      sources: [
        {
          id: 'iplist',
          type: 'http',
          status: 'failed',
          error_code: 'unreachable',
          domains: [],
          domain_count: 0,
          address_count: 0,
          prefix_count: 0,
          skipped_count: 0,
        },
      ],
      domains: ['ai.google.dev'],
      domain_count: 1,
      address_count: 0,
      prefix_count: 32,
      skipped_count: 1,
    })
    await expect(previewList('google-ai')).resolves.toEqual({
      listID: 'google-ai',
      sources: [
        {
          id: 'iplist',
          type: 'http',
          status: 'failed',
          errorCode: 'unreachable',
          domains: [],
          domainCount: 0,
          addressCount: 0,
          prefixCount: 0,
          skippedCount: 0,
        },
      ],
      domains: ['ai.google.dev'],
      domainCount: 1,
      addressCount: 0,
      prefixCount: 32,
      skippedCount: 1,
    })
  })

  // A source that succeeded states no code at all. A blank one is a field the
  // server should not have sent, and nothing prints a blank reason.
  it('refuses a blank failure code and omits an absent one', async () => {
    const source = {
      id: 'iplist',
      type: 'http',
      status: 'ready',
      domains: [],
      domain_count: 0,
      address_count: 0,
      prefix_count: 0,
      skipped_count: 0,
    }
    const preview = {
      list_id: 'google-ai',
      domains: [],
      domain_count: 0,
      address_count: 0,
      prefix_count: 0,
      skipped_count: 0,
    }

    answer({ ...preview, sources: [source] })
    const decoded = await previewList('google-ai')
    expect(decoded.sources[0] && 'errorCode' in decoded.sources[0]).toBe(false)

    answer({ ...preview, sources: [{ ...source, error_code: '' }] })
    await expect(previewList('google-ai')).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })

  it('renames every field the list contents table carries', async () => {
    answer({
      list_id: 'discord',
      rows: [
        {
          value: 'discord.com',
          kind: 'domain',
          origin: 'itdoginfo',
          enabled: true,
          missing: false,
        },
        { value: '1.1.1.0/24', kind: 'prefix' },
      ],
      sources: [{ id: 'itdoginfo', type: 'http', custom: true, enabled: true }],
      observed: true,
    })
    await expect(loadListContents('discord')).resolves.toEqual({
      listID: 'discord',
      rows: [
        {
          value: 'discord.com',
          kind: 'domain',
          origin: 'itdoginfo',
          enabled: true,
          missing: false,
        },
        {
          value: '1.1.1.0/24',
          kind: 'prefix',
          origin: '',
          enabled: false,
          missing: false,
        },
      ],
      sources: [
        {
          id: 'itdoginfo',
          type: 'http',
          custom: true,
          enabled: true,
          url: '',
        },
      ],
      observed: true,
    })

    answer({
      service_id: 'discord',
      rows: [{ value: 'discord.com', kind: 'hostname' }],
      sources: [],
    })
    await expect(loadListContents('discord')).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })
})
