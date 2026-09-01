import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  loadCatalog,
  localizedTargetHint,
  localizedTargetTitle,
  previewService,
} from './catalog'
import { loadExportFormats, requestListExport } from './exports'
import { createList, loadLists, previewComposition } from './lists'
import { addOutput, buildOutput, setOutputDevice } from './outputs'
import { RoutevaneAPIError } from './http'

const listID = 'a'.repeat(32)
const outputID = 'f'.repeat(32)
const artifactID = 'b'.repeat(32)
const subscription = `${window.location.origin}/v1/subscriptions/rv1.${'c'.repeat(32)}.${'d'.repeat(43)}`

function json(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function outputPayload(subscriptionURL = subscription): unknown {
  return {
    output: {
      id: outputID,
      list_id: listID,
      target_id: 'keenetic',
    },
    subscription_url: subscriptionURL,
  }
}

type BuildPayload = {
  output: { id: string; list_id: string; target_id: string }
  snapshot: { id: string; routing_plan?: unknown }
  artifact: {
    id: string
    artifact_hash: string
    content_created_at: string
    validation_status: string
    status: string
    renderer_id: string
    renderer_version: string
    content_type: string
  }
  summary: {
    rule_count: number
    partial_coverage: boolean
    partial_coverage_count: number
    content_created_at: string
    validation_status: string
    status: string
  }
  subscription_url?: string
}

function buildPayload(): BuildPayload {
  return {
    output: { id: outputID, list_id: listID, target_id: 'keenetic' },
    snapshot: { id: 'c'.repeat(32) },
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

describe('Routevane local API decoders', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    fetchMock.mockReset()
    vi.stubGlobal('fetch', fetchMock)
  })

  it('loads only a valid catalog and keeps requests relative', async () => {
    fetchMock.mockResolvedValueOnce(
      json({
        services: ['discord', 'youtube'],
        service_details: [
          {
            id: 'discord',
            title: 'Discord',
            categories: ['communication'],
            domains: [{ value: 'discord.com', include_subdomains: true }],
            sources: [
              { id: 'itdoginfo', type: 'http' },
              { id: 'v2fly', type: 'http' },
            ],
            source_count: 5,
          },
          { id: 'youtube', title: 'YouTube', categories: ['video'] },
        ],
        categories: [
          {
            id: 'communication',
            title: 'Общение',
            services: ['discord'],
            custom: false,
          },
          {
            id: 'video',
            title: 'Видео',
            services: ['youtube'],
            custom: true,
          },
        ],
      }),
    )
    fetchMock.mockResolvedValueOnce(
      json({
        targets: [
          {
            id: 'keenetic',
            title: 'Keenetic',
            kind: 'router',
            profile_key: 'keenetic-bat-ipv4-v1',
            renderer_id: 'keenetic-route-bat',
            file_extension: 'bat',
            manual_installation_hint: 'Import the file.',
          },
          {
            id: 'mikrotik',
            title: 'MikroTik',
            title_en: 'MikroTik router',
            kind: 'router',
            profile_key: 'mikrotik-address-list-v1',
            renderer_id: 'mikrotik-address-list-rsc',
            file_extension: 'rsc',
            manual_installation_hint: 'выполните /import',
            manual_installation_hint_en: 'run /import',
          },
        ],
      }),
    )

    await expect(loadCatalog()).resolves.toEqual({
      services: ['discord', 'youtube'],
      serviceDetails: [
        {
          id: 'discord',
          title: 'Discord',
          categories: ['communication'],
          domains: [{ value: 'discord.com', includeSubdomains: true }],
          sources: [
            { id: 'itdoginfo', type: 'http' },
            { id: 'v2fly', type: 'http' },
          ],
          sourceCount: 5,
        },
        {
          id: 'youtube',
          title: 'YouTube',
          categories: ['video'],
          domains: [],
          sources: [],
          sourceCount: 0,
        },
      ],
      categories: [
        {
          id: 'communication',
          title: 'Общение',
          services: ['discord'],
          custom: false,
        },
        {
          id: 'video',
          title: 'Видео',
          services: ['youtube'],
          custom: true,
        },
      ],
      targets: [
        // No English rendition in the catalog is the normal case, and it
        // decodes as an absent one rather than a refusal.
        {
          id: 'keenetic',
          title: 'Keenetic',
          titleEn: '',
          kind: 'router',
          profileKey: 'keenetic-bat-ipv4-v1',
          rendererID: 'keenetic-route-bat',
          fileExtension: 'bat',
          manualInstallationHint: 'Import the file.',
          manualInstallationHintEn: '',
        },
        {
          id: 'mikrotik',
          title: 'MikroTik',
          titleEn: 'MikroTik router',
          kind: 'router',
          profileKey: 'mikrotik-address-list-v1',
          rendererID: 'mikrotik-address-list-rsc',
          fileExtension: 'rsc',
          manualInstallationHint: 'выполните /import',
          manualInstallationHintEn: 'run /import',
        },
      ],
    })
    expect(fetchMock.mock.calls.map(([path]) => path)).toEqual([
      '/v1/services',
      '/v1/targets',
    ])
  })

  it('checks automatic service contents without a list mutation', async () => {
    fetchMock.mockResolvedValueOnce(
      json({
        service_id: 'google-ai',
        sources: [
          {
            id: 'iplist',
            type: 'http',
            status: 'ready',
            domains: [],
            domain_count: 0,
            address_count: 0,
            prefix_count: 32,
            skipped_count: 0,
          },
          {
            id: 'itdoginfo',
            type: 'http',
            status: 'ready',
            domains: ['ai.google.dev', 'aistudio.google.com'],
            domain_count: 2,
            address_count: 0,
            prefix_count: 0,
            skipped_count: 1,
          },
        ],
        domains: ['ai.google.dev', 'aistudio.google.com'],
        domain_count: 2,
        address_count: 0,
        prefix_count: 32,
        skipped_count: 1,
      }),
    )

    await expect(previewService('google-ai')).resolves.toMatchObject({
      serviceID: 'google-ai',
      domains: ['ai.google.dev', 'aistudio.google.com'],
      domainCount: 2,
      prefixCount: 32,
      sources: [
        { id: 'iplist', status: 'ready', prefixCount: 32 },
        { id: 'itdoginfo', status: 'ready', domainCount: 2 },
      ],
    })
    expect(fetchMock).toHaveBeenCalledWith('/v1/services/google-ai/preview', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Routevane-Request': '1',
      },
      body: '{}',
    })
  })

  it('decodes library rows and refuses a malformed latest artifact', async () => {
    const output = {
      id: outputID,
      target_id: 'keenetic',
      target_title: 'Keenetic',
      target_kind: 'router',
      file_extension: 'bat',
      created_at: '2026-08-20T12:00:00Z',
      latest: {
        id: artifactID,
        snapshot_id: 'c'.repeat(32),
        size_bytes: 42,
        content_type: 'application/x-bat',
        content_created_at: '2026-08-20T12:00:00Z',
      },
      last_attempt: {
        status: 'failed',
        code: 'rule_limit',
        projected_rules: 1740,
        maximum_rules: 1024,
        completed_at: '2026-08-20T12:01:00Z',
      },
    }
    const gone = {
      id: 'e'.repeat(32),
      target_id: 'gone-device',
      target_title: 'gone-device',
      created_at: '2026-08-20T12:00:00Z',
    }
    const list = {
      id: listID,
      name: 'Видео',
      services: ['discord'],
      categories: ['video'],
      exclusions: [],
      resolved: ['discord', 'youtube'],
      missing_categories: [],
      created_at: '2026-08-20T12:00:00Z',
      updated_at: '2026-08-20T12:00:00Z',
      outputs: [output, gone],
    }
    fetchMock.mockResolvedValueOnce(json({ lists: [list] }))
    const cards = await loadLists()
    expect(cards).toHaveLength(1)
    expect(cards[0]?.outputs[0]?.latest?.snapshotID).toBe('c'.repeat(32))
    expect(cards[0]?.outputs[0]?.lastAttempt).toMatchObject({
      status: 'failed',
      code: 'rule_limit',
      projectedRules: 1740,
      maximumRules: 1024,
    })
    // An output whose target left the catalog still lists, without kind or
    // format.
    expect(cards[0]?.outputs[1]).toMatchObject({
      targetKind: '',
      fileExtension: '',
      latest: null,
    })

    fetchMock.mockResolvedValueOnce(
      json({
        lists: [
          { ...list, outputs: [{ ...output, latest: { id: artifactID } }] },
        ],
      }),
    )
    await expect(loadLists()).rejects.toBeInstanceOf(RoutevaneAPIError)
  })

  it('lists independent export formats and downloads one without an output id', async () => {
    fetchMock.mockResolvedValueOnce(
      json({
        formats: [
          {
            id: 'raw-json',
            renderer_id: 'raw-json',
            file_extension: 'json',
            content_type: 'application/json',
          },
        ],
      }),
    )
    await expect(loadExportFormats()).resolves.toEqual([
      {
        id: 'raw-json',
        rendererID: 'raw-json',
        fileExtension: 'json',
        contentType: 'application/json',
      },
    ])

    fetchMock.mockResolvedValueOnce(
      new Response('{"rules":[]}', {
        status: 200,
        headers: {
          'Content-Type': 'application/json',
          'Content-Disposition': `attachment; filename="routevane-${listID}-raw-json.json"`,
        },
      }),
    )
    const file = await requestListExport(listID, 'raw-json')
    expect(file.fileName).toBe(`routevane-${listID}-raw-json.json`)
    expect(await file.blob.text()).toBe('{"rules":[]}')
    expect(fetchMock).toHaveBeenLastCalledWith(`/v1/lists/${listID}/export`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Routevane-Request': '1',
      },
      body: JSON.stringify({ format_id: 'raw-json' }),
    })
  })

  it('rejects malformed and cross-origin bearer-bearing output responses', async () => {
    fetchMock.mockResolvedValueOnce(
      json(outputPayload('http://other.test/v1/subscriptions/rv1.invalid')),
    )
    await expect(addOutput(listID, 'keenetic')).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )

    fetchMock.mockResolvedValueOnce(json({ output: { id: outputID } }))
    await expect(addOutput(listID, 'keenetic')).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })

  it('keeps bounded build failure details on API errors', async () => {
    fetchMock.mockResolvedValueOnce(
      json(
        {
          error: 'operation failed',
          code: 'rule_limit',
          projected_rules: 1740,
          maximum_rules: 1024,
        },
        422,
      ),
    )
    const error = await addOutput(listID, 'keenetic').catch(
      (reason: unknown) => reason,
    )
    expect(error).toBeInstanceOf(RoutevaneAPIError)
    expect(error).toMatchObject({
      code: 'rule_limit',
      details: { projected_rules: 1740, maximum_rules: 1024 },
      status: 422,
    })
  })

  it('sends the exact mutation guards and rejects unsafe or inconsistent builds', async () => {
    fetchMock.mockResolvedValueOnce(
      json({
        list: {
          id: listID,
          name: 'Видео',
          services: ['discord', 'youtube'],
          categories: ['video'],
          exclusions: [],
          created_at: '2026-08-20T12:00:00Z',
          updated_at: '2026-08-20T12:00:00Z',
        },
      }),
    )
    await createList('Видео', {
      services: ['youtube', 'discord'],
      categories: ['video'],
      exclusions: [],
      serviceDomains: {},
    })
    expect(fetchMock).toHaveBeenCalledWith('/v1/lists', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Routevane-Request': '1',
      },
      body: JSON.stringify({
        name: 'Видео',
        services: ['youtube', 'discord'],
        categories: ['video'],
        exclusions: [],
        service_domains: {},
      }),
    })

    fetchMock.mockResolvedValueOnce(json(buildPayload()))
    await expect(buildOutput(outputID)).resolves.toEqual({
      output: { id: outputID, listID, targetID: 'keenetic', deviceID: '' },
      snapshotID: 'c'.repeat(32),
      artifactID,
      artifactHash: 'd'.repeat(64),
      contentCreatedAt: '2026-08-20T12:00:00Z',
      validationStatus: 'valid',
      status: 'published',
      ruleCount: 2,
      partialCoverage: false,
      partialCoverageCount: 0,
      rendererID: 'keenetic-route-bat',
      rendererVersion: '1.0.0',
      contentType: 'application/x-bat',
      degradedSources: [],
      subscriptionURL: subscription,
    })

    fetchMock.mockResolvedValueOnce(
      json({
        output: {
          id: outputID,
          list_id: listID,
          target_id: 'keenetic',
          device_id: 'e'.repeat(32),
        },
      }),
    )
    await expect(setOutputDevice(outputID, 'e'.repeat(32))).resolves.toEqual({
      output: {
        id: outputID,
        listID,
        targetID: 'keenetic',
        deviceID: 'e'.repeat(32),
      },
    })
    expect(fetchMock).toHaveBeenLastCalledWith(
      `/v1/outputs/${outputID}/device`,
      {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Routevane-Request': '1',
        },
        body: JSON.stringify({ device_id: 'e'.repeat(32) }),
      },
    )

    for (const field of [
      'renderer_id',
      'renderer_version',
      'content_type',
    ] as const) {
      const base = buildPayload()
      const missing = {
        ...base,
        artifact: Object.fromEntries(
          Object.entries(base.artifact).filter(([key]) => key !== field),
        ),
      }
      fetchMock.mockResolvedValueOnce(json(missing))
      await expect(buildOutput(outputID)).rejects.toBeInstanceOf(
        RoutevaneAPIError,
      )
    }

    const rawPlan = buildPayload()
    rawPlan.snapshot.routing_plan = { canary: 'must-never-render' }
    fetchMock.mockResolvedValueOnce(json(rawPlan))
    await expect(buildOutput(outputID)).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )

    const inconsistent = buildPayload()
    inconsistent.summary.validation_status = 'invalid'
    fetchMock.mockResolvedValueOnce(json(inconsistent))
    await expect(buildOutput(outputID)).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )

    const unsafeSubscription = buildPayload()
    unsafeSubscription.subscription_url =
      'http://other.test/v1/subscriptions/rv1.invalid'
    fetchMock.mockResolvedValueOnce(json(unsafeSubscription))
    await expect(buildOutput(outputID)).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })
})

