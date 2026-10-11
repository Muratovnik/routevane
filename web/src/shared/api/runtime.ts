import * as v from 'valibot'

import { decode, fields, requestJSON, text } from './http'

const VERSION_READ_BUDGET_MILLISECONDS = 4000

const parseVersion = decode(
  v.pipe(
    fields({ status: v.literal('ok'), version: text }),
    v.transform((runtime) => runtime.version),
  ),
)

export const loadRuntimeVersion = (): Promise<string> =>
  requestJSON(
    '/health',
    {
      cache: 'no-store',
      method: 'GET',
      signal: AbortSignal.timeout(VERSION_READ_BUDGET_MILLISECONDS),
    },
    parseVersion,
  )
