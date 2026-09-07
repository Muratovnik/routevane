import * as v from 'valibot'

import {
  decode,
  type Decoder,
  fields,
  getJSON,
  mutationHeaders,
  requestFile,
  RoutevaneAPIError,
  text,
} from './http'

export type ExportFormat = {
  id: string
  rendererID: string
  fileExtension: string
  contentType: string
}

export const loadExportFormats = async (): Promise<ExportFormat[]> =>
  getJSON('/v1/export-formats', parseExportFormats)

export const requestProfileExport = async (
  profileID: string,
  formatID: string,
): Promise<{ blob: Blob; fileName: string }> =>
  requestFile(
    `/v1/profiles/${profileID}/export`,
    {
      method: 'POST',
      headers: mutationHeaders,
      body: JSON.stringify({ format_id: formatID }),
    },
    async (response) => {
      const disposition = response.headers.get('Content-Disposition') ?? ''
      const match = /filename="([a-z0-9.-]+)"/i.exec(disposition)
      if (match?.[1] === undefined) {
        throw new RoutevaneAPIError(
          'Сервер не указал имя файла.',
          response.status,
        )
      }
      return { blob: await response.blob(), fileName: match[1] }
    },
  )

export const downloadProfileExport = async (
  profileID: string,
  formatID: string,
): Promise<void> => {
  const file = await requestProfileExport(profileID, formatID)
  const url = URL.createObjectURL(file.blob)
  try {
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = file.fileName
    anchor.hidden = true
    document.body.append(anchor)
    anchor.click()
    anchor.remove()
  } finally {
    URL.revokeObjectURL(url)
  }
}

// An export format is independent of any output: it names the renderer that
// writes it and the file it produces, and a format that cannot state all four
// facts is not offered at all.
const exportFormatSchema = v.pipe(
  fields({
    id: text,
    renderer_id: text,
    file_extension: text,
    content_type: text,
  }),
  v.transform((format): ExportFormat => ({
    id: format.id,
    rendererID: format.renderer_id,
    fileExtension: format.file_extension,
    contentType: format.content_type,
  })),
)

const exportFormatsSchema = v.pipe(
  fields({ formats: v.array(exportFormatSchema) }),
  v.transform((payload): ExportFormat[] => payload.formats),
)

const parseExportFormats: Decoder<ExportFormat[]> = decode(exportFormatsSchema)
