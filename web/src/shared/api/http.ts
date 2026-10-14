import * as v from 'valibot'

import {
  reportReachable,
  reportTransportFailure,
} from '@/shared/model/useServiceHealth'

export class RoutevaneAPIError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code = '',
    readonly details: Readonly<Record<string, number>> = {},
    // The refused body as it arrived. `details` carries the counts every
    // refusal may state; a refusal that names objects instead — the routes
    // still holding a category, say — is a shape this module does not own, so
    // it travels intact and is read by the endpoint that declared it.
    readonly payload: unknown = null,
  ) {
    super(message)
    this.name = 'RoutevaneAPIError'
  }
}

/**
 * What a failed request tells the operator to do. `unreachable` means nothing
 * answered, so the service has to be started or reached; `refused` means the
 * service answered with an error status, so it is running and the same request
 * is worth repeating. Saying the first when the second is true sends the
 * operator to check a process that is up.
 */
export type FailureKind = 'unreachable' | 'refused'

// The desktop transport answers 503 with no body when its backend never
// replied; a status line with a body is the service's own.
const TRANSPORT_SILENCE = 503

export const failureKind = (error: unknown): FailureKind => {
  if (!(error instanceof RoutevaneAPIError)) return 'unreachable'
  return error.status === TRANSPORT_SILENCE && error.payload === null
    ? 'unreachable'
    : 'refused'
}

export const mutationHeaders = {
  'Content-Type': 'application/json',
  'X-Routevane-Request': '1',
}

/**
 * The one place this surface reaches the network, so one place can tell whether
 * the service is answering at all. Any reply is proof that it is — a refusal
 * included, because a refusal is an answer. A transport failure means no reply
 * was received; a write may already have reached the service and taken effect.
 *
 * The failure travels on exactly as it arrived. Each endpoint below turns what
 * it catches into the message it owns, and this is a report to the surface, not
 * a translation for the caller.
 */
export const request = async (
  path: string,
  init: RequestInit = {},
): Promise<Response> => {
  try {
    const response = await fetch(path, init)
    reportReachable()
    return response
  } catch (error) {
    reportTransportFailure()
    throw error
  }
}

// A public payload is admitted by validation, never by assertion: the screen
// has to be able to refuse a reply that is outside the contract, and a type
// assertion can refuse nothing. Every shape below is stated once, here or
// beside the endpoint that reads it, and nothing reaches a screen unvalidated.

// An empty string is how this API says "absent", so a stated value is never
// empty.
export const text = v.pipe(v.string(), v.minLength(1))

export const texts = v.array(text)

// A count a browser can hold and print: whole, never negative, and never past
// the point where integers stop being exact.
export const count = v.pipe(
  v.number(),
  v.integer(),
  v.minValue(0),
  v.safeInteger(),
)

// A moment kept in the server's own spelling. It is admitted by being placeable
// on a clock and stored as it arrived, so what round-trips is what was
// published rather than this tab's rendering of it.
export const timestamp = v.pipe(
  text,
  v.check((value) => !Number.isNaN(Date.parse(value))),
)

// Fields the server may leave out. An absent value is a fact about the server's
// state rather than a malformed reply: an output whose target left the catalog
// carries no kind, and a profile that has never refreshed carries no refresh
// time.
export const optionalText = v.fallback(text, '')
export const optionalTimestamp = v.fallback(timestamp, '')
export const optionalCount = v.fallback(count, 0)
export const optionalFlag = v.fallback(v.boolean(), false)

// A JSON object, and not an array. `v.object` alone admits an array, and an
// array is never a record in this API.
export const jsonObject = v.custom<Record<string, unknown>>(
  (value) =>
    typeof value === 'object' && value !== null && !Array.isArray(value),
)

export const fields = <const TEntries extends v.ObjectEntries>(
  entries: TEntries,
) => v.pipe(jsonObject, v.object(entries))

// Some endpoints answer with nothing but the fact that they answered.
export const acknowledged = v.pipe(
  jsonObject,
  v.transform((): true => true),
)

// A decoder answers null for a payload outside the contract. The caller turns
// that into the message its own endpoint owns; a validator's issue text is
// never shown to an operator and never leaves this module.
export type Decoder<T> = (value: unknown) => T | null

export const decode =
  <TSchema extends v.GenericSchema>(
    schema: TSchema,
  ): Decoder<v.InferOutput<TSchema>> =>
  (value) => {
    const result = v.safeParse(schema, value)
    return result.success ? result.output : null
  }

const readRecord = decode(jsonObject)
const readCount = decode(count)
const readCode = decode(optionalText)
const readMessage = decode(
  v.pipe(
    fields({ error: text }),
    v.transform((record) => record.error),
  ),
)

export const getJSON = async <T>(
  path: string,
  decoder: Decoder<T>,
): Promise<T> => requestJSON(path, { method: 'GET' }, decoder)

