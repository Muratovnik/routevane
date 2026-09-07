import { beforeEach, describe, expect, it, vi } from 'vitest'

import { loadDiagnostics } from './artifacts'
import {
  categoryInUse,
  createCategory,
  loadCatalog,
  loadListContents,
  previewList,
  refreshList,
  removeCategory,
  removeList,
  saveDefaultPriority,
  listInUse,
  updateCategory,
} from './catalog'
import {
  applyDeployment,
  loadDeployableTargets,
  planDeployment,
} from './deploy'
import { loadDevices } from './devices'
import { loadExportFormats, requestProfileExport } from './exports'
import { RoutevaneAPIError } from './http'
import {
  loadProfiles,
  previewComposition,
  refreshInterval,
  saveProfileRefreshInterval,
} from './profiles'
import { addOutput, buildOutput, loadOutput } from './outputs'
import { loadSettings } from './settings'

const profileID = 'a'.repeat(32)
const outputID = 'f'.repeat(32)
const artifactID = 'b'.repeat(32)
const snapshotID = 'c'.repeat(32)
const customCategoryID = 'custom-1234567890abcdef'
const subscription = `${window.location.origin}/v1/subscriptions/rv1.${'c'.repeat(32)}.${'d'.repeat(43)}`

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

function json(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function answer(payload: unknown, status = 200): void {
  fetchMock.mockResolvedValueOnce(json(payload, status))
}

const connection = {
  device: '192.168.1.1',
  username: 'admin',
  password: 'secret',
  interfaceName: 'wan',
}

function buildPayload(): Record<string, unknown> {
  return {
    output: { id: outputID, list_id: profileID, target_id: 'keenetic' },
    snapshot: { id: snapshotID },
    artifact: {
      id: artifactID,
      artifact_hash: 'd'.repeat(64),
      content_created_at: '2026-08-20T12:00:00Z',
      validation_status: 'valid',
      status: 'published',
      renderer_id: 'keenetic-route-bat',
      renderer_version: '1.0.0',
      content_type: 'application/x-bat',
    },
    summary: {
      rule_count: 2,
      partial_coverage: false,
      partial_coverage_count: 0,
      content_created_at: '2026-08-20T12:00:00Z',
      validation_status: 'valid',
      status: 'published',
    },
    subscription_url: subscription,
  }
}

function profilePayload(fields: Record<string, unknown> = {}): unknown {
  return {
    profiles: [
      {
        id: profileID,
        name: 'Video',
        lists: ['discord'],
        categories: [],
        exclusions: [],
        resolved: ['discord'],
        missing_categories: [],
        created_at: '2026-08-20T12:00:00Z',
        updated_at: '2026-08-20T12:00:00Z',
        outputs: [],
        ...fields,
      },
    ],
  }
}

function attemptPayload(attempt: Record<string, unknown>): unknown {
  return profilePayload({
    outputs: [
      {
        id: outputID,
        target_id: 'keenetic',
        target_title: 'Keenetic',
        created_at: '2026-08-20T12:00:00Z',
        last_attempt: attempt,
      },
    ],
  })
}

function forecastPayload(target: Record<string, unknown>): unknown {
  return {
    targets: [
      {
        target_id: 'keenetic',
        maximum_rules: 1024,
        projected_rules: 12,
        fits: true,
        per_list: [],
        ...target,
      },
    ],
  }
}

const draft = {
  lists: ['discord'],
  categories: [],
  exclusions: [],
  listDomains: {},
}

describe('forecast overlap contract', () => {
  const entry = {
    rule_kind: 'domain_suffix',
    value: 'shared.example',
    lists: ['alpha', 'beta'],
  }
  it('keeps typed ownership and the explicit detail limit', async () => {
    answer(
      forecastPayload({
        overlaps: { items: [{ kind: 'duplicate', entry }], truncated: true },
      }),
    )
    const result = await previewComposition(draft)
    expect(result[0]?.overlaps).toEqual({
      items: [
        {
          kind: 'duplicate',
          entry: {
            ruleKind: 'domain_suffix',
            value: 'shared.example',
            lists: ['alpha', 'beta'],
          },
        },
      ],
      truncated: true,
    })
  })
  it('decodes complete list adjacency separately from capped details', async () => {
    answer(
      forecastPayload({
        overlaps: {
          items: [],
          truncated: true,
          summary: [
            { list_id: 'alpha', overlaps: ['beta'] },
            { list_id: 'beta', overlaps: ['alpha'] },
            { list_id: 'solo', overlaps: [] },
          ],
        },
      }),
    )
    const result = await previewComposition(draft)
    expect(result[0]?.overlaps?.summary).toEqual([
      { listID: 'alpha', overlaps: ['beta'] },
      { listID: 'beta', overlaps: ['alpha'] },
      { listID: 'solo', overlaps: [] },
    ])
  })
  it('refuses missing truncation, self-coverage and malformed relations', async () => {
    for (const overlaps of [
      { items: [] },
      { items: [{ kind: 'similar', entry }], truncated: false },
      {
        items: [{ kind: 'duplicate', entry: { ...entry, lists: ['alpha'] } }],
        truncated: false,
      },
      {
        items: [
          {
            kind: 'covered',
            entry: { ...entry, lists: ['alpha'] },
            covering: entry,
          },
        ],
        truncated: false,
      },
      { items: [{ kind: 'covered', entry }], truncated: false },
      {
        items: Array.from({ length: 101 }, () => ({
          kind: 'duplicate',
          entry,
        })),
        truncated: true,
      },
      {
        items: [],
        truncated: false,
        summary: [{ list_id: 'alpha', overlaps: ['alpha'] }],
      },
      {
        items: [],
        truncated: false,
        summary: [{ list_id: 'alpha', overlaps: ['beta', 'beta'] }],
      },
      {
        items: [],
        truncated: false,
        summary: [{ list_id: 'alpha', overlaps: ['zeta', 'beta'] }],
      },
      {
        items: [],
        truncated: false,
        summary: [
          { list_id: 'alpha', overlaps: ['beta'] },
          { list_id: 'beta', overlaps: [] },
        ],
      },
      {
        items: [],
        truncated: false,
        summary: [
          { list_id: 'beta', overlaps: [] },
          { list_id: 'beta', overlaps: [] },
        ],
      },
    ]) {
      answer(forecastPayload({ overlaps }))
      await expect(previewComposition(draft)).rejects.toBeInstanceOf(
        RoutevaneAPIError,
      )
    }
  })
})

function requirementsPayload(requirements: Record<string, unknown>): unknown {
  return {
    targets: [
      {
        target_id: 'keenetic',
        title: 'Keenetic',
        deployer_id: 'keenetic-telnet',
        requirements: {
          address_label: 'Address',
          address_example: '192.168.1.1',
          needs_credential: true,
          needs_interface: false,
          ...requirements,
        },
      },
    ],
  }
}

function outcomePayload(result: Record<string, unknown>): unknown {
  return {
    result: {
      applied: true,
      rolled_back: false,
      device: { deployer_id: 'keenetic-telnet', vendor: 'Keenetic' },
      ...result,
    },
  }
}

describe('the shape of a value', () => {
  // A count is a whole, non-negative, exactly representable number. Everything
  // else is a claim the screen cannot print, whatever it looks like.
  it('admits only a safe non-negative integer as a count', async () => {
    answer(forecastPayload({ maximum_rules: 0 }))
    await expect(previewComposition(draft)).resolves.toMatchObject([
      { maximumRules: 0 },
    ])

    for (const value of [12.5, -1, 2 ** 53, '1024', null, Number.NaN]) {
      answer(forecastPayload({ maximum_rules: value }))
      await expect(previewComposition(draft)).rejects.toBeInstanceOf(
        RoutevaneAPIError,
      )
    }
  })

  // An empty string is how this API says "absent", so it is never a stated
  // value.
  it('refuses an empty string where a value is required', async () => {
    answer(profilePayload({ name: '' }))
    await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)

    answer(profilePayload({ name: 42 }))
    await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)
  })

  // A moment is admitted by being placeable on a clock and kept in the
  // server's own spelling, so what round-trips is what was published.
  it('keeps a timestamp as stated and refuses one no clock can place', async () => {
    answer(profilePayload({ created_at: '2026-08-20T12:00:00+03:00' }))
    await expect(loadProfiles()).resolves.toMatchObject([
      { createdAt: '2026-08-20T12:00:00+03:00' },
    ])

    answer(profilePayload({ created_at: 'whenever' }))
    await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)
  })

  // An array is never a record here, and a schema of optional fields must not
  // quietly accept one.
  it('refuses an array where the contract states an object', async () => {
    answer([])
    await expect(loadListContents('discord')).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )

    answer([])
    await expect(refreshList('discord')).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )

    answer({ refresh: {} })
    await expect(refreshList('discord')).resolves.toEqual({
      skippedEntries: 0,
    })
  })

  it('preserves skipped source entries and refuses malformed counts', async () => {
    answer({ refresh: { skipped_entries: 2 } })
    await expect(refreshList('kinopub')).resolves.toEqual({
      skippedEntries: 2,
    })
    for (const payload of [
      {},
      { refresh: [] },
      { refresh: { skipped_entries: -1 } },
      { refresh: { skipped_entries: 1.5 } },
      { refresh: { skipped_entries: '1' } },
    ]) {
      answer(payload)
      await expect(refreshList('kinopub')).rejects.toBeInstanceOf(
        RoutevaneAPIError,
      )
    }
  })
})

