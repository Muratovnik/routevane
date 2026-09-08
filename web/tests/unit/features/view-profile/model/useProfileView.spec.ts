import { beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import {
  usePublishedProfile,
  type FreshBuild,
} from '@/entities/profile-build/model/publishedProfile'

import { useProfileView } from '@/features/view-profile/model/useProfileView'
import { invalidateCatalogCache } from '@/shared/api/catalog'

const PROFILE_ID = 'profile-1'
const OUTPUT_A = 'output-a'
const OUTPUT_B = 'output-b'
const ARTIFACT_A = 'artifact-a'
const ARTIFACT_B = 'artifact-b'

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const outputCard = (id: string, artifactID: string, snapshotID: string) => ({
  id,
  target_id: `${id}-target`,
  target_title: `${id} target`,
  target_kind: 'router',
  file_extension: 'bat',
  created_at: '2026-08-20T12:00:00Z',
  latest: {
    id: artifactID,
    snapshot_id: snapshotID,
    size_bytes: 10,
    content_type: 'text/plain',
    content_created_at: '2026-08-20T12:00:00Z',
  },
})

const buildPayload = (id: string, artifactID: string) => ({
  output: {
    id,
    list_id: PROFILE_ID,
    target_id: `${id}-target`,
  },
  snapshot: { id: `snapshot-${id}` },
  artifact: {
    id: artifactID,
    artifact_hash: 'd'.repeat(64),
    content_created_at: '2026-08-20T12:00:00Z',
    validation_status: 'valid',
    status: 'published',
    renderer_id: 'test-renderer',
    renderer_version: '1.0.0',
    content_type: 'text/plain',
  },
  summary: {
    rule_count: 1,
    partial_coverage: false,
    partial_coverage_count: 0,
    content_created_at: '2026-08-20T12:00:00Z',
    validation_status: 'valid',
    status: 'published',
  },
})

const profileDetailPayload = (): unknown => ({
  profile: {
    id: PROFILE_ID,
    name: 'Список для проверки',
    lists: [],
    categories: [],
    exclusions: [],
    refresh_interval: '',
    last_refreshed_at: '',
    last_refresh_failed: false,
    archived_at: '',
    created_at: '2026-08-20T12:00:00Z',
    updated_at: '2026-08-20T12:00:00Z',
  },
  outputs: [
    outputCard(OUTPUT_A, ARTIFACT_A, 'snapshot-a'),
    outputCard(OUTPUT_B, ARTIFACT_B, 'snapshot-b'),
  ],
  resolved: [],
  missing_categories: [],
  schedule: {
    interval: '',
    effective: 'off',
    follows_default: true,
    last_refreshed_at: '',
    last_refresh_failed: false,
    next_refresh_at: '',
  },
})

const emptyCatalogAndDeployables = (): void => {
  fetchMock.mockResolvedValueOnce(
    json({ lists: [], list_details: [], categories: [] }),
  )
  fetchMock.mockResolvedValueOnce(json({ targets: [] }))
  fetchMock.mockResolvedValueOnce(json({ targets: [] }))
}

/** The four requests `initialize()` fires, in the order it fires them. */
const queueInitializeFetches = (profile: Response | Error): void => {
  if (profile instanceof Error) {
    fetchMock.mockRejectedValueOnce(profile)
  } else {
    fetchMock.mockResolvedValueOnce(profile)
  }
  emptyCatalogAndDeployables()
}

const deferred = <T>(): {
  promise: Promise<T>
  resolve: (value: T) => void
} => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
  // The session-scoped catalog copy must not leak between tests: each test
  // stages its own server answers.
  invalidateCatalogCache()
})

describe('initialize', () => {
  // A profile the server has genuinely never heard of, versus a list that
  // cannot answer right now, are different facts to an operator: only the
  // first should read "not found" with no way back in. Everything else must
  // land on 'failed', where ProfileView.vue renders a working retry button.
  it('reads a 404 as the list not existing, not as the service being down', async () => {
    queueInitializeFetches(json({ error: 'list not found' }, 404))
    const view = useProfileView(() => PROFILE_ID)

    await view.initialize()

    expect(view.state.value).toBe('missing')
  })

  it('reads a 5xx as the service being unavailable, not as the list not existing', async () => {
    queueInitializeFetches(json({ error: 'internal' }, 500))
    const view = useProfileView(() => PROFILE_ID)

    await view.initialize()

    expect(view.state.value).toBe('failed')
  })

  it('reads a network failure as the service being unavailable', async () => {
    queueInitializeFetches(new TypeError('Failed to fetch'))
    const view = useProfileView(() => PROFILE_ID)

    await view.initialize()

    expect(view.state.value).toBe('failed')
  })

  it('recovers to ready once the service answers again', async () => {
    queueInitializeFetches(json({ error: 'internal' }, 500))
    const view = useProfileView(() => PROFILE_ID)
    await view.initialize()
    expect(view.state.value).toBe('failed')

    queueInitializeFetches(json(profileDetailPayload()))
    await view.initialize()

    expect(view.state.value).toBe('ready')
  })
})

