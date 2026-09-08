/** What an export format has to state, and what a download has to be named. */
import { beforeEach, describe, expect, it } from 'vitest'

import { loadExportFormats, requestProfileExport } from '@/shared/api/exports'

import { answer, fetchMock, profileID, stubFetch } from './support/fixtures'

beforeEach(stubFetch)

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
