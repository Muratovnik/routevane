import * as v from 'valibot'

import {
  count,
  decode,
  type Decoder,
  fields,
  getJSON,
  jsonObject,
  optionalCount,
  optionalText,
  postJSON,
  text,
  texts,
  timestamp,
} from './http'

// An output binds one list to one format. It owns the subscription and the
// chain of published files, which is why every build and download addresses an
// output rather than a list.
export type Output = {
  id: string
  listID: string
  targetID: string
  deviceID: string
}

export type StoredOutput = Output & {
  latestArtifactID: string
}

export type CreatedOutput = {
  output: Output
}

export type OutputAttempt = {
  status: 'success' | 'failed'
  code: string
  projectedRules: number
  maximumRules: number
  artifactID: string
  completedAt: string
}

// One output as a screen states it. The target may have left the catalog since
// the output was made; the card then carries the stored identity and no kind or
// extension, and the screen says so instead of guessing.
export type OutputArtifact = {
  id: string
  snapshotID: string
  sizeBytes: number
  contentType: string
  contentCreatedAt: string
}

export type OutputCard = {
  id: string
  targetID: string
  deviceID: string
  targetTitle: string
  targetKind: string
  fileExtension: string
  createdAt: string
  latest: OutputArtifact | null
  lastAttempt: OutputAttempt | null
}

export type BuildResult = {
  output: Output
  snapshotID: string
  artifactID: string
  artifactHash: string
  contentCreatedAt: string
  validationStatus: string
  status: string
  ruleCount: number
  partialCoverage: boolean
  partialCoverageCount: number
  rendererID: string
  rendererVersion: string
  contentType: string
  degradedSources: string[]
  subscriptionURL: string
}

export function addOutput(
  listID: string,
  targetID: string,
): Promise<CreatedOutput> {
  return postJSON(
    `/v1/lists/${listID}/outputs`,
    { target_id: targetID },
    parseCreatedOutput,
  )
}

export function buildOutput(outputID: string): Promise<BuildResult> {
  return postJSON(`/v1/outputs/${outputID}/build`, {}, parseBuild)
}

export function setOutputDevice(
  outputID: string,
  deviceID: string,
): Promise<CreatedOutput> {
  return postJSON(
    `/v1/outputs/${outputID}/device`,
    { device_id: deviceID },
    parseCreatedOutput,
  )
}

export function loadOutput(outputID: string): Promise<StoredOutput> {
  return getJSON(`/v1/outputs/${outputID}`, parseStoredOutput)
}

const outputSchema = v.pipe(
  fields({ id: text, list_id: text, target_id: text, device_id: optionalText }),
  v.transform((output): Output => ({
    id: output.id,
    listID: output.list_id,
    targetID: output.target_id,
    deviceID: output.device_id,
  })),
)

// The subscription bearer used to travel back with the output that had just
// been created. A reply that still carries one is refused rather than read, so
// nothing on a screen can start depending on the old envelope again.
const createdOutputSchema = v.pipe(
  jsonObject,
  v.check((record) => !('subscription_url' in record)),
  v.object({ output: outputSchema }),
  v.transform((created): CreatedOutput => ({ output: created.output })),
)

const storedOutputSchema = v.pipe(
  fields({
    id: text,
    list_id: text,
    target_id: text,
    device_id: optionalText,
    latest_artifact_id: optionalText,
  }),
  v.transform((output): StoredOutput => ({
    id: output.id,
    listID: output.list_id,
    targetID: output.target_id,
    deviceID: output.device_id,
    latestArtifactID: output.latest_artifact_id,
  })),
)

// The routing plan is diagnostic material an operator asks the snapshot
// endpoint for. A build reply that carries one is refused, so the plan never
// reaches a screen that did not ask for it.
const buildSnapshotSchema = v.pipe(
  jsonObject,
  v.check((record) => !('routing_plan' in record)),
  v.object({ id: text }),
)

const buildArtifactSchema = fields({
  id: text,
  artifact_hash: text,
  content_created_at: timestamp,
  validation_status: text,
  status: text,
  renderer_id: text,
  renderer_version: text,
  content_type: text,
})

const buildSummarySchema = fields({
  rule_count: count,
  partial_coverage: v.boolean(),
  partial_coverage_count: count,
  degraded_sources: v.optional(texts, []),
  content_created_at: timestamp,
  validation_status: text,
  status: text,
})

