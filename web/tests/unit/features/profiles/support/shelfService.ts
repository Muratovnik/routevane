/**
 * The local service as the profiles shelf sees it: a library of profiles that
 * answers the shelf reads, the archive, restore and delete writes, a rebuild
 * and the decorations a row is drawn from. It is stateful, so a write is
 * visible to the read that follows it, and it records every request, so a spec
 * can state what was sent and what was not.
 */
import { vi } from 'vitest'

export type FakeOutput = {
  id: string
  target: string
  title: string
  /** Whether a build has ever succeeded: only then is there a published file. */
  published: boolean
  /** The last attempt failed. */
  failed?: boolean
  deviceID?: string
}

export type FakeProfile = {
  id: string
  name: string
  archived: boolean
  outputs: FakeOutput[]
}

export type ShelfCall = { method: string; path: string }

type ServiceOptions = {
  profiles: FakeProfile[]
  devices?: { id: string; name: string }[]
  /** Targets a published file can be sent to. */
  deployable?: string[]
  /** What the delete route answers instead of agreeing. */
  deleteRefusal?: { status: number; body: unknown }
  /** Builds answer with a failure instead of a published file. */
  buildFails?: boolean
  /** Archived profiles beyond the ones listed, which the server counts. */
  archivedElsewhere?: number
}

const TIMESTAMP = '2026-09-12T12:00:00Z'

export const fakeOutput = (
  id: string,
  title: string,
  extra: Partial<FakeOutput> = {},
): FakeOutput => ({
  id,
  published: true,
  target: title.toLowerCase(),
  title,
  ...extra,
})

export const fakeProfile = (
  id: string,
  name: string,
  outputs: FakeOutput[] = [],
  archived = false,
): FakeProfile => ({ archived, id, name, outputs })

const artifactOf = (output: FakeOutput): string => `artifact-${output.id}`

const outputCard = (output: FakeOutput): Record<string, unknown> => ({
  id: output.id,
  target_id: output.target,
  target_title: output.title,
  device_id: output.deviceID ?? '',
  created_at: TIMESTAMP,
  latest: output.published
    ? {
        id: artifactOf(output),
        snapshot_id: `snapshot-${output.id}`,
        size_bytes: 14,
        content_type: 'text/plain',
        content_created_at: TIMESTAMP,
      }
    : null,
  last_attempt: output.failed
    ? {
        status: 'failed',
        code: 'source_unavailable',
        completed_at: TIMESTAMP,
      }
    : null,
})

const record = (profile: FakeProfile): Record<string, unknown> => ({
  id: profile.id,
  name: profile.name,
  lists: ['example'],
  categories: [],
  exclusions: [],
  archived_at: profile.archived ? '2026-09-13T12:00:00Z' : '',
  created_at: TIMESTAMP,
  updated_at: TIMESTAMP,
})

const card = (profile: FakeProfile): Record<string, unknown> => ({
  ...record(profile),
  resolved: ['example'],
  missing_categories: [],
  outputs: profile.outputs.map(outputCard),
})

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    headers: { 'Content-Type': 'application/json' },
    status,
  })

const buildPayload = (profile: FakeProfile, outputID: string): unknown => {
  const output = profile.outputs.find((candidate) => candidate.id === outputID)
  return {
    output: { id: outputID, list_id: profile.id, target_id: output?.target },
    snapshot: { id: `snapshot-${outputID}` },
    artifact: {
      id: `artifact-${outputID}`,
      artifact_hash: 'd'.repeat(64),
      content_created_at: '2026-09-14T12:00:00Z',
      validation_status: 'valid',
      status: 'published',
      renderer_id: 'fixture',
      renderer_version: '1.0.0',
      content_type: 'text/plain',
    },
    summary: {
      rule_count: 2,
      partial_coverage: false,
      partial_coverage_count: 0,
      content_created_at: '2026-09-14T12:00:00Z',
      validation_status: 'valid',
      status: 'published',
    },
  }
}

