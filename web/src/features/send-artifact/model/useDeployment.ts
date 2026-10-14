import * as v from 'valibot'
import { computed, onScopeDispose, ref, watch } from 'vue'

import { useDeploymentConnections } from './useDeploymentConnections'

import {
  applyDeployment,
  createDeploymentAttemptID,
  deploymentNotStarted,
  loadDeploymentAttempt,
  loadDeployableTargets,
  planDeployment,
  type ConnectionRequirements,
  type DeployableTarget,
  type DeployConnection,
  type DeployOutcome,
  type DeployPlan,
} from '@/shared/api/deploy'
import { saveDeviceCredential } from '@/shared/api/devices'
import {
  decode,
  failureKind,
  fields,
  refusalCode,
  refusalField,
  RoutevaneAPIError,
  text,
  type FailureKind,
} from '@/shared/api/http'
import { validAddress } from '@/shared/lib/deviceAddress'

export type DeploymentState =
  | 'idle'
  | 'planning'
  | 'planned'
  | 'applying'
  | 'applied'
  | 'failed'
  | 'outcome_unknown'

export type TargetsState = 'loading' | 'ready' | 'unsupported' | 'failed'

/** The fields a send can ask for, in the order the form shows them. */
export type SendField = 'address' | 'username' | 'password' | 'interface'
const FIELD_ORDER: SendField[] = [
  'address',
  'username',
  'password',
  'interface',
]

// The access used by one attempt, independent of the next connection selected
// in the form. Only these non-secret fields may survive a reload.
type DeploymentRecipient = Readonly<{
  device: string
  username: string
  interfaceName: string
}>

/**
 * useDeployment applies one published artifact to one destination.
 *
 * The password lives in this model for the length of one attempt and is cleared
 * as soon as the attempt ends: never in browser storage, never in the URL, never
 * kept after a success or a failure. A registered connection may hold a saved
 * password of its own (ADR 0050); the operator saves one here only by asking
 * to, and it then goes to the server once, after a plan or an apply succeeded
 * with it. The address, account and interface are different —
 * they are not secrets, and asking for them again after every build is the
 * redundant entry WCAG 2.2 asks products to stop doing — so they are remembered
 * for the tab and no longer.
 *
 * Failures answer with a message key. A server code the operator cannot act on
 * never reaches the screen as itself.
 */