const buildSchema = v.pipe(
  fields({
    output: outputSchema,
    snapshot: buildSnapshotSchema,
    artifact: buildArtifactSchema,
    summary: buildSummarySchema,
    subscription_url: optionalText,
  }),
  // The summary a screen prints and the file it links to are one published
  // fact. A reply whose two halves disagree describes no build that happened.
  v.check(
    (build) =>
      build.summary.validation_status === build.artifact.validation_status &&
      build.summary.status === build.artifact.status &&
      build.summary.content_created_at === build.artifact.content_created_at,
  ),
  // A subscription address is a bearer. It is admitted only as this origin's
  // own plain-HTTP subscription path, so a reply can neither point the operator
  // at somebody else's server nor smuggle another scheme past the screen.
  v.check(
    (build) =>
      build.subscription_url === '' ||
      validSubscriptionURL(build.subscription_url),
  ),
  v.transform((build): BuildResult => ({
    output: build.output,
    snapshotID: build.snapshot.id,
    artifactID: build.artifact.id,
    artifactHash: build.artifact.artifact_hash,
    contentCreatedAt: build.artifact.content_created_at,
    validationStatus: build.artifact.validation_status,
    status: build.artifact.status,
    ruleCount: build.summary.rule_count,
    partialCoverage: build.summary.partial_coverage,
    partialCoverageCount: build.summary.partial_coverage_count,
    rendererID: build.artifact.renderer_id,
    rendererVersion: build.artifact.renderer_version,
    contentType: build.artifact.content_type,
    degradedSources: build.summary.degraded_sources,
    subscriptionURL: build.subscription_url,
  })),
)

const cardArtifactSchema = v.pipe(
  fields({
    id: text,
    snapshot_id: text,
    size_bytes: count,
    content_type: text,
    content_created_at: timestamp,
  }),
  v.transform((artifact): OutputArtifact => ({
    id: artifact.id,
    snapshotID: artifact.snapshot_id,
    sizeBytes: artifact.size_bytes,
    contentType: artifact.content_type,
    contentCreatedAt: artifact.content_created_at,
  })),
)

const attemptSchema = v.pipe(
  fields({
    status: v.picklist(['success', 'failed']),
    code: optionalText,
    projected_rules: optionalCount,
    maximum_rules: optionalCount,
    artifact_id: optionalText,
    completed_at: timestamp,
  }),
  // A success names no failure code and points at the file it published; a
  // failure names the code and points at nothing. An attempt claiming both or
  // neither is not an attempt this screen can report.
  v.check((attempt) =>
    attempt.status === 'success'
      ? attempt.code === '' && attempt.artifact_id !== ''
      : attempt.code !== '' && attempt.artifact_id === '',
  ),
  v.transform((attempt): OutputAttempt => ({
    status: attempt.status,
    code: attempt.code,
    projectedRules: attempt.projected_rules,
    maximumRules: attempt.maximum_rules,
    artifactID: attempt.artifact_id,
    completedAt: attempt.completed_at,
  })),
)

// An absent artifact or attempt is a card that honestly has none. A malformed
// one is a reply outside the contract, and it refuses the whole list rather
// than showing a row nobody can trust.
const outputCardSchema = v.pipe(
  fields({
    id: text,
    target_id: text,
    device_id: optionalText,
    target_title: text,
    target_kind: optionalText,
    file_extension: optionalText,
    created_at: timestamp,
    latest: v.nullish(cardArtifactSchema, null),
    last_attempt: v.nullish(attemptSchema, null),
  }),
  v.transform((card): OutputCard => ({
    id: card.id,
    targetID: card.target_id,
    deviceID: card.device_id,
    targetTitle: card.target_title,
    targetKind: card.target_kind,
    fileExtension: card.file_extension,
    createdAt: card.created_at,
    latest: card.latest,
    lastAttempt: card.last_attempt,
  })),
)

export const outputCardsSchema = v.array(outputCardSchema)

const parseCreatedOutput: Decoder<CreatedOutput> = decode(createdOutputSchema)
const parseBuild: Decoder<BuildResult> = decode(buildSchema)
const parseStoredOutput: Decoder<StoredOutput> = decode(storedOutputSchema)

function validSubscriptionURL(value: string): boolean {
  try {
    const url = new URL(value)
    return (
      url.origin === window.location.origin &&
      url.protocol === 'http:' &&
      url.pathname.startsWith('/v1/subscriptions/rv1.')
    )
  } catch {
    return false
  }
}