describe('the envelope every endpoint shares', () => {
  it('separates a broken reply, a refusal and a reply outside the contract', async () => {
    fetchMock.mockResolvedValueOnce(new Response('not json', { status: 200 }))
    await expect(loadProfiles()).rejects.toThrow(
      'Сервер вернул некорректный ответ.',
    )

    answer(
      {
        error: 'operation failed',
        code: 'rule_limit',
        projected_rules: 1740,
        maximum_rules: 1024,
      },
      422,
    )
    const refused = await addOutput(profileID, 'keenetic').catch(
      (reason: unknown) => reason,
    )
    expect(refused).toMatchObject({
      message: 'operation failed',
      code: 'rule_limit',
      details: { projected_rules: 1740, maximum_rules: 1024 },
      status: 422,
    })

    answer({ profiles: [{ id: profileID }] })
    await expect(loadProfiles()).rejects.toThrow(
      'Сервер вернул ответ вне контракта.',
    )
  })

  // A refusal that states one of its counts badly still delivers the rest.
  it('reads each part of a refusal on its own', async () => {
    answer({ error: 'too many', code: 'rule_limit', projected_rules: -1 }, 422)
    const refused = await addOutput(profileID, 'keenetic').catch(
      (reason: unknown) => reason,
    )
    expect(refused).toMatchObject({ code: 'rule_limit', details: {} })
  })
})