export const useDeployment = (
  artifactID: () => string,
  targetID: () => string,
  preferredDeviceID: () => string = () => '',
) => {
  const targets = ref<DeployableTarget[]>([])
  const targetsState = ref<TargetsState>('loading')
  const targetsFailure = ref<FailureKind>('unreachable')
  const state = ref<DeploymentState>('idle')
  const plan = ref<DeployPlan | null>(null)
  const outcome = ref<DeployOutcome | null>(null)
  const errorKey = ref<string | null>(null)
  // Whether the last send relied on the connection's saved password, so a
  // refusal of the connection can be told apart from a refusal of what was
  // typed for this send.
  const sentWithSavedPassword = ref(false)
  const checkingOutcome = ref(false)
  const currentAttemptID = ref(readRememberedAttempt(artifactID()))
  const attemptRecipient = ref<DeploymentRecipient | null>(
    readRememberedRecipient(artifactID(), currentAttemptID.value),
  )
  const addressTouched = ref(false)
  // The service refused the address typed for this send and named the field.
  // The words go at the field until the address changes.
  const addressRefused = ref(false)
  // Whether a review was asked for since the form was last reset: from then on
  // every field that stops it says why, beside itself.
  const attempted = ref(false)
  // A saved password is used unless the operator asks to type another one.
  const overridePassword = ref(false)
  const savePassword = ref(false)
  // Why a password asked to be kept was not: the message the failure reads as,
  // or '' while nothing failed.
  const passwordSaveFailure = ref('')
  const passwordSaveFailed = computed(() => passwordSaveFailure.value !== '')
  const connections = useDeploymentConnections(targetID, preferredDeviceID)
  let initializationRequest = 0
  let reviewRequest = 0

  const device = ref(readRemembered('device'))
  const username = ref(readRemembered('username'))
  const password = ref('')
  const interfaceName = ref(readRemembered('interface'))

  watch(device, (value) => remember('device', value))
  // A refusal was about the address as typed; the next one is a new question.
  watch(
    device,
    () => {
      addressRefused.value = false
    },
    { flush: 'sync' },
  )
  watch(username, (value) => remember('username', value))
  watch(interfaceName, (value) => remember('interface', value))

  watch(
    [
      connections.selectedID,
      () => connections.selected.value?.address,
      () => connections.selected.value?.account,
      () => connections.selected.value?.interfaceName,
    ],
    () => {
      password.value = ''
      addressTouched.value = false
      addressRefused.value = false
      attempted.value = false
      overridePassword.value = false
      savePassword.value = false
      passwordSaveFailure.value = ''
    },
    { flush: 'sync' },
  )
  watch(
    [
      device,
      username,
      password,
      interfaceName,
      connections.selectedID,
      connections.selected,
    ],
    () => {
      reviewRequest += 1
      if (!['planned', 'planning'].includes(state.value)) return
      plan.value = null
      state.value = 'idle'
    },
    { flush: 'sync' },
  )

  const target = computed<DeployableTarget | null>(
    () => targets.value.find((entry) => entry.targetID === targetID()) ?? null,
  )
  const requirements = computed<ConnectionRequirements | null>(
    () => target.value?.requirements ?? null,
  )
  const busy = computed(() => ['planning', 'applying'].includes(state.value))
  const addressValid = computed(() => validAddress(device.value.trim()))
  // Whether this send asks for a password: always for entered details, and for
  // a registered connection unless it holds a saved one the operator keeps.
  const passwordNeeded = computed(
    () =>
      requirements.value?.needsCredential === true &&
      (connections.manual.value ||
        !connections.storedCredential.value ||
        overridePassword.value),
  )
  // The message each field states, or null. A required field says so only once
  // a review was asked for; a malformed address says so once it was left.
  const fieldErrors = computed<Record<SendField, string | null>>(() => {
    const needed = requirements.value
    const manual = connections.manual.value
    const address = device.value.trim()
    const required = (missing: boolean): string | null =>
      attempted.value && missing ? 'send.field.required' : null
    let addressMessage: string | null = null
    if (manual && address === '') addressMessage = required(true)
    else if (
      manual &&
      (addressTouched.value || attempted.value) &&
      !addressValid.value
    )
      addressMessage = 'send.address.invalid'
    // The service's own refusal, which owns the policy the shape check leaves
    // out; it never outranks a shape problem the operator has since made.
    else if (manual && addressRefused.value)
      addressMessage = 'send.address.rejected'
    return {
      address: addressMessage,
      username: required(
        manual &&
          needed?.needsCredential === true &&
          username.value.trim() === '',
      ),
      password: required(passwordNeeded.value && password.value === ''),
      interface: required(
        manual &&
          needed?.needsInterface === true &&
          interfaceName.value.trim() === '',
      ),
    }
  })
  const addressError = computed(() => fieldErrors.value.address)
  // Whether a review can be asked for at all. A missing field never makes the
  // button unavailable; it is named beside the field when the button is used.
  const available = computed(() => {
    if (
      requirements.value === null ||
      busy.value ||
      state.value === 'outcome_unknown'
    )
      return false
    return (
      connections.manual.value ||
      (connections.state.value === 'ready' &&
        connections.selected.value !== null)
    )
  })
  const complete = computed(() => {
    const needed = requirements.value
    if (needed === null) return false
    if (passwordNeeded.value && password.value === '') return false
    if (!connections.manual.value) return true
    if (!addressValid.value) return false
    if (needed.needsCredential && username.value.trim() === '') return false
    if (needed.needsInterface && interfaceName.value.trim() === '') return false
    return true
  })
  const canSubmit = computed(() => available.value && complete.value)

  const initialize = async (): Promise<void> => {
    const request = ++initializationRequest
    targetsState.value = 'loading'
    try {
      const [loaded] = await Promise.all([
        loadDeployableTargets(),
        connections.initialize(),
      ])
      if (request !== initializationRequest) return
      targets.value = loaded
      targetsState.value = target.value === null ? 'unsupported' : 'ready'
      if (targetsState.value === 'ready') await recoverOutcome()
    } catch (error) {
      if (request !== initializationRequest) return
      targetsFailure.value = failureKind(error)
      targetsState.value = 'failed'
    }
  }

  const connection = (): DeployConnection => {
    if (!connections.manual.value) {
      const selected = connections.selected.value
      return {
        deviceID: connections.selectedID.value,
        password: password.value,
        expectedConnection: {
          device: selected?.address ?? '',
          username: selected?.account ?? '',
          interfaceName: selected?.interfaceName ?? '',
        },
      }
    }
    const needed = requirements.value
    return {
      device: device.value.trim(),
      username: needed?.needsCredential === true ? username.value.trim() : '',
      password: needed?.needsCredential === true ? password.value : '',
      interfaceName:
        needed?.needsInterface === true ? interfaceName.value.trim() : '',
    }
  }

  // The service refused the address typed for this send and named the field.
  // A saved connection has no typed address, so its refusal is a failure of the
  // connection and reads as one.
  const refusesTypedAddress = (error: unknown): boolean =>
    connections.manual.value &&
    refusalCode(error) === 'address_rejected' &&
    refusalField(error) === 'address'

  const usesSavedPassword = (submitted: DeployConnection): boolean =>
    'deviceID' in submitted &&
    submitted.password === '' &&
    connections.storedCredential.value

  // The connection was refused while it relied on its saved password: the
  // password is what to replace, not the connection's other details.
  const savedPasswordRefused = computed(
    () =>
      sentWithSavedPassword.value &&
      errorKey.value === 'error.connection_invalid',
  )

  // A password the operator asked to keep goes to the connection once an apply
  // has succeeded with it: a review of a router does not contact the device, so
  // it proves nothing about the password. A failure is stated and changes
  // nothing else.
  const persistPassword = async (
    submitted: DeployConnection,
  ): Promise<void> => {
    if (
      !savePassword.value ||
      !('deviceID' in submitted) ||
      submitted.password === ''
    )
      return
    try {
      const written = await saveDeviceCredential(
        submitted.deviceID,
        submitted.password,
      )
      if (written.credentialStored === false) throw new Error('not stored')
      connections.markCredentialStored(submitted.deviceID)
      if (connections.selectedID.value === submitted.deviceID) {
        savePassword.value = false
        passwordSaveFailure.value = ''
      }
    } catch (error) {
      if (connections.selectedID.value === submitted.deviceID)
        passwordSaveFailure.value = passwordSaveFailureKey(error)
    }
  }

  /**
   * Review may read the device to preview exact DNS changes; it never applies
   * them. A field that stops it is named: the answer is the first such field,
   * for the form to move the keyboard to, or null.
   */
  const review = async (): Promise<SendField | null> => {
    addressTouched.value = true
    attempted.value = true
    if (!available.value) return null
    if (!complete.value)
      return (
        FIELD_ORDER.find((field) => fieldErrors.value[field] !== null) ?? null
      )
    // Every field was there; a field emptied later, by the end of an attempt,
    // is not a mistake to report.
    attempted.value = false
    state.value = 'planning'
    errorKey.value = null
    outcome.value = null
    const request = ++reviewRequest
    const submittedArtifactID = artifactID()
    const submittedConnection = connection()
    sentWithSavedPassword.value = usesSavedPassword(submittedConnection)
    try {
      const result = await planDeployment(
        submittedArtifactID,
        submittedConnection,
      )
      if (request !== reviewRequest || artifactID() !== submittedArtifactID)
        return null
      plan.value = result
      state.value = 'planned'
    } catch (error) {
      if (request !== reviewRequest || artifactID() !== submittedArtifactID)
        return null
      plan.value = null
      if (refusesTypedAddress(error)) {
        // The refusal is about a field, so it is stated at the field and the
        // send is simply not started: nothing failed that a notice must carry.
        state.value = 'idle'
        addressRefused.value = true
        return 'address'
      }
      state.value = 'failed'
      errorKey.value = messageKey(error)
    }
    return null
  }

  const apply = async (): Promise<void> => {
    if (state.value !== 'planned' || !canSubmit.value) return
    state.value = 'applying'
    errorKey.value = null
    const submittedArtifactID = artifactID()
    const submittedConnection = connection()
    sentWithSavedPassword.value = usesSavedPassword(submittedConnection)
    const recipient = recipientSnapshot(submittedConnection)
    const attemptID = createDeploymentAttemptID()
    currentAttemptID.value = attemptID
    attemptRecipient.value = recipient
    rememberAttempt(submittedArtifactID, attemptID, recipient)
    try {
      const result = await applyDeployment(
        submittedArtifactID,
        submittedConnection,
        attemptID,
      )
      // Only an attempt this screen accepted as succeeded proves the password.
      if (consumeOutcome(result, attemptID))
        await persistPassword(submittedConnection)
    } catch (error) {
      if (
        artifactID() !== submittedArtifactID ||
        currentAttemptID.value !== attemptID
      )
        return
      if (deploymentNotStarted(error)) {
        forgetAttempt(submittedArtifactID)
        currentAttemptID.value = ''
        attemptRecipient.value = null
        password.value = ''
        plan.value = null
        if (refusesTypedAddress(error)) {
          state.value = 'idle'
          addressRefused.value = true
        } else {
          state.value = 'failed'
          errorKey.value = messageKey(error)
        }
      } else {
        state.value = 'outcome_unknown'
        await checkOutcome()
      }
    } finally {
      // The attempt is over, so the password has no reason to still exist.
      if (
        artifactID() === submittedArtifactID &&
        currentAttemptID.value === attemptID
      )
        password.value = ''
    }
  }

  // Answers whether this screen accepted the result as a succeeded attempt.
  const consumeOutcome = (
    result: DeployOutcome,
    expectedAttemptID: string,
  ): boolean => {
    if (currentAttemptID.value !== expectedAttemptID) return false
    if (
      result.attemptID !== expectedAttemptID ||
      result.artifactID !== artifactID() ||
      result.status === 'outcome_unknown'
    ) {
      state.value = 'outcome_unknown'
      outcome.value = null
      errorKey.value = null
      return false
    }
    outcome.value = result
    state.value = result.status === 'succeeded' ? 'applied' : 'failed'
    errorKey.value = result.status === 'succeeded' ? null : outcomeKey(result)
    return result.status === 'succeeded'
  }

  const checkOutcome = async (): Promise<void> => {
    const attemptID =
      currentAttemptID.value || readRememberedAttempt(artifactID())
    if (attemptID === '') return
    const expectedArtifactID = artifactID()
    if (currentAttemptID.value !== attemptID)
      attemptRecipient.value = readRememberedRecipient(
        expectedArtifactID,
        attemptID,
      )
    currentAttemptID.value = attemptID
    checkingOutcome.value = true
    try {
      const result = await loadDeploymentAttempt(attemptID)
      if (artifactID() !== expectedArtifactID) return
      consumeOutcome(result, attemptID)
    } catch {
      if (
        artifactID() !== expectedArtifactID ||
        currentAttemptID.value !== attemptID
      )
        return
      state.value = 'outcome_unknown'
      errorKey.value = null
    } finally {
      if (
        artifactID() === expectedArtifactID &&
        currentAttemptID.value === attemptID
      )
        checkingOutcome.value = false
    }
  }

  const recoverOutcome = async (): Promise<void> => {
    currentAttemptID.value = readRememberedAttempt(artifactID())
    attemptRecipient.value = readRememberedRecipient(
      artifactID(),
      currentAttemptID.value,
    )
    if (currentAttemptID.value === '') return
    state.value = 'outcome_unknown'
    await checkOutcome()
  }

  const reset = (): void => {
    initializationRequest += 1
    reviewRequest += 1
    state.value = 'idle'
    plan.value = null
    outcome.value = null
    errorKey.value = null
    sentWithSavedPassword.value = false
    checkingOutcome.value = false
    attempted.value = false
    addressRefused.value = false
    overridePassword.value = false
    savePassword.value = false
    passwordSaveFailure.value = ''
    currentAttemptID.value = readRememberedAttempt(artifactID())
    attemptRecipient.value = readRememberedRecipient(
      artifactID(),
      currentAttemptID.value,
    )
    password.value = ''
  }

  onScopeDispose(() => {
    initializationRequest += 1
    reviewRequest += 1
    password.value = ''
  })

  return {
    addressError,
    addressRefused,
    addressTouched,
    apply,
    attemptRecipient,
    available,
    busy,
    canSubmit,
    checkOutcome,
    checkingOutcome,
    connections,
    device,
    errorKey,
    fieldErrors,
    initialize,
    interfaceName,
    outcome,
    overridePassword,
    password,
    passwordNeeded,
    passwordSaveFailed,
    passwordSaveFailure,
    savedPasswordRefused,
    plan,
    requirements,
    recoverOutcome,
    reset,
    review,
    savePassword,
    state,
    target,
    targetsFailure,
    targetsState,
    username,
  }
}

