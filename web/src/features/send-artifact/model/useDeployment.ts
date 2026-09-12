import { computed, ref, watch } from 'vue'

import {
  applyDeployment,
  createDeploymentAttemptID,
  loadDeploymentAttempt,
  loadDeployableTargets,
  planDeployment,
  type ConnectionRequirements,
  type DeployableTarget,
  type DeployOutcome,
  type DeployPlan,
} from '@/shared/api/deploy'
import { RoutevaneAPIError } from '@/shared/api/http'
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

/**
 * useDeployment applies one published artifact to one destination.
 *
 * The password lives in this model for the length of one attempt and is cleared
 * as soon as the attempt ends: never stored, never in the URL, never kept after
 * a success or a failure. The address, account and interface are different —
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
) => {
  const targets = ref<DeployableTarget[]>([])
  const targetsState = ref<TargetsState>('loading')
  const state = ref<DeploymentState>('idle')
  const plan = ref<DeployPlan | null>(null)
  const outcome = ref<DeployOutcome | null>(null)
  const errorKey = ref<string | null>(null)
  const checkingOutcome = ref(false)
  const currentAttemptID = ref(readRememberedAttempt(artifactID()))
  const addressTouched = ref(false)

  const device = ref(readRemembered('device'))
  const username = ref(readRemembered('username'))
  const password = ref('')
  const interfaceName = ref(readRemembered('interface'))

  watch(device, (value) => remember('device', value))
  watch(username, (value) => remember('username', value))
  watch(interfaceName, (value) => remember('interface', value))

  watch(
    [device, username, password, interfaceName],
    () => {
      if (plan.value?.fqdnChanges === undefined || state.value !== 'planned')
        return
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
  const addressError = computed(() =>
    addressTouched.value && device.value.trim() !== '' && !addressValid.value
      ? 'send.address.invalid'
      : null,
  )
  const canSubmit = computed(() => {
    const needed = requirements.value
    if (
      needed === null ||
      busy.value ||
      state.value === 'outcome_unknown' ||
      !addressValid.value
    )
      return false
    if (needed.needsCredential) {
      if (username.value.trim() === '' || password.value === '') return false
    }
    if (needed.needsInterface && interfaceName.value.trim() === '') return false
    return true
  })

  const initialize = async (): Promise<void> => {
    targetsState.value = 'loading'
    try {
      targets.value = await loadDeployableTargets()
      targetsState.value = target.value === null ? 'unsupported' : 'ready'
      if (targetsState.value === 'ready') await recoverOutcome()
    } catch {
      targetsState.value = 'failed'
    }
  }

  const connection = () => {
    const needed = requirements.value
    return {
      device: device.value.trim(),
      username: needed?.needsCredential === true ? username.value.trim() : '',
      password: needed?.needsCredential === true ? password.value : '',
      interfaceName:
        needed?.needsInterface === true ? interfaceName.value.trim() : '',
    }
  }

  // Review may read the device to preview exact DNS changes; it never applies them.
  const review = async (): Promise<void> => {
    addressTouched.value = true
    if (!canSubmit.value) return
    state.value = 'planning'
    errorKey.value = null
    outcome.value = null
    try {
      plan.value = await planDeployment(artifactID(), connection())
      state.value = 'planned'
    } catch (error) {
      state.value = 'failed'
      plan.value = null
      errorKey.value = messageKey(error)
    }
  }

  const apply = async (): Promise<void> => {
    if (state.value !== 'planned' || !canSubmit.value) return
    state.value = 'applying'
    errorKey.value = null
    const submittedArtifactID = artifactID()
    const attemptID = createDeploymentAttemptID()
    currentAttemptID.value = attemptID
    rememberAttempt(submittedArtifactID, attemptID)
    try {
      consumeOutcome(
        await applyDeployment(submittedArtifactID, connection(), attemptID),
        attemptID,
      )
    } catch (error) {
      if (
        artifactID() !== submittedArtifactID ||
        currentAttemptID.value !== attemptID
      )
        return
      if (
        error instanceof RoutevaneAPIError &&
        ['attempt_id_invalid', 'attempt_mismatch'].includes(error.message)
      ) {
        state.value = 'failed'
        errorKey.value = messageKey(error)
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

  const consumeOutcome = (
    result: DeployOutcome,
    expectedAttemptID: string,
  ): void => {
    if (currentAttemptID.value !== expectedAttemptID) return
    if (
      result.attemptID !== expectedAttemptID ||
      result.artifactID !== artifactID() ||
      result.status === 'outcome_unknown'
    ) {
      state.value = 'outcome_unknown'
      outcome.value = null
      errorKey.value = null
      return
    }
    outcome.value = result
    state.value = result.status === 'succeeded' ? 'applied' : 'failed'
    errorKey.value = result.status === 'succeeded' ? null : outcomeKey(result)
  }

  const checkOutcome = async (): Promise<void> => {
    const attemptID =
      currentAttemptID.value || readRememberedAttempt(artifactID())
    if (attemptID === '') return
    const expectedArtifactID = artifactID()
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
    if (currentAttemptID.value === '') return
    state.value = 'outcome_unknown'
    await checkOutcome()
  }

  const reset = (): void => {
    state.value = 'idle'
    plan.value = null
    outcome.value = null
    errorKey.value = null
    checkingOutcome.value = false
    currentAttemptID.value = readRememberedAttempt(artifactID())
    password.value = ''
  }

  return {
    addressError,
    addressTouched,
    apply,
    busy,
    canSubmit,
    checkOutcome,
    checkingOutcome,
    device,
    errorKey,
    initialize,
    interfaceName,
    outcome,
    password,
    plan,
    requirements,
    recoverOutcome,
    reset,
    review,
    state,
    target,
    targetsState,
    username,
  }
}

const failureCodes = new Set([
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
  if (!(error instanceof RoutevaneAPIError)) return 'error.network'
  if (failureCodes.has(error.message)) return `error.${error.message}`
  if (error.status === 404) return 'error.notFound'
  if (error.status === 409) return 'error.targetChanged'
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

const rememberAttempt = (artifactID: string, attemptID: string): void => {
  try {
    window.sessionStorage.setItem(
      `${ATTEMPT_STORAGE_PREFIX}${artifactID}`,
      attemptID,
    )
  } catch {
    // Recovery survives a response loss when session storage is available.
  }
}
