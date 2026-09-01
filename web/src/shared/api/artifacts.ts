import * as v from 'valibot'

import {
  decode,
  type Decoder,
  fields,
  getJSON,
  RoutevaneAPIError,
  text,
  texts,
} from './http'

// The published file as bytes the operator can read before trusting it. The
// server serves it as an attachment; reading it here is what lets the screen
// show the same bytes the device will receive.
export type ArtifactContent = {
  text: string
  contentType: string
  sizeBytes: number
}

export type DiagnosticRule = {
  serviceID: string
  value: string
  reasons: string[]
  excluded: boolean
}

export function loadDiagnostics(snapshotID: string): Promise<DiagnosticRule[]> {
  return getJSON(`/v1/snapshots/${snapshotID}`, parseDiagnostics)
}

// loadArtifactContent reads the published file itself. It is text because the
// operator is going to look at it, and its size comes from the bytes rather than
// from a claim about them.
export async function loadArtifactContent(
  artifactID: string,
): Promise<ArtifactContent> {
  const response = await fetch(`/v1/artifacts/${artifactID}`, {
    headers: { Accept: 'text/plain, */*' },
    method: 'GET',
  })
  if (!response.ok) {
    throw new RoutevaneAPIError('Файл не прочитан.', response.status)
  }
  const content = await response.text()
  return {
    contentType: response.headers.get('Content-Type') ?? '',
    sizeBytes: new TextEncoder().encode(content).length,
    text: content,
  }
}

// A rule the plan kept states its own reasons. An excluded candidate states
// them one level up, because the reason belongs to the exclusion rather than to
// the destination it was made about.
const ruleSchema = v.pipe(
  fields({ service_id: text, value: text, reason_codes: texts }),
  v.transform((rule): DiagnosticRule => ({
    serviceID: rule.service_id,
    value: rule.value,
    reasons: rule.reason_codes,
    excluded: false,
  })),
)

const excludedSchema = v.pipe(
  fields({
    candidate: fields({ service_id: text, value: text }),
    reason_codes: texts,
  }),
  v.transform((entry): DiagnosticRule => ({
    serviceID: entry.candidate.service_id,
    value: entry.candidate.value,
    reasons: entry.reason_codes,
    excluded: true,
  })),
)

const diagnosticsSchema = v.pipe(
  fields({
    routing_plan: fields({
      rules: v.array(ruleSchema),
      excluded: v.array(excludedSchema),
    }),
  }),
  v.transform((snapshot): DiagnosticRule[] => [
    ...snapshot.routing_plan.rules,
    ...snapshot.routing_plan.excluded,
  ]),
)

const parseDiagnostics: Decoder<DiagnosticRule[]> = decode(diagnosticsSchema)