describe('a build reply describes one published fact', () => {
  it('accepts a summary that matches its artifact field for field', async () => {
    answer(buildPayload())
    await expect(buildOutput(outputID)).resolves.toMatchObject({
      snapshotID,
      artifactID,
      validationStatus: 'valid',
      status: 'published',
      contentCreatedAt: '2026-08-20T12:00:00Z',
      degradedSources: [],
      subscriptionURL: subscription,
    })
  })

  it('refuses a summary that disagrees with its artifact', async () => {
    for (const [field, value] of [
      ['validation_status', 'invalid'],
      ['status', 'draft'],
      ['content_created_at', '2026-08-20T13:00:00Z'],
    ] as const) {
      const payload = buildPayload()
      Object.assign(payload.summary as Record<string, unknown>, {
        [field]: value,
      })
      answer(payload)
      await expect(buildOutput(outputID)).rejects.toBeInstanceOf(
        RoutevaneAPIError,
      )
    }
  })

  // The routing plan is diagnostic material an operator asks for by name. A
  // build reply that smuggles one in is refused rather than read.
  it('refuses a snapshot that carries a routing plan', async () => {
    const payload = buildPayload()
    Object.assign(payload.snapshot as Record<string, unknown>, {
      routing_plan: { canary: 'must-never-render' },
    })
    answer(payload)
    await expect(buildOutput(outputID)).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })

  // A subscription address is a bearer: this origin's own plain-HTTP
  // subscription path, or nothing at all.
  it('admits only a same-origin subscription address', async () => {
    const accepted = ['', subscription]
    for (const value of accepted) {
      const payload = buildPayload()
      payload.subscription_url = value
      answer(payload)
      await expect(buildOutput(outputID)).resolves.toMatchObject({
        subscriptionURL: value,
      })
    }

    const refused = [
      'http://other.test/v1/subscriptions/rv1.invalid',
      `https://${window.location.host}/v1/subscriptions/rv1.invalid`,
      `${window.location.origin}/v1/profiles/${profileID}/export`,
      'javascript:alert(1)',
      'not a url',
    ]
    for (const value of refused) {
      const payload = buildPayload()
      payload.subscription_url = value
      answer(payload)
      await expect(buildOutput(outputID)).rejects.toBeInstanceOf(
        RoutevaneAPIError,
      )
    }
  })

  // The bearer used to travel back with the output that had just been created.
  it('refuses a created output that still carries a subscription', async () => {
    answer({
      output: { id: outputID, list_id: profileID, target_id: 'keenetic' },
    })
    await expect(addOutput(profileID, 'keenetic')).resolves.toEqual({
      output: { id: outputID, profileID, targetID: 'keenetic', deviceID: '' },
    })

    answer({
      output: { id: outputID, list_id: profileID, target_id: 'keenetic' },
      subscription_url: subscription,
    })
    await expect(addOutput(profileID, 'keenetic')).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })

  it('reads a stored output and its latest file', async () => {
    answer({
      id: outputID,
      list_id: profileID,
      target_id: 'keenetic',
      latest_artifact_id: artifactID,
    })
    await expect(loadOutput(outputID)).resolves.toEqual({
      id: outputID,
      profileID,
      targetID: 'keenetic',
      deviceID: '',
      latestArtifactID: artifactID,
    })
  })
})

