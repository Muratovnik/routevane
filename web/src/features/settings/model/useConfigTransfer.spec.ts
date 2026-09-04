import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useConfigTransfer } from './useConfigTransfer'

const fetchMock = vi.fn()
const digest = `sha256:${'b'.repeat(64)}`

function json(payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

function previewResponse(): Response {
  return json({
    digest,
    can_apply: true,
    counts: {
      custom_lists: 1,
      custom_categories: 0,
      custom_sources: 0,
      routes: 1,
      devices: 0,
      outputs: 0,
    },
    warnings: [],
  })
}

function file(name: string, body = '{}'): File {
  return new File([body], name, { type: 'application/json' })
}

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

describe('useConfigTransfer', () => {
  it('rejects an oversized file before reading its contents', async () => {
    const text = vi.fn(async () => '{}')
    const transfer = useConfigTransfer()

    await transfer.choose({
      name: 'large.json',
      size: 16 * 1024 * 1024 + 1,
      text,
    } as unknown as File)

    expect(transfer.failure.value).toBe('fileSize')
    expect(text).not.toHaveBeenCalled()
  })

  it('does not let an apply response for a replaced file become current', async () => {
    fetchMock.mockResolvedValueOnce(previewResponse())
    let release!: (response: Response) => void
    const applying = new Promise<Response>((resolve) => {
      release = resolve
    })
    fetchMock.mockReturnValueOnce(applying)

    const transfer = useConfigTransfer()
    await transfer.choose(file('first.json'))
    await expect(transfer.previewSelected()).resolves.toBe(true)
    const apply = transfer.apply()
    await transfer.choose(file('second.json'))
    release(
      json({
        applied: true,
        counts: {
          custom_lists: 1,
          custom_categories: 0,
          custom_sources: 0,
          routes: 1,
          devices: 0,
          outputs: 0,
        },
      }),
    )

    await expect(apply).resolves.toBe(false)
    expect(transfer.applied.value).toBeNull()
    expect(transfer.fileName.value).toBe('second.json')
  })
})