describe('openContent race with output selection', () => {
  // openContent() and selectOutput() both touch content/contentState.
  // A response that lands after the operator has already moved on to a
  // different output must be dropped, not written over the newly selected
  // output's (idle) state.
  it('does not let a stale content response overwrite the state after switching output', async () => {
    queueInitializeFetches(json(profileDetailPayload()))
    const view = useProfileView(() => PROFILE_ID)
    await view.initialize()
    expect(view.selectedOutputID.value).toBe(OUTPUT_A)

    const staleResponse = deferred<Response>()
    fetchMock.mockImplementationOnce(() => staleResponse.promise)

    const openPromise = view.openContent()
    expect(view.contentState.value).toBe('loading')

    // The operator picks a different output before output A's file arrives.
    view.selectOutput(OUTPUT_B)
    expect(view.contentState.value).toBe('idle')
    expect(view.content.value).toBeNull()

    // Output A's file now arrives, late.
    staleResponse.resolve(
      new Response('stale contents for output A', {
        status: 200,
        headers: { 'Content-Type': 'text/plain' },
      }),
    )
    await openPromise

    expect(view.contentState.value).toBe('idle')
    expect(view.content.value).toBeNull()
    expect(view.selectedOutputID.value).toBe(OUTPUT_B)
  })

  it('loads content normally for the output currently selected', async () => {
    queueInitializeFetches(json(profileDetailPayload()))
    const view = useProfileView(() => PROFILE_ID)
    await view.initialize()

    fetchMock.mockResolvedValueOnce(
      new Response('contents for output A', {
        status: 200,
        headers: { 'Content-Type': 'text/plain' },
      }),
    )
    await view.openContent()

    expect(view.contentState.value).toBe('ready')
    expect(view.content.value?.text).toBe('contents for output A')
  })
})

describe('rebuild', () => {
  it('continues with the remaining outputs when one build fails', async () => {
    queueInitializeFetches(json(profileDetailPayload()))
    const view = useProfileView(() => PROFILE_ID)
    await view.initialize()

    fetchMock.mockResolvedValueOnce(json({ refresh: [] }))
    fetchMock.mockResolvedValueOnce(
      json(
        {
          error: 'projected rules exceed the target limit',
          code: 'rule_limit',
          projected_rules: 1152,
          maximum_rules: 1024,
        },
        422,
      ),
    )
    fetchMock.mockResolvedValueOnce(json(buildPayload(OUTPUT_B, ARTIFACT_B)))
    queueInitializeFetches(json(profileDetailPayload()))

    await view.rebuild()

    const requested = fetchMock.mock.calls.map(([url]) => String(url))
    expect(requested).toContain(`/v1/outputs/${OUTPUT_A}/build`)
    expect(requested).toContain(`/v1/outputs/${OUTPUT_B}/build`)
    expect(view.rebuildFailed.value).toBe(true)
    expect(view.work.value).toBe('failed')
  })
})

it('requires a new reveal when the selected output or its subscription changes', () => {
  const published = usePublishedProfile()
  published.clear()
  const build = (id: string, revision = 'first'): FreshBuild => ({
    output: { id, profileID: PROFILE_ID, targetID: 'keenetic', deviceID: '' },
    subscriptionURL: `http://example.test/${id}/${revision}`,
    artifactID: `artifact-${id}`,
    artifactHash: 'fixture-hash',
    snapshotID: `snapshot-${id}`,
    contentCreatedAt: '2026-09-08T00:00:00Z',
    validationStatus: 'valid',
    status: 'published',
    ruleCount: 1,
    partialCoverage: false,
    partialCoverageCount: 0,
    rendererID: 'fixture',
    rendererVersion: '1',
    contentType: 'text/plain',
    degradedSources: [],
    stale: false,
  })
  published.publish(build(OUTPUT_A))
  published.publish(build(OUTPUT_B))
  const scope = effectScope()
  const view = scope.run(() => useProfileView(() => PROFILE_ID))!
  view.selectOutput(OUTPUT_A)
  view.reveal()
  expect(view.revealed.value).toBe(true)
  view.selectOutput(OUTPUT_B)
  expect(view.revealed.value).toBe(false)
  view.reveal()
  expect(view.revealed.value).toBe(true)
  published.publish(build(OUTPUT_B, 'replaced'))
  expect(view.revealed.value).toBe(false)
  scope.stop()
  published.clear()
})