describe('a build attempt names either a file or a reason', () => {
  it('accepts the two shapes an attempt can honestly have', async () => {
    answer(
      attemptPayload({
        status: 'success',
        artifact_id: artifactID,
        completed_at: '2026-08-20T12:01:00Z',
      }),
    )
    await expect(loadProfiles()).resolves.toMatchObject([
      {
        outputs: [
          {
            lastAttempt: {
              status: 'success',
              code: '',
              artifactID,
              projectedRules: 0,
              maximumRules: 0,
            },
          },
        ],
      },
    ])

    answer(
      attemptPayload({
        status: 'failed',
        code: 'rule_limit',
        projected_rules: 1740,
        maximum_rules: 1024,
        completed_at: '2026-08-20T12:01:00Z',
      }),
    )
    await expect(loadProfiles()).resolves.toMatchObject([
      {
        outputs: [
          {
            lastAttempt: {
              status: 'failed',
              code: 'rule_limit',
              artifactID: '',
              projectedRules: 1740,
              maximumRules: 1024,
            },
          },
        ],
      },
    ])
  })

  it('refuses an attempt that claims both or neither', async () => {
    const refused = [
      // A success that also names a failure code.
      {
        status: 'success',
        code: 'rule_limit',
        artifact_id: artifactID,
        completed_at: '2026-08-20T12:01:00Z',
      },
      // A success that published nothing.
      { status: 'success', completed_at: '2026-08-20T12:01:00Z' },
      // A failure with no reason.
      { status: 'failed', completed_at: '2026-08-20T12:01:00Z' },
      // A failure that nonetheless points at a file.
      {
        status: 'failed',
        code: 'rule_limit',
        artifact_id: artifactID,
        completed_at: '2026-08-20T12:01:00Z',
      },
      // An outcome outside the closed set, and one with no time.
      { status: 'partial', code: 'x', completed_at: '2026-08-20T12:01:00Z' },
      { status: 'failed', code: 'rule_limit' },
    ]
    for (const attempt of refused) {
      answer(attemptPayload(attempt))
      await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)
    }
  })

  // An absent attempt is a card that honestly has none; a malformed one
  // refuses the row.
  it('separates an absent attempt from a malformed one', async () => {
    answer(attemptPayload({}))
    await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)

    answer(
      profilePayload({
        outputs: [
          {
            id: outputID,
            target_id: 'keenetic',
            target_title: 'Keenetic',
            created_at: '2026-08-20T12:00:00Z',
            last_attempt: null,
            latest: null,
          },
        ],
      }),
    )
    await expect(loadProfiles()).resolves.toMatchObject([
      { outputs: [{ lastAttempt: null, latest: null, targetKind: '' }] },
    ])
  })
})

