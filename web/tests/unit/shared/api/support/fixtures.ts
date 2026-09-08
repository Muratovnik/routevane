/**
 * The replies the API contract specs beside this file are read against.
 *
 * Each of those specs covers one area of the contract — the envelope, the
 * forecast, a build, a deployment, a stored record, the diagnostics, an
 * export — and they answer through one mocked `fetch`. The ids and payload
 * builders live here so every area states the same well-formed reply and
 * differs only in the field it is about.
 */
import { vi } from 'vitest'

export const profileID = 'a'.repeat(32)
export const outputID = 'f'.repeat(32)
export const artifactID = 'b'.repeat(32)
export const snapshotID = 'c'.repeat(32)
export const CUSTOM_CATEGORY_ID = 'custom-1234567890abcdef'
export const subscription = `${window.location.origin}/v1/subscriptions/rv1.${'c'.repeat(32)}.${'d'.repeat(43)}`

export const fetchMock = vi.fn()

/**
 * Registered by each spec in its own `beforeEach`, so no case inherits an
 * answer queued by the one before it.
 */
export const stubFetch = (): void => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
}

export const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

export const answer = (payload: unknown, status = 200): void => {
  fetchMock.mockResolvedValueOnce(json(payload, status))
}

export const connection = {
  device: '192.168.1.1',
  username: 'admin',
  password: 'secret',
  interfaceName: 'wan',
}

export const buildPayload = (): Record<string, unknown> => ({
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
})

export const profilePayload = (
  fields: Record<string, unknown> = {},
): unknown => ({
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
})

export const attemptPayload = (attempt: Record<string, unknown>): unknown =>
  profilePayload({
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

export const forecastPayload = (target: Record<string, unknown>): unknown => ({
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
})

export const draft = {
  lists: ['discord'],
  categories: [],
  exclusions: [],
  listDomains: {},
}

export const requirementsPayload = (
  requirements: Record<string, unknown>,
): unknown => ({
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
})

export const outcomePayload = (result: Record<string, unknown>): unknown => ({
  result: {
    applied: true,
    rolled_back: false,
    device: { deployer_id: 'keenetic-telnet', vendor: 'Keenetic' },
    ...result,
  },
})
