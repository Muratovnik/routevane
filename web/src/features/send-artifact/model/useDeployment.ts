import { computed, ref, watch } from 'vue'

import {
  applyDeployment,
  loadDeployableTargets,
  planDeployment,
  type ConnectionRequirements,
  type DeployableTarget,
  type DeployOutcome,
  type DeployPlan,
} from '@/shared/api/deploy'
import { RoutevaneAPIError } from '@/shared/api/http'

export type DeploymentState =
  'idle' | 'planning' | 'planned' | 'applying' | 'applied' | 'failed'

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
  const addressTouched = ref(false)

  const device = ref(readRemembered('device'))
  const username = ref(readRemembered('username'))
  const password = ref('')
  const interfaceName = ref(readRemembered('interface'))

  watch(device, (value) => remember('device', value))
  watch(username, (value) => remember('username', value))
  watch(interfaceName, (value) => remember('interface', value))

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
    if (needed === null || busy.value || !addressValid.value) return false
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

  // review asks the server what would happen. Nothing is contacted and nothing
  // is changed, so an operator can correct an address before a device is touched.
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
    try {
      const result = await applyDeployment(artifactID(), connection())
      outcome.value = result
      state.value = result.applied ? 'applied' : 'failed'
      errorKey.value = result.applied ? null : outcomeKey(result)
    } catch (error) {
      state.value = 'failed'
      errorKey.value = messageKey(error)
    } finally {
      // The attempt is over, so the password has no reason to still exist.
      password.value = ''
    }
  }

  const reset = (): void => {
    state.value = 'idle'
    plan.value = null
    outcome.value = null
    errorKey.value = null
    password.value = ''
  }

  return {
    addressError,
    addressTouched,
    apply,
    busy,
    canSubmit,
    device,
    errorKey,
    initialize,
    interfaceName,
    outcome,
    password,
    plan,
    requirements,
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
  'deploy_failed',
])

// A destination is whatever its deployer declares: a device on the network, or
// a configuration file on this computer. What this rejects is a malformed
// address, never an address that merely turns out to be unreachable — that
// answer belongs to the destination. The host is read label by label rather
// than by one nested pattern, so no input can make the check backtrack.
const HTTP_SCHEME_PATTERN = /^https?:\/\//i
const HOST_LABEL_PATTERN = /^[a-z\d-]+$/i
const PORT_PATTERN = /^\d{1,5}$/

const validHostLabel = (label: string): boolean =>
  !label.startsWith('-') &&
  !label.endsWith('-') &&
  HOST_LABEL_PATTERN.test(label)

const validHostAndPort = (value: string): boolean => {
  const authority = value.endsWith('/') ? value.slice(0, -1) : value
  const [host, port, ...extra] = authority.split(':')
  if (extra.length > 0 || host === undefined || host === '') return false
  if (!host.split('.').every(validHostLabel)) return false
  if (port === undefined) return true
  return PORT_PATTERN.test(port) && Number(port) >= 1 && Number(port) <= 65535
}

export const validAddress = (value: string): boolean => {
  if (value === '') return false
  // A local configuration file is a legitimate destination, and its own
  // deployer offers it as the example, so the form must accept what it shows.
  if (/^file:/i.test(value)) {
    try {
      return new URL(value).pathname.length > 1
    } catch {
      return false
    }
  }
  if (/^[a-z][a-z\d+.-]*:/i.test(value) && !HTTP_SCHEME_PATTERN.test(value)) {
    return false
  }
  return validHostAndPort(value.replace(HTTP_SCHEME_PATTERN, ''))
}

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