describe('catalog wording per locale', () => {
  const keenetic = {
    manualInstallationHint: 'Импортируйте файл.',
    manualInstallationHintEn: '',
    title: 'Keenetic',
    titleEn: '',
  }
  const mikrotik = {
    manualInstallationHint: 'выполните /import',
    manualInstallationHintEn: 'run /import',
    title: 'MikroTik',
    titleEn: 'MikroTik router',
  }

  it('prefers the English rendition only when English is being read', () => {
    expect(localizedTargetTitle(mikrotik, 'en')).toBe('MikroTik router')
    expect(localizedTargetTitle(mikrotik, 'ru')).toBe('MikroTik')
    expect(localizedTargetHint(mikrotik, 'en')).toBe('run /import')
    expect(localizedTargetHint(mikrotik, 'ru')).toBe('выполните /import')
  })

  // A catalog that names an entry once names it correctly in both locales.
  it('falls back to the catalog wording when there is no rendition', () => {
    expect(localizedTargetTitle(keenetic, 'en')).toBe('Keenetic')
    expect(localizedTargetHint(keenetic, 'en')).toBe('Импортируйте файл.')
  })

  // An output whose target has left the catalog keeps the title stored with
  // it rather than showing an identifier or nothing at all.
  it('answers with the caller fallback when the target is gone', () => {
    expect(localizedTargetTitle(null, 'en', 'Retired device')).toBe(
      'Retired device',
    )
    expect(localizedTargetTitle(undefined, 'ru')).toBe('')
    expect(localizedTargetHint(null, 'en')).toBe('')
  })
})