// A password asked to be kept is refused by the registry's own codes (ADR 0050);
// each reads as what the operator can act on, and anything else as the plain
// statement that it was not kept.
const passwordSaveFailureKey = (error: unknown): string => {
  switch (refusalCode(error)) {
    case 'secret_store_unavailable':
      return 'devices.credential.unavailable'
    case 'credential_invalid':
      return 'devices.credential.invalid'
    default:
      return 'send.password.save.failed'
  }
}

const failureCodes = new Set([
  'address_rejected',
  'attempt_mismatch',
  'connection_invalid',
  'device_incompatible',
  'backup_unavailable',
  'verify_failed',
  'rollback_failed',
  'deployer_unavailable',
  'confirmation_required',
  'fqdn_ownership_conflict',
  'deploy_failed',
])

export const messageKey = (error: unknown): string => {
  if (
    !(error instanceof RoutevaneAPIError) ||
    failureKind(error) === 'unreachable'
  )
    return 'failure.unreachable.body'
  if (failureCodes.has(error.message)) return `error.${error.message}`
  if (error.status === 404) return 'error.notFound'
  if (error.status === 503) return 'error.unavailable'
  if (error.status === 400 || error.status === 422) {
    return 'error.operationFailed'
  }
  return 'error.unexpected'
}

