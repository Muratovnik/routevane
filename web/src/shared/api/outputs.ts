import * as v from 'valibot'
import { retirementBody } from './deviceObjects'
import { servingOrigin } from './desktop'

import {
  count,
  decode,
  type Decoder,
  fields,
  getJSON,
  mutationHeaders,
  requestJSON,
  jsonObject,
  optionalCount,
  optionalFlag,
  optionalText,
  optionalTimestamp,
  postJSON,
  text,
  texts,
  timestamp,
} from './http'

// An output binds one profile to one format. It owns the subscription and the
// chain of published files, which is why every build and download addresses an
// output rather than a profile.
export type Output = {
  id: string
  profileID: string
  targetID: string
  deviceID: string
  fqdnGroupPrefix?: string
  // Whether the file keeps the addresses a name already covers (ADR 0057).
  keepAddresses: boolean
}

export type StoredOutput = Output & {
  latestArtifactID: string
}

export type CreatedOutput = {
  output: Output
}

// A list a build could not cover, by identity and the title it had then.
export type UncoveredList = {
  id: string
  title: string
}

export type OutputAttempt = {
  status: 'success' | 'failed'
  code: string
  projectedRules: number
  maximumRules: number
  // The lists a partial_coverage failure names, sorted by identity; empty for
  // every other attempt.
  uncoveredLists: UncoveredList[]
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
  // When this file became the format's current one; empty from a server that
  // does not report it.
  builtAt: string
}

// The last time this format's file reached a saved connection, whichever
// artifact it was. A send to an address typed by hand names no connection and
// is never reported here.
export type OutputDelivery = {
  deviceID: string
  artifactID: string
  completedAt: string
}

// A send to a saved connection that failed after the latest successful one:
// the stable code it answered with and the step it stopped at.
export type OutputDeliveryFailure = {
  deviceID: string
  code: string
  step: string
  completedAt: string
}