describe('a deployment form asks only for what it can label', () => {
  it('requires an interface label exactly when an interface is required', async () => {
    answer(requirementsPayload({ needs_interface: false }))
    await expect(loadDeployableTargets()).resolves.toMatchObject([
      { requirements: { needsInterface: false, interfaceLabel: '' } },
    ])

    answer(
      requirementsPayload({
        needs_interface: true,
        interface_label: 'Interface',
      }),
    )
    await expect(loadDeployableTargets()).resolves.toMatchObject([
      { requirements: { needsInterface: true, interfaceLabel: 'Interface' } },
    ])

    answer(requirementsPayload({ needs_interface: true }))
    await expect(loadDeployableTargets()).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )

    answer(requirementsPayload({ needs_interface: true, interface_label: '' }))
    await expect(loadDeployableTargets()).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })

  it('reads a plan under its own envelope', async () => {
    answer({
      plan: {
        artifact_id: artifactID,
        artifact_hash: 'd'.repeat(64),
        target_id: 'keenetic',
        title: 'Keenetic',
        deployer_id: 'keenetic-telnet',
        size_bytes: 0,
      },
    })
    await expect(planDeployment(artifactID, connection)).resolves.toEqual({
      artifactID,
      artifactHash: 'd'.repeat(64),
      targetID: 'keenetic',
      title: 'Keenetic',
      deployerID: 'keenetic-telnet',
      sizeBytes: 0,
    })
  })
})