const outcomeKey = (result: DeployOutcome): string =>
  failureCodes.has(result.error)
    ? `error.${result.error}`
    : 'error.deploy_failed'

const STORAGE_PREFIX = 'rv.deploy.'
const ATTEMPT_STORAGE_PREFIX = `${STORAGE_PREFIX}attempt.`
const RECIPIENT_STORAGE_PREFIX = `${STORAGE_PREFIX}recipient.`

const recipientSnapshot = (
  connection: DeployConnection,
): DeploymentRecipient => {
  const recipient =
    'deviceID' in connection ? connection.expectedConnection : connection
  return Object.freeze({
    device: recipient.device,
    username: recipient.username,
    interfaceName: recipient.interfaceName,
  })
}

const parseRememberedRecipient = decode(
  fields({
    attemptID: text,
    device: text,
    username: v.string(),
    interfaceName: v.string(),
  }),
)

const readRememberedRecipient = (
  artifactID: string,
  attemptID: string,
): DeploymentRecipient | null => {
  if (attemptID === '') return null
  try {
    const raw = window.sessionStorage.getItem(
      `${RECIPIENT_STORAGE_PREFIX}${artifactID}`,
    )
    const stored = parseRememberedRecipient(JSON.parse(raw ?? 'null'))
    if (stored === null || stored.attemptID !== attemptID) return null
    return Object.freeze({
      device: stored.device,
      username: stored.username,
      interfaceName: stored.interfaceName,
    })
  } catch {
    // Older attempts and unavailable metadata have no known recipient. Never
    // infer their destination from today's registry or the current form.
    return null
  }
}