export type OutputCard = {
  id: string
  targetID: string
  deviceID: string
  fqdnGroupPrefix?: string
  keepAddresses: boolean
  // Whether the format can hold the setting; the screen offers it only there.
  keepAddressesAvailable: boolean
  targetTitle: string
  targetKind: string
  fileExtension: string
  createdAt: string
  latest: OutputArtifact | null
  lastAttempt: OutputAttempt | null
  lastDelivery: OutputDelivery | null
  // The bound connection's name, empty when the format is bound to none.
  deviceName: string
  // The newest successful send of any file of this format to any saved
  // connection, and a failed send newer than it.
  latestDelivery: OutputDelivery | null
  latestFailure: OutputDeliveryFailure | null
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

export const addOutput = (
  profileID: string,
  targetID: string,
): Promise<CreatedOutput> =>
  postJSON(
    `/v1/profiles/${profileID}/outputs`,
    { target_id: targetID },
    parseCreatedOutput,
  )

export const buildOutput = (outputID: string): Promise<BuildResult> =>
  postJSON(`/v1/outputs/${outputID}/build`, {}, parseBuild)

export const setOutputDevice = (
  outputID: string,
  deviceID: string,
): Promise<CreatedOutput> =>
  postJSON(
    `/v1/outputs/${outputID}/device`,
    { device_id: deviceID },
    parseCreatedOutput,
  )

export const loadOutput = (outputID: string): Promise<StoredOutput> =>
  getJSON(`/v1/outputs/${outputID}`, parseStoredOutput)

export const setOutputFQDNPrefix = (
  outputID: string,
  prefix: string,
): Promise<CreatedOutput> =>
  requestJSON(
    `/v1/outputs/${outputID}/fqdn-prefix`,
    {
      method: 'PUT',
      headers: mutationHeaders,
      body: JSON.stringify({ fqdn_group_prefix: prefix }),
    },
    parseCreatedOutput,
  )

export const setOutputKeepAddresses = (
  outputID: string,
  keep: boolean,
): Promise<CreatedOutput> =>
  requestJSON(
    `/v1/outputs/${outputID}/keep-addresses`,
    {
      method: 'PUT',
      headers: mutationHeaders,
      body: JSON.stringify({ keep_addresses: keep }),
    },
    parseCreatedOutput,
  )

export type RemovedOutput = {
  id: string
  removedAt: string
}

// Retiring a format is permanent (ADR 0051): its links stop answering, its
// files stay where they are, and nothing on a router is touched.
export const removeOutput = (
  outputID: string,
  removeFromDevices = false,
): Promise<RemovedOutput> =>
  postJSON(
    `/v1/outputs/${outputID}/remove`,
    retirementBody(removeFromDevices),
    parseRemovedOutput,
  )

// A reissued link replaces the active one at once. The new address is a bearer
// shown exactly once, held in memory by the caller and never read back.
export const reissueSubscription = (outputID: string): Promise<string> =>
  postJSON(
    `/v1/outputs/${outputID}/subscription/reissue`,
    {},
    parseReissuedSubscription,
  )

const outputSchema = v.pipe(
  fields({
    id: text,
    list_id: text,
    target_id: text,
    device_id: optionalText,
    fqdn_group_prefix: optionalText,
    keep_addresses: optionalFlag,
  }),
  v.transform((output): Output => ({
    id: output.id,
    profileID: output.list_id,
    targetID: output.target_id,
    deviceID: output.device_id,
    fqdnGroupPrefix: output.fqdn_group_prefix,
    keepAddresses: output.keep_addresses,
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
    fqdn_group_prefix: optionalText,
    keep_addresses: optionalFlag,
    latest_artifact_id: optionalText,
  }),
  v.transform((output): StoredOutput => ({
    id: output.id,
    profileID: output.list_id,
    targetID: output.target_id,
    deviceID: output.device_id,
    fqdnGroupPrefix: output.fqdn_group_prefix,
    keepAddresses: output.keep_addresses,
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
    built_at: optionalTimestamp,
  }),
  v.transform((artifact): OutputArtifact => ({
    id: artifact.id,
    snapshotID: artifact.snapshot_id,
    sizeBytes: artifact.size_bytes,
    contentType: artifact.content_type,
    contentCreatedAt: artifact.content_created_at,
    builtAt: artifact.built_at,
  })),
)

const attemptSchema = v.pipe(
  fields({
    status: v.picklist(['success', 'failed']),
    code: optionalText,
    projected_rules: optionalCount,
    maximum_rules: optionalCount,
    uncovered_lists: v.optional(v.array(fields({ id: text, title: text })), []),
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
    uncoveredLists: attempt.uncovered_lists,
    artifactID: attempt.artifact_id,
    completedAt: attempt.completed_at,
  })),
)

const deliverySchema = v.pipe(
  fields({
    device_id: text,
    artifact_id: text,
    completed_at: timestamp,
  }),
  v.transform((delivery): OutputDelivery => ({
    deviceID: delivery.device_id,
    artifactID: delivery.artifact_id,
    completedAt: delivery.completed_at,
  })),
)

const deliveryFailureSchema = v.pipe(
  fields({
    device_id: text,
    code: text,
    step: optionalText,
    completed_at: timestamp,
  }),
  v.transform((failure): OutputDeliveryFailure => ({
    deviceID: failure.device_id,
    code: failure.code,
    step: failure.step,
    completedAt: failure.completed_at,
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
    fqdn_group_prefix: optionalText,
    keep_addresses: optionalFlag,
    keep_addresses_available: optionalFlag,
    target_title: text,
    target_kind: optionalText,
    file_extension: optionalText,
    created_at: timestamp,
    latest: v.nullish(cardArtifactSchema, null),
    last_attempt: v.nullish(attemptSchema, null),
    last_delivery: v.nullish(deliverySchema, null),
    device_name: optionalText,
    latest_delivery: v.nullish(deliverySchema, null),
    latest_failure: v.nullish(deliveryFailureSchema, null),
  }),
  v.transform((card): OutputCard => ({
    id: card.id,
    targetID: card.target_id,
    deviceID: card.device_id,
    fqdnGroupPrefix: card.fqdn_group_prefix,
    keepAddresses: card.keep_addresses,
    keepAddressesAvailable: card.keep_addresses_available,
    targetTitle: card.target_title,
    targetKind: card.target_kind,
    fileExtension: card.file_extension,
    createdAt: card.created_at,
    latest: card.latest,
    lastAttempt: card.last_attempt,
    lastDelivery: card.last_delivery,
    deviceName: card.device_name,
    latestDelivery: card.latest_delivery,
    latestFailure: card.latest_failure,
  })),
)

export const outputCardsSchema = v.array(outputCardSchema)

const parseCreatedOutput: Decoder<CreatedOutput> = decode(createdOutputSchema)
const parseBuild: Decoder<BuildResult> = decode(buildSchema)
const parseStoredOutput: Decoder<StoredOutput> = decode(storedOutputSchema)
const parseRemovedOutput: Decoder<RemovedOutput> = decode(
  v.pipe(
    fields({ id: text, removed_at: timestamp }),
    v.transform((removed): RemovedOutput => ({
      id: removed.id,
      removedAt: removed.removed_at,
    })),
  ),
)
// The same admission a first build's link passes: this origin's own plain-HTTP
// subscription path, and nothing else.
const parseReissuedSubscription: Decoder<string> = decode(
  v.pipe(
    fields({ subscription_url: text }),
    v.check((reply) => validSubscriptionURL(reply.subscription_url)),
    v.transform((reply) => reply.subscription_url),
  ),
)

const validSubscriptionURL = (value: string): boolean => {
  try {
    const url = new URL(value)
    return (
      url.origin === servingOrigin() &&
      url.protocol === 'http:' &&
      url.pathname.startsWith('/v1/subscriptions/rv1.')
    )
  } catch {
    return false
  }
}
