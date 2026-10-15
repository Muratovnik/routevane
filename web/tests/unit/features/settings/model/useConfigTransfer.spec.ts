import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useConfigTransfer } from '@/features/settings/model/useConfigTransfer'

const fetchMock = vi.fn()
const digest = `sha256:${'b'.repeat(64)}`

const json = (payload: unknown): Response =>
  new Response(JSON.stringify(payload), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })

const previewResponse = (): Response =>
  json({
    digest,
    can_apply: true,
    counts: {
      custom_lists: 1,
      custom_categories: 0,
      custom_sources: 0,
      profiles: 1,
      devices: 0,
      outputs: 0,
    },
    warnings: [],
  })

// A check the service refuses. A transport failure would do too, but it also
// starts the shell's reachability probe, which is another fetch.
const refusedPreview = (): Response =>
  new Response(
    JSON.stringify({
      code: 'config_transfer_duplicate_key',
      error: 'duplicate key',
    }),
    { status: 422, headers: { 'Content-Type': 'application/json' } },
  )

const file = (name: string, body = '{}'): File =>
  new File([body], name, { type: 'application/json' })

const byteFile = (name: string, parts: BlobPart[]): File =>
  new File(parts, name, { type: 'application/json' })

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

describe('useConfigTransfer', () => {
  it('rejects an oversized file before reading its contents', async () => {
    const arrayBuffer = vi.fn(async () => new ArrayBuffer(0))
    const transfer = useConfigTransfer()

    await transfer.choose({
      name: 'large.json',
      size: 64 * 1024 * 1024 + 1,
      arrayBuffer,
    } as unknown as File)

    expect(transfer.failure.value).toBe('fileSize')
    expect(arrayBuffer).not.toHaveBeenCalled()
  })

  it('accepts a file exactly at the 64 MiB boundary', async () => {
    const arrayBuffer = vi.fn(async () => new TextEncoder().encode('{}').buffer)
    fetchMock.mockResolvedValueOnce(previewResponse())
    const transfer = useConfigTransfer()

    await transfer.choose({
      name: 'boundary.json',
      size: 64 * 1024 * 1024,
      arrayBuffer,
    } as unknown as File)

    expect(arrayBuffer).toHaveBeenCalledOnce()
    expect(transfer.state.value).toBe('ready')
    expect(transfer.canApply.value).toBe(true)
  })

  it('checks a chosen file at once and keeps its duplicate keys', async () => {
    const source =
      '{"version":"config-transfer-v1.1","settings":{"refresh_interval":"off","refresh_interval":"daily"}}'
    fetchMock.mockResolvedValueOnce(previewResponse())
    const transfer = useConfigTransfer()

    await transfer.choose(file('duplicate.json', source))

    expect(fetchMock).toHaveBeenCalledOnce()
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/v1/config-transfer/preview',
      expect.objectContaining({ body: source }),
    )
    expect(transfer.state.value).toBe('ready')
    expect(transfer.preview.value?.digest).toBe(digest)
  })

  it('drops the check of a file replaced while it was being checked', async () => {
    let release!: (response: Response) => void
    fetchMock.mockReturnValueOnce(
      new Promise<Response>((resolve) => {
        release = resolve
      }),
    )
    fetchMock.mockResolvedValueOnce(refusedPreview())
    const transfer = useConfigTransfer()

    const first = transfer.choose(file('first.json'))
    await vi.waitFor(() => expect(transfer.state.value).toBe('previewing'))
    await transfer.choose(file('second.json'))
    release(previewResponse())
    await first

    // The answer for the first file arrived last, and it is not the answer
    // for the file on screen: the second file's failed check stands.
    expect(transfer.fileName.value).toBe('second.json')
    expect(transfer.preview.value).toBeNull()
    expect(transfer.failure.value).toBe('preview')
    expect(transfer.canApply.value).toBe(false)
  })

  it('checks the same file again after a failed check', async () => {
    fetchMock.mockResolvedValueOnce(refusedPreview())
    fetchMock.mockResolvedValueOnce(previewResponse())
    const transfer = useConfigTransfer()

    await transfer.choose(file('retry.json'))
    expect(transfer.failure.value).toBe('preview')
    expect(transfer.canApply.value).toBe(false)

    await expect(transfer.previewSelected()).resolves.toBe(true)
    expect(transfer.failure.value).toBe('')
    expect(transfer.canApply.value).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it.each([
    [
      'invalid UTF-8',
      byteFile('invalid.json', [
        new TextEncoder().encode('{"label":"'),
        new Uint8Array([0xff]),
        new TextEncoder().encode('"}'),
      ]),
    ],
    [
      'UTF-8 BOM',
      byteFile('bom.json', [new Uint8Array([0xef, 0xbb, 0xbf]), '{}']),
    ],
  ])('rejects %s without rewriting or previewing it', async (_name, input) => {
    const transfer = useConfigTransfer()

    await transfer.choose(input)

    expect(transfer.failure.value).toBe('fileRead')
    expect(transfer.document.value).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('does not let an apply response for a replaced file become current', async () => {
    fetchMock.mockResolvedValueOnce(previewResponse())
    let release!: (response: Response) => void
    const applying = new Promise<Response>((resolve) => {
      release = resolve
    })
    fetchMock.mockReturnValueOnce(applying)

    fetchMock.mockResolvedValueOnce(previewResponse())

    const transfer = useConfigTransfer()
    await transfer.choose(file('first.json'))
    expect(transfer.canApply.value).toBe(true)
    const apply = transfer.apply()
    await transfer.choose(file('second.json'))
    release(
      json({
        applied: true,
        counts: {
          custom_lists: 1,
          custom_categories: 0,
          custom_sources: 0,
          profiles: 1,
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