export const installShelfService = (options: ServiceOptions) => {
  const library = options.profiles.map((profile) => ({ ...profile }))
  let deleted = 0
  const calls: ShelfCall[] = []
  const find = (id: string): FakeProfile | undefined =>
    library.find((profile) => profile.id === id)
  const archivedCount = (): number =>
    library.filter((profile) => profile.archived).length +
    (options.archivedElsewhere ?? 0)

  const route = (path: string, method: string): Response | null => {
    const shelf = /^\/v1\/profile-shelf\/(active|archived)$/.exec(path)
    if (shelf !== null) {
      const wanted = shelf[1] === 'archived'
      return json({
        profiles: library
          .filter((profile) => profile.archived === wanted)
          .map(card),
        next: '',
        archived_count: archivedCount(),
      })
    }
    const write =
      /^\/v1\/profiles\/([^/]+)\/(archive|restore|delete|refresh)$/.exec(path)
    if (write !== null && method === 'POST') {
      const profile = find(write[1] ?? '')
      if (profile === undefined) return json({ error: 'not found' }, 404)
      if (write[2] === 'refresh') return json({ refresh: [] })
      if (write[2] === 'delete') {
        if (options.deleteRefusal !== undefined)
          return json(options.deleteRefusal.body, options.deleteRefusal.status)
        library.splice(library.indexOf(profile), 1)
        deleted += 1
        return json({ id: profile.id, deleted_at: '2026-09-15T12:00:00Z' })
      }
      profile.archived = write[2] === 'archive'
      return json({ profile: record(profile) })
    }
    const detail = /^\/v1\/profiles\/([^/]+)$/.exec(path)
    if (detail !== null) {
      const profile = find(detail[1] ?? '')
      if (profile === undefined) return json({ error: 'not found' }, 404)
      return json({
        profile: record(profile),
        outputs: profile.outputs.map(outputCard),
        resolved: ['example'],
        missing_categories: [],
        schedule: { interval: '', effective: 'daily' },
      })
    }
    const build = /^\/v1\/outputs\/([^/]+)\/build$/.exec(path)
    if (build !== null && method === 'POST') {
      const profile = library.find((candidate) =>
        candidate.outputs.some((output) => output.id === build[1]),
      )
      if (profile === undefined) return json({ error: 'not found' }, 404)
      if (options.buildFails === true)
        return json(
          { error: 'operation failed', code: 'source_unavailable' },
          422,
        )
      return json(buildPayload(profile, build[1] ?? ''))
    }
    const artifact = /^\/v1\/artifacts\/(.+)$/.exec(path)
    if (artifact !== null) return new Response(`bytes of ${artifact[1]}`)
    return null
  }

  const decorations: Record<string, unknown> = {
    '/v1/lists': { lists: [], list_details: [], categories: [] },
    '/v1/targets': { targets: [] },
    '/v1/deployments/targets': {
      targets: (options.deployable ?? []).map((target) => ({
        target_id: target,
        title: target,
        deployer_id: `${target}-deployer`,
        requirements: {
          address_label: 'Address',
          address_example: '192.0.2.1',
          needs_credential: false,
          needs_interface: false,
        },
      })),
    },
    '/v1/export-formats': {
      formats: [
        {
          id: 'fixture',
          renderer_id: 'fixture-renderer',
          file_extension: 'txt',
          content_type: 'text/plain',
        },
      ],
    },
    '/v1/devices': {
      devices: (options.devices ?? []).map((device) => ({
        id: device.id,
        target_id: 'keenetic',
        target_title: 'Keenetic',
        name: device.name,
        address: '192.0.2.1',
      })),
    },
  }

  const fetchMock = vi.fn((input: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    calls.push({ method, path: input })
    const decoration = decorations[input]
    if (decoration !== undefined) return Promise.resolve(json(decoration))
    const answered = route(input, method)
    if (answered !== null) return Promise.resolve(answered)
    return Promise.reject(new Error(`unexpected request ${method} ${input}`))
  })
  vi.stubGlobal('fetch', fetchMock)

  return {
    calls,
    deleted: (): number => deleted,
    posts: (): string[] =>
      calls.filter((call) => call.method === 'POST').map((call) => call.path),
  }
}