describe('composition forecast', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    fetchMock.mockReset()
    vi.stubGlobal('fetch', fetchMock)
  })

  it('asks about the draft as stored and decodes every format it names', async () => {
    fetchMock.mockResolvedValueOnce(
      json({
        targets: [
          {
            target_id: 'keenetic',
            maximum_rules: 1024,
            projected_rules: 12,
            fits: true,
            per_service: [
              { service_id: 'discord', rules: 5 },
              { service_id: 'youtube', rules: 7 },
            ],
          },
          {
            target_id: 'limited-fixture',
            maximum_rules: 1,
            projected_rules: 12,
            fits: false,
            per_service: [],
          },
        ],
      }),
    )

    await expect(
      previewComposition(
        {
          services: ['discord'],
          categories: ['video'],
          exclusions: [],
          serviceDomains: {},
        },
        ['keenetic', 'limited-fixture'],
      ),
    ).resolves.toEqual([
      {
        targetID: 'keenetic',
        maximumRules: 1024,
        projectedRules: 12,
        fits: true,
        perService: [
          { serviceID: 'discord', rules: 5 },
          { serviceID: 'youtube', rules: 7 },
        ],
      },
      {
        targetID: 'limited-fixture',
        maximumRules: 1,
        projectedRules: 12,
        fits: false,
        perService: [],
      },
    ])
    expect(fetchMock).toHaveBeenCalledWith('/v1/lists/preview', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Routevane-Request': '1',
      },
      body: JSON.stringify({
        services: ['discord'],
        categories: ['video'],
        exclusions: [],
        service_domains: {},
        targets: ['keenetic', 'limited-fixture'],
      }),
    })
  })

  // No named format means "every format this build knows", which the request
  // states by leaving the field out rather than by sending an empty list.
  it('omits the target list when the caller names none', async () => {
    fetchMock.mockResolvedValueOnce(json({ targets: [] }))
    await expect(
      previewComposition({
        services: ['discord'],
        categories: [],
        exclusions: [],
        serviceDomains: {},
      }),
    ).resolves.toEqual([])
    expect(String(fetchMock.mock.calls[0]?.[1]?.body)).not.toContain('targets')
  })

  it('refuses a forecast that does not state its own bounds', async () => {
    fetchMock.mockResolvedValueOnce(
      json({
        targets: [
          { target_id: 'keenetic', projected_rules: 3, per_service: [] },
        ],
      }),
    )
    await expect(
      previewComposition({
        services: ['discord'],
        categories: [],
        exclusions: [],
        serviceDomains: {},
      }),
    ).rejects.toBeInstanceOf(RoutevaneAPIError)
  })
})