export const postJSON = async <T>(
  path: string,
  body: unknown,
  decoder: Decoder<T>,
): Promise<T> =>
  requestJSON(
    path,
    { method: 'POST', headers: mutationHeaders, body: JSON.stringify(body) },
    decoder,
  )

// A mutation whose success is the status line and nothing else: a 204 carries
// no body at all, and reading one as JSON would turn a completed removal into a
// contract failure. The refusal path is the shared one, so a refused removal
// still states the server's own words and hands its body to the caller.
export const postNoContent = async (
  path: string,
  body: unknown,
): Promise<void> => {
  const response = await request(path, {
    method: 'POST',
    headers: mutationHeaders,
    body: JSON.stringify(body),
  })
  if (response.ok) return
  throw refusal(await refusalPayload(response), response.status)
}

export const requestJSON = async <T>(
  path: string,
  init: RequestInit,
  decoder: Decoder<T>,
): Promise<T> => {
  const response = await request(path, init)
  const payload = await readPayload(response)
  if (!response.ok) throw refusal(payload, response.status)
  return contracted(decoder(payload), response.status)
}

// The endpoints whose reply describes what happened whatever the status line
// says. A refused or rolled-back deployment still names every step it took, and
// that record is the most useful thing an operator can be shown, so the body is
// read before the status and only a body that is not the answer becomes an
// error.
export const requestDescribed = async <T>(
  path: string,
  init: RequestInit,
  decoder: Decoder<T>,
  refused: string,
): Promise<T> => {
  const response = await request(path, init)
  const payload = await readPayload(response)
  const decoded = decoder(payload)
  if (decoded === null) {
    throw new RoutevaneAPIError(
      readError(payload) ?? refused,
      response.status,
      '',
      {},
      payload,
    )
  }
  return decoded
}

// Not every reply is JSON: an export answers with bytes and a file name, so the
// caller reads the response itself. The refusal path stays here, which is why a
// failed download reports the server's own words like every other endpoint.
export const requestFile = async <T>(
  path: string,
  init: RequestInit,
  read: (response: Response) => Promise<T>,
): Promise<T> => {
  const response = await request(path, init)
  if (!response.ok) {
    throw new RoutevaneAPIError(
      readError(await refusalPayload(response)) ?? 'Операция не выполнена.',
      response.status,
    )
  }
  return read(response)
}

export const readError = (value: unknown): string | null => readMessage(value)

// The stable code a refusal carries. The service states it in one of two
// places: beside the message in `code`, or as the message itself, which is how
// the device and deployment routes answer. A message in words is no code, so
// only a snake_case token counts, and an answer that states neither is ''.
const STABLE_CODE = /^[a-z][a-z_]*$/

export const refusalCode = (error: unknown): string => {
  if (!(error instanceof RoutevaneAPIError)) return ''
  if (error.code !== '') return error.code
  return STABLE_CODE.test(error.message) ? error.message : ''
}

const readField = decode(
  v.pipe(
    fields({ field: text }),
    v.transform((record) => record.field),
  ),
)

// The field a refusal is about, when the service names one; '' otherwise. The
// value is the service's own vocabulary (`address`, `credential`), never text
// the operator typed.
export const refusalField = (error: unknown): string =>
  error instanceof RoutevaneAPIError ? (readField(error.payload) ?? '') : ''

// A refused reply is read as far as it can be read. Not every refusal answers
// in JSON — a proxy or a crash may answer in anything — and a bounded generic
// message beats leaking a raw response, so an unreadable body becomes no body.
const refusalPayload = async (response: Response): Promise<unknown> => {
  try {
    return await response.json()
  } catch {
    return null
  }
}

const readPayload = async (response: Response): Promise<unknown> => {
  try {
    return await response.json()
  } catch {
    throw new RoutevaneAPIError(
      'Сервер вернул некорректный ответ.',
      response.status,
    )
  }
}

const contracted = <T>(decoded: T | null, status: number): T => {
  if (decoded === null) {
    throw new RoutevaneAPIError('Сервер вернул ответ вне контракта.', status)
  }
  return decoded
}

// A refusal describes itself as far as it can: its own code, and the two counts
// a rule-limit refusal carries. Each is read on its own, so a body that states
// one of them badly still delivers the rest.
const refusal = (payload: unknown, status: number): RoutevaneAPIError => {
  const record = readRecord(payload)
  const details: Record<string, number> = {}
  for (const key of ['projected_rules', 'maximum_rules']) {
    const parsed = record === null ? null : readCount(record[key])
    if (parsed !== null) details[key] = parsed
  }
  return new RoutevaneAPIError(
    readError(payload) ?? 'Операция не выполнена.',
    status,
    record === null ? '' : (readCode(record.code) ?? ''),
    details,
    payload,
  )
}
