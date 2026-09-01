import * as v from 'valibot'

import {
  count,
  decode,
  type Decoder,
  fields,
  getJSON,
  mutationHeaders,
  optionalText,
  postJSON,
  requestDescribed,
  text,
} from './http'

// A deployable target says what its deployer needs. The form is built from this
// rather than from a list of devices the screen keeps, so a destination that
// takes no credential never asks for one.
export type ConnectionRequirements = {
  addressLabel: string
  addressExample: string
  needsCredential: boolean
  needsInterface: boolean
  interfaceLabel: string
}

export type DeployableTarget = {
  targetID: string
  title: string
  deployerID: string
  requirements: ConnectionRequirements
}

export type DeployConnection = {
  device: string
  username: string
  password: string
  interfaceName: string
}

export type DeployPlan = {
  artifactID: string
  artifactHash: string
  targetID: string
  title: string
  deployerID: string
  sizeBytes: number
}

export type DeployEvent = {
  step: string
  outcome: string
  detail: string
}

export type DeployOutcome = {
  applied: boolean
  rolledBack: boolean
  deployerID: string
  vendor: string
  firmwareVersion: string
  interfaceName: string
  backupHash: string
  events: DeployEvent[]
  error: string
}

export function loadDeployableTargets(): Promise<DeployableTarget[]> {
  return getJSON('/v1/deployments/targets', parseDeployableTargets)
}

// planDeployment asks what a deployment would do without contacting the device.
// The credential is sent because the deployer validates it, and it is never
// stored by this module.
export function planDeployment(
  artifactID: string,
  connection: DeployConnection,
): Promise<DeployPlan> {
  return postJSON(
    `/v1/artifacts/${artifactID}/deploy`,
    deployBody(connection, false),
    parseDeployPlan,
  )
}

// applyDeployment keeps the audit trail on failure. A refused or rolled-back
// deployment answers with a non-2xx status and a body that still describes every
// step, and that record is the most useful thing an operator can be shown.
export async function applyDeployment(
  artifactID: string,
  connection: DeployConnection,
): Promise<DeployOutcome> {
  return requestDescribed(
    `/v1/artifacts/${artifactID}/deploy`,
    {
      method: 'POST',
      headers: mutationHeaders,
      body: JSON.stringify(deployBody(connection, true)),
    },
    parseDeployOutcome,
    'Применение не выполнено.',
  )
}

function deployBody(
  connection: DeployConnection,
  confirm: boolean,
): Record<string, unknown> {
  return {
    device: connection.device,
    username: connection.username,
    password: connection.password,
    interface: connection.interfaceName,
    confirm,
  }
}

const requirementsSchema = v.pipe(
  fields({
    address_label: text,
    address_example: text,
    needs_credential: v.boolean(),
    needs_interface: v.boolean(),
    interface_label: optionalText,
  }),
  // The interface label is only meaningful when an interface is required, and
  // a form that must ask for one cannot ask for it unlabelled.
  v.check(
    (requirements) =>
      !requirements.needs_interface || requirements.interface_label !== '',
  ),
  v.transform((requirements): ConnectionRequirements => ({
    addressLabel: requirements.address_label,
    addressExample: requirements.address_example,
    needsCredential: requirements.needs_credential,
    needsInterface: requirements.needs_interface,
    interfaceLabel: requirements.interface_label,
  })),
)

const deployableTargetSchema = v.pipe(
  fields({
    target_id: text,
    title: text,
    deployer_id: text,
    requirements: requirementsSchema,
  }),
  v.transform((target): DeployableTarget => ({
    targetID: target.target_id,
    title: target.title,
    deployerID: target.deployer_id,
    requirements: target.requirements,
  })),
)

const deployableTargetsSchema = v.pipe(
  fields({ targets: v.array(deployableTargetSchema) }),
  v.transform((payload): DeployableTarget[] => payload.targets),
)

const deployPlanSchema = v.pipe(
  fields({
    plan: fields({
      artifact_id: text,
      artifact_hash: text,
      target_id: text,
      title: text,
      deployer_id: text,
      size_bytes: count,
    }),
  }),
  v.transform((envelope): DeployPlan => ({
    artifactID: envelope.plan.artifact_id,
    artifactHash: envelope.plan.artifact_hash,
    targetID: envelope.plan.target_id,
    title: envelope.plan.title,
    deployerID: envelope.plan.deployer_id,
    sizeBytes: envelope.plan.size_bytes,
  })),
)

const deployEventSchema = fields({
  step: text,
  outcome: text,
  detail: optionalText,
})

// A reply that states no steps at all is not malformed: a deployment refused
// before it started took none. A step that does not say what it was is
// malformed, and it refuses the whole outcome.
const deployEventsSchema = v.optional(
  v.union([
    v.array(deployEventSchema),
    v.pipe(
      v.custom<unknown>((value) => !Array.isArray(value)),
      v.transform((): DeployEvent[] => []),
    ),
  ]),
  [],
)

const deployOutcomeSchema = v.pipe(
  fields({
    result: fields({
      applied: v.boolean(),
      rolled_back: v.boolean(),
      // The device identifies itself as far as it can. Which of its facts the
      // deployer learned depends on the device, so each is optional while the
      // block itself is not.
      device: fields({
        deployer_id: optionalText,
        vendor: optionalText,
        firmware_version: optionalText,
        interface: optionalText,
      }),
      backup: v.fallback(
        v.pipe(
          fields({ hash: text }),
          v.transform((backup): string => backup.hash),
        ),
        '',
      ),
      events: deployEventsSchema,
    }),
    error: optionalText,
  }),
  v.transform((payload): DeployOutcome => ({
    applied: payload.result.applied,
    rolledBack: payload.result.rolled_back,
    deployerID: payload.result.device.deployer_id,
    vendor: payload.result.device.vendor,
    firmwareVersion: payload.result.device.firmware_version,
    interfaceName: payload.result.device.interface,
    backupHash: payload.result.backup,
    events: payload.result.events,
    error: payload.error,
  })),
)

const parseDeployableTargets: Decoder<DeployableTarget[]> = decode(
  deployableTargetsSchema,
)
const parseDeployPlan: Decoder<DeployPlan> = decode(deployPlanSchema)
const parseDeployOutcome: Decoder<DeployOutcome> = decode(deployOutcomeSchema)
