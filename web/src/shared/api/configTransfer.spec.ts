import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  applyConfigTransfer,
  parseConfigTransferDocument,
  previewConfigTransfer,
  requestConfigTransferExport,
} from './configTransfer'
import { RoutevaneAPIError } from './http'

const fetchMock = vi.fn()
const document = { version: 1, routes: [] }
const digest = `sha256:${'a'.repeat(64)}`

function json(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function counts(): Record<string, number> {
  return {
    custom_lists: 2,
    custom_categories: 1,
    custom_sources: 3,
    routes: 4,
    devices: 5,
    outputs: 6,
  }
}

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

describe('configuration transfer API contract', () => {
  it('downloads the JSON attachment and replaces unsafe server file names', async () => {
    fetchMock.mockResolvedValueOnce(
      new Response('{"version":1}', {
        headers: {
          'Content-Disposition': 'attachment; filename="portable.json"',
          'Content-Type': 'application/json',
        },
      }),
    )
    await expect(requestConfigTransferExport()).resolves.toMatchObject({
      fileName: 'portable.json',
    })
    expect(fetchMock).toHaveBeenLastCalledWith('/v1/config-transfer/export', {
      method: 'GET',
    })

    fetchMock.mockResolvedValueOnce(
      new Response('{"version":1}', {
        headers: {
          'Content-Disposition': 'attachment; filename="../settings.json"',
          'Content-Type': 'application/json',
        },
      }),
    )
    await expect(requestConfigTransferExport()).resolves.toMatchObject({
      fileName: 'routevane-config.json',
    })
  })

  it('binds apply to the strictly decoded preview digest', async () => {
    fetchMock.mockResolvedValueOnce(
      json({
        digest,
        can_apply: true,
        counts: counts(),
        warnings: [
          { code: 'devices_require_credentials' },
          { code: 'automatic_delivery_disabled' },
          { code: 'outputs_require_publication' },
        ],
      }),
    )
    await expect(previewConfigTransfer(document)).resolves.toEqual({
      digest,
      canApply: true,
      counts: {
        customLists: 2,
        customCategories: 1,
        customSources: 3,
        routes: 4,
        devices: 5,
        outputs: 6,
      },
      warnings: [
        'devices_require_credentials',
        'automatic_delivery_disabled',
        'outputs_require_publication',
      ],
    })
    expect(fetchMock).toHaveBeenLastCalledWith('/v1/config-transfer/preview', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Routevane-Request': '1',
      },
      body: JSON.stringify(document),
    })

    fetchMock.mockResolvedValueOnce(json({ applied: true, counts: counts() }))
    await expect(applyConfigTransfer(digest, document)).resolves.toEqual({
      applied: true,
      counts: {
        customLists: 2,
        customCategories: 1,
        customSources: 3,
        routes: 4,
        devices: 5,
        outputs: 6,
      },
    })
    expect(fetchMock).toHaveBeenLastCalledWith('/v1/config-transfer/apply', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Routevane-Request': '1',
      },
      body: JSON.stringify({ preview_digest: digest, transfer: document }),
    })
  })

  it('refuses any response that extends or weakens the frozen schema', async () => {
    const valid = {
      digest,
      can_apply: true,
      counts: counts(),
      warnings: [],
    }
    for (const payload of [
      { ...valid, unexpected: true },
      { ...valid, can_apply: false },
      { ...valid, counts: { ...counts(), routes: -1 } },
      { ...valid, counts: { ...counts(), extra: 1 } },
      { ...valid, warnings: [{ code: 'new_warning' }] },
      {
        ...valid,
        warnings: [
          { code: 'devices_require_credentials', detail: 'do not render' },
        ],
      },
    ]) {
      fetchMock.mockResolvedValueOnce(json(payload))
      await expect(previewConfigTransfer(document)).rejects.toBeInstanceOf(
        RoutevaneAPIError,
      )
    }

    fetchMock.mockResolvedValueOnce(
      json({
        ...valid,
        warnings: [
          { code: 'outputs_require_publication' },
          { code: 'devices_require_credentials' },
        ],
      }),
    )
    await expect(previewConfigTransfer(document)).resolves.toMatchObject({
      warnings: ['outputs_require_publication', 'devices_require_credentials'],
    })
  })

  it('keeps only a JSON object as an import document', () => {
    expect(parseConfigTransferDocument('{"version":1}')).toEqual({ version: 1 })
    for (const source of ['not-json', 'null', '[]', '"text"']) {
      expect(() => parseConfigTransferDocument(source)).toThrow(
        RoutevaneAPIError,
      )
    }
  })
})
