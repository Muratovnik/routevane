import type { DeviceFailure } from '@/features/devices/model/useDevices'

/**
 * How a refused write reads on the Connections screen. The service states a
 * stable code and, for a refusal about one field, that field (ADR 0050); this
 * decides where the words go, and nothing here looks at what was submitted.
 */

/** The refusal names the address: it is stated at the address field. */
export const addressRefused = (failure: DeviceFailure | null): boolean =>
  failure?.code === 'address_rejected' && failure.field === 'address'

/** The refusal names the password: it is stated at the password field. */
export const passwordRefused = (failure: DeviceFailure | null): boolean =>
  failure?.code === 'credential_invalid' && failure.field === 'credential'

/**
 * The words under the failure notice for a refusal that belongs to no field.
 * A refusal that names a field is stated at that field instead, so it answers
 * null and the notice is not drawn twice.
 */
export const failureBodyKey = (
  failure: DeviceFailure | null,
): string | null => {
  if (addressRefused(failure) || passwordRefused(failure)) return null
  switch (failure?.code) {
    case 'credential_required':
      return 'devices.work.credentialRequired'
    case 'secret_store_unavailable':
      return 'devices.credential.unavailable'
    case 'connection_invalid':
      return 'devices.work.connectionInvalid'
    default:
      return 'devices.work.failed.body'
  }
}
