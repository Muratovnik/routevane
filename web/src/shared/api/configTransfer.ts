import * as v from 'valibot'

import {
  decode,
  type Decoder,
  mutationHeaders,
  requestJSON,
  requestFile,
  RoutevaneAPIError,
  text,
} from './http'

export type ConfigTransferCounts = {
  customCategories: number
  customLists: number
  customSources: number
  devices: number
  outputs: number
  routes: number
}

export type ConfigTransferWarning =
  | 'custom_sources_require_recreation'
  | 'devices_require_credentials'
  | 'automatic_delivery_disabled'
  | 'outputs_require_publication'

export type ConfigTransferPreview = {
  canApply: true
  counts: ConfigTransferCounts
  digest: string
  warnings: ConfigTransferWarning[]
}

export type ConfigTransferApplyResult = {
  applied: true
  counts: ConfigTransferCounts
}

export type ConfigTransferExport = { blob: Blob; fileName: string }

const fallbackFileName = 'routevane-config.json'
const configTransferDigestHeader = 'X-Routevane-Transfer-Digest'

export const configTransferMaximumFileBytes = 64 * 1024 * 1024

/**
 * A portable configuration is opaque to the client. The server is the one
 * authority on its version and fields. The browser keeps the exact decoded
 * UTF-8 text so duplicate keys, whitespace, and ordering reach preview and
 * apply unchanged.
 */
declare const configTransferDocument: unique symbol
export type ConfigTransferDocument = string & {
  readonly [configTransferDocument]: true
}

export function requestConfigTransferExport(): Promise<ConfigTransferExport> {
  return requestFile(
    '/v1/config-transfer/export',
    { method: 'GET' },
    async (response) => ({
      blob: await response.blob(),
      fileName: fileNameFromDisposition(
        response.headers.get('Content-Disposition') ?? '',
      ),
    }),
  )
}

export async function downloadConfigTransfer(): Promise<void> {
  const file = await requestConfigTransferExport()
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

export function previewConfigTransfer(
  document: ConfigTransferDocument,
): Promise<ConfigTransferPreview> {
  return requestJSON(
    '/v1/config-transfer/preview',
    { method: 'POST', headers: mutationHeaders, body: document },
    parsePreview,
  )
}

export function applyConfigTransfer(
  previewDigest: string,
  transfer: ConfigTransferDocument,
): Promise<ConfigTransferApplyResult> {
  return requestJSON(
    '/v1/config-transfer/apply',
    {
      method: 'POST',
      headers: {
        ...mutationHeaders,
        [configTransferDigestHeader]: previewDigest,
      },
      body: transfer,
    },
    parseApplyResult,
  )
}

// Content-Disposition is an HTTP header, not a trusted path. The exported
// document is JSON, so any name outside this compact portable-file grammar is
// replaced instead of giving the browser a path, control character or another
// file type to interpret.
function fileNameFromDisposition(disposition: string): string {
  // Prefer RFC 5987's UTF-8 form, then the ordinary filename parameter. The
  // value is still reduced to a basename-like, JSON-only name before it is
  // handed to `download`; a header must never supply a path or control byte.
  const encoded = /(?:^|;)\s*filename\*=(?:UTF-8'')?([^;\r\n]+)/i.exec(
    disposition,
  )?.[1]
  const quoted = /(?:^|;)\s*filename="([^"\r\n]+)"/i.exec(disposition)?.[1]
  const bare = /(?:^|;)\s*filename=([^;\s]+)/i.exec(disposition)?.[1]
  const candidate = (encoded ?? quoted ?? bare ?? '').trim()
  let fileName = candidate
  if (encoded !== undefined) {
    try {
      fileName = decodeURIComponent(candidate)
    } catch {
      return fallbackFileName
    }
  }
  if (
    fileName === '' ||
    fileName.length > 128 ||
    !/\.json$/i.test(fileName) ||
    /[\\/]/.test(fileName) ||
    [...fileName].some((character) => {
      const code = character.charCodeAt(0)
      return code < 0x20 || code === 0x7f
    }) ||
    fileName === '.' ||
    fileName === '..' ||
    fileName.includes('..')
  ) {
    return fallbackFileName
  }
  return fileName
}

const digest = v.pipe(text, v.regex(/^sha256:[a-f0-9]{64}$/))

const countsSchema = v.pipe(
  v.strictObject({
    custom_lists: v.pipe(
      v.number(),
      v.integer(),
      v.minValue(0),
      v.safeInteger(),
    ),
    custom_categories: v.pipe(
      v.number(),
      v.integer(),
      v.minValue(0),
      v.safeInteger(),
    ),
    custom_sources: v.pipe(
      v.number(),
      v.integer(),
      v.minValue(0),
      v.safeInteger(),
    ),
    profiles: v.pipe(v.number(), v.integer(), v.minValue(0), v.safeInteger()),
    devices: v.pipe(v.number(), v.integer(), v.minValue(0), v.safeInteger()),
    outputs: v.pipe(v.number(), v.integer(), v.minValue(0), v.safeInteger()),
  }),
  v.transform((counts): ConfigTransferCounts => ({
    customLists: counts.custom_lists,
    customCategories: counts.custom_categories,
    customSources: counts.custom_sources,
    routes: counts.profiles,
    devices: counts.devices,
    outputs: counts.outputs,
  })),
)

const warningSchema = v.pipe(
  v.strictObject({
    code: v.picklist([
      'devices_require_credentials',
      'automatic_delivery_disabled',
      'outputs_require_publication',
      'custom_sources_require_recreation',
    ]),
  }),
  v.transform((warning): ConfigTransferWarning => warning.code),
)

const warningsSchema = v.pipe(
  v.array(warningSchema),
  v.check(
    (warnings) =>
      new Set(warnings).size === warnings.length &&
      warnings.every(
        (warning, index) =>
          index === 0 ||
          warningOrder(warnings[index - 1]!) < warningOrder(warning),
      ),
  ),
)

const previewSchema = v.pipe(
  v.strictObject({
    digest,
    can_apply: v.literal(true),
    counts: countsSchema,
    warnings: warningsSchema,
  }),
  v.transform((preview): ConfigTransferPreview => ({
    digest: preview.digest,
    canApply: preview.can_apply,
    counts: preview.counts,
    warnings: preview.warnings,
  })),
)

const applyResultSchema = v.pipe(
  v.strictObject({ applied: v.literal(true), counts: countsSchema }),
  v.transform((result): ConfigTransferApplyResult => ({
    applied: result.applied,
    counts: result.counts,
  })),
)

const parsePreview: Decoder<ConfigTransferPreview> = decode(previewSchema)
const parseApplyResult: Decoder<ConfigTransferApplyResult> =
  decode(applyResultSchema)

function warningOrder(warning: ConfigTransferWarning): number {
  return [
    'custom_sources_require_recreation',
    'devices_require_credentials',
    'automatic_delivery_disabled',
    'outputs_require_publication',
  ].indexOf(warning)
}

export function parseConfigTransferDocument(
  textContent: string,
): ConfigTransferDocument {
  let parsed: unknown
  try {
    parsed = JSON.parse(textContent)
  } catch {
    throw new RoutevaneAPIError('invalid config document', 0)
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    throw new RoutevaneAPIError('invalid config document', 0)
  }
  return textContent as ConfigTransferDocument
}