const readRemembered = (field: string): string => {
  try {
    return window.sessionStorage.getItem(`${STORAGE_PREFIX}${field}`) ?? ''
  } catch {
    return ''
  }
}

const remember = (field: string, value: string): void => {
  try {
    window.sessionStorage.setItem(`${STORAGE_PREFIX}${field}`, value)
  } catch {
    // Convenience only: the operator can always type it again.
  }
}

const readRememberedAttempt = (artifactID: string): string => {
  try {
    return (
      window.sessionStorage.getItem(`${ATTEMPT_STORAGE_PREFIX}${artifactID}`) ??
      ''
    )
  } catch {
    return ''
  }
}

const rememberAttempt = (
  artifactID: string,
  attemptID: string,
  recipient: DeploymentRecipient,
): void => {
  try {
    window.sessionStorage.setItem(
      `${ATTEMPT_STORAGE_PREFIX}${artifactID}`,
      attemptID,
    )
    window.sessionStorage.setItem(
      `${RECIPIENT_STORAGE_PREFIX}${artifactID}`,
      JSON.stringify({
        attemptID,
        device: recipient.device,
        username: recipient.username,
        interfaceName: recipient.interfaceName,
      }),
    )
  } catch {
    // Recovery survives a response loss when session storage is available.
  }
}

const forgetAttempt = (artifactID: string): void => {
  try {
    window.sessionStorage.removeItem(`${ATTEMPT_STORAGE_PREFIX}${artifactID}`)
    window.sessionStorage.removeItem(`${RECIPIENT_STORAGE_PREFIX}${artifactID}`)
  } catch {
    // The in-memory identity is still cleared after a proven refusal.
  }
}
