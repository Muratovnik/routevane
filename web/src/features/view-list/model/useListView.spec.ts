import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useListView } from '@/features/view-list/model/useListView'
import { invalidateCatalogCache } from '@/shared/api/catalog'

const listID = 'list-1'
const outputA = 'output-a'
const outputB = 'output-b'
const artifactA = 'artifact-a'
const artifactB = 'artifact-b'

function json(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function outputCard(id: string, artifactID: string, snapshotID: string) {
  return {
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
  }
}

function buildPayload(id: string, artifactID: string) {
  return {
    output: {
      id,
      list_id: listID,
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
  }
}

function listDetailPayload(): unknown {
  return {
    profile: {
      id: listID,
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
      outputCard(outputA, artifactA, 'snapshot-a'),
      outputCard(outputB, artifactB, 'snapshot-b'),
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
  }
}

function emptyCatalogAndDeployables(): void {
  fetchMock.mockResolvedValueOnce(
    json({ lists: [], list_details: [], categories: [] }),
  )
  fetchMock.mockResolvedValueOnce(json({ targets: [] }))
  fetchMock.mockResolvedValueOnce(json({ targets: [] }))
}

/** The four requests `initialize()` fires, in the order it fires them. */
function queueInitializeFetches(list: Response | Error): void {
  if (list instanceof Error) {
    fetchMock.mockRejectedValueOnce(list)
  } else {
    fetchMock.mockResolvedValueOnce(list)
  }
  emptyCatalogAndDeployables()
}

function deferred<T>(): {
  promise: Promise<T>
  resolve: (value: T) => void
} {
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
  // A list the server has genuinely never heard of, versus a service that
  // cannot answer right now, are different facts to an operator: only the
  // first should read "not found" with no way back in. Everything else must
  // land on 'failed', where ListView.vue renders a working retry button.
  it('reads a 404 as the list not existing, not as the service being down', async () => {
    queueInitializeFetches(json({ error: 'list not found' }, 404))
    const view = useListView(() => listID)

    await view.initialize()

    expect(view.state.value).toBe('missing')
  })

  it('reads a 5xx as the service being unavailable, not as the list not existing', async () => {
    queueInitializeFetches(json({ error: 'internal' }, 500))
    const view = useListView(() => listID)

    await view.initialize()

    expect(view.state.value).toBe('failed')
  })

  it('reads a network failure as the service being unavailable', async () => {
    queueInitializeFetches(new TypeError('Failed to fetch'))
    const view = useListView(() => listID)

    await view.initialize()

    expect(view.state.value).toBe('failed')
  })

  it('recovers to ready once the service answers again', async () => {
    queueInitializeFetches(json({ error: 'internal' }, 500))
    const view = useListView(() => listID)
    await view.initialize()
    expect(view.state.value).toBe('failed')

    queueInitializeFetches(json(listDetailPayload()))
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
    queueInitializeFetches(json(listDetailPayload()))
    const view = useListView(() => listID)
    await view.initialize()
    expect(view.selectedOutputID.value).toBe(outputA)

    const staleResponse = deferred<Response>()
    fetchMock.mockImplementationOnce(() => staleResponse.promise)

    const openPromise = view.openContent()
    expect(view.contentState.value).toBe('loading')

    // The operator picks a different output before output A's file arrives.
    view.selectOutput(outputB)
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
    expect(view.selectedOutputID.value).toBe(outputB)
  })

  it('loads content normally for the output currently selected', async () => {
    queueInitializeFetches(json(listDetailPayload()))
    const view = useListView(() => listID)
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
    queueInitializeFetches(json(listDetailPayload()))
    const view = useListView(() => listID)
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
    fetchMock.mockResolvedValueOnce(json(buildPayload(outputB, artifactB)))
    queueInitializeFetches(json(listDetailPayload()))

    await view.rebuild()

    const requested = fetchMock.mock.calls.map(([url]) => String(url))
    expect(requested).toContain(`/v1/outputs/${outputA}/build`)
    expect(requested).toContain(`/v1/outputs/${outputB}/build`)
    expect(view.rebuildFailed.value).toBe(true)
    expect(view.work.value).toBe('failed')
  })
})