describe('a refused deployment still describes itself', () => {
  it('reads the audit trail out of a non-2xx reply', async () => {
    answer(
      {
        result: {
          applied: false,
          rolled_back: true,
          device: { deployer_id: 'keenetic-telnet', interface: 'wan' },
          backup: { hash: 'e'.repeat(64) },
          events: [
            { step: 'connect', outcome: 'ok' },
            { step: 'apply', outcome: 'failed', detail: 'refused' },
          ],
        },
        error: 'device refused the change',
      },
      502,
    )
    await expect(applyDeployment(artifactID, connection)).resolves.toEqual({
      applied: false,
      rolledBack: true,
      deployerID: 'keenetic-telnet',
      vendor: '',
      firmwareVersion: '',
      interfaceName: 'wan',
      backupHash: 'e'.repeat(64),
      events: [
        { step: 'connect', outcome: 'ok', detail: '' },
        { step: 'apply', outcome: 'failed', detail: 'refused' },
      ],
      error: 'device refused the change',
    })
  })

  it('reports the server words when the reply is not an outcome', async () => {
    answer({ error: 'artifact unavailable' }, 503)
    await expect(applyDeployment(artifactID, connection)).rejects.toThrow(
      'artifact unavailable',
    )

    answer({ result: {} }, 200)
    await expect(applyDeployment(artifactID, connection)).rejects.toThrow(
      'Применение не выполнено.',
    )
  })

  // A deployment refused before it started took no step, and that is not a
  // malformed reply. A step that does not say what it was is.
  it('reads no steps as none and a nameless step as malformed', async () => {
    answer(outcomePayload({}))
    await expect(
      applyDeployment(artifactID, connection),
    ).resolves.toMatchObject({ events: [], backupHash: '' })

    answer(outcomePayload({ events: 'none' }))
    await expect(
      applyDeployment(artifactID, connection),
    ).resolves.toMatchObject({ events: [] })

    answer(outcomePayload({ events: [{ step: 'connect' }] }))
    await expect(applyDeployment(artifactID, connection)).rejects.toThrow(
      'Применение не выполнено.',
    )

    answer(outcomePayload({ backup: 'gone' }))
    await expect(
      applyDeployment(artifactID, connection),
    ).resolves.toMatchObject({ backupHash: '' })
  })
})

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
      id: customCategoryID,
      title: 'Мои списки',
      lists: ['discord'],
      custom: true,
    }
    answer({ category: written }, 201)
    await expect(createCategory('Мои списки', ['discord'])).resolves.toEqual({
      id: customCategoryID,
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
      id: customCategoryID,
      title: 'Мои списки',
      lists: ['discord'],
      custom: true,
    }
    answer({ category: written })
    await updateCategory(customCategoryID, { lists: ['discord'] })
    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      `/v1/categories/${customCategoryID}/update`,
    )
    expect((fetchMock.mock.calls[0]?.[1] as RequestInit).body).toBe(
      JSON.stringify({ lists: ['discord'] }),
    )

    answer({ category: written })
    await updateCategory(customCategoryID, { title: 'Мои списки' })
    expect((fetchMock.mock.calls[1]?.[1] as RequestInit).body).toBe(
      JSON.stringify({ title: 'Мои списки' }),
    )

    // An empty membership is a request, not an absent field.
    answer({ category: { ...written, lists: [] } })
    await updateCategory(customCategoryID, { lists: [] })
    expect((fetchMock.mock.calls[2]?.[1] as RequestInit).body).toBe(
      JSON.stringify({ lists: [] }),
    )

    answer({ error: 'not found' }, 404)
    await expect(updateCategory(customCategoryID, {})).rejects.toBeInstanceOf(
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
      removeCategory(customCategoryID, 'detach'),
    ).resolves.toBeUndefined()
    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      `/v1/categories/${customCategoryID}/remove`,
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
    await removeCategory(customCategoryID, 'delete')
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
    const refused = await removeCategory(customCategoryID, 'detach').catch(
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
      const other = await removeCategory(customCategoryID, 'detach').catch(
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

describe('diagnostics name the reason at the level that owns it', () => {
  it('reads a kept rule and an excluded candidate out of one plan', async () => {
    answer({
      routing_plan: {
        rules: [
          {
            service_id: 'discord',
            value: 'discord.com',
            reason_codes: ['catalog_domain'],
          },
        ],
        excluded: [
          {
            candidate: { service_id: 'discord', value: '0.0.0.0/0' },
            reason_codes: ['too_broad'],
          },
        ],
      },
    })
    await expect(loadDiagnostics(snapshotID)).resolves.toEqual([
      {
        listID: 'discord',
        value: 'discord.com',
        reasons: ['catalog_domain'],
        excluded: false,
      },
      {
        listID: 'discord',
        value: '0.0.0.0/0',
        reasons: ['too_broad'],
        excluded: true,
      },
    ])
  })

  it('refuses an exclusion with no candidate and a plan with no lists', async () => {
    answer({
      routing_plan: {
        rules: [],
        excluded: [{ service_id: 'discord', reason_codes: ['too_broad'] }],
      },
    })
    await expect(loadDiagnostics(snapshotID)).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )

    answer({ routing_plan: { rules: [] } })
    await expect(loadDiagnostics(snapshotID)).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })
})

describe('an export is read through the same envelope', () => {
  it('states every fact a format needs or offers it at all', async () => {
    answer({
      formats: [
        {
          id: 'raw-json',
          renderer_id: 'raw-json',
          file_extension: 'json',
          content_type: 'application/json',
        },
      ],
    })
    await expect(loadExportFormats()).resolves.toEqual([
      {
        id: 'raw-json',
        rendererID: 'raw-json',
        fileExtension: 'json',
        contentType: 'application/json',
      },
    ])

    answer({ formats: [{ id: 'raw-json', renderer_id: 'raw-json' }] })
    await expect(loadExportFormats()).rejects.toThrow(
      'Сервер вернул ответ вне контракта.',
    )

    answer({ error: 'export unavailable' }, 503)
    await expect(loadExportFormats()).rejects.toThrow('export unavailable')
  })

  it('refuses a download the server did not name', async () => {
    fetchMock.mockResolvedValueOnce(
      new Response('{"rules":[]}', {
        status: 200,
        headers: { 'Content-Disposition': 'attachment' },
      }),
    )
    await expect(requestProfileExport(profileID, 'raw-json')).rejects.toThrow(
      'Сервер не указал имя файла.',
    )

    fetchMock.mockResolvedValueOnce(
      new Response('nope', { status: 500, headers: {} }),
    )
    await expect(requestProfileExport(profileID, 'raw-json')).rejects.toThrow(
      'Операция не выполнена.',
    )
  })
})
