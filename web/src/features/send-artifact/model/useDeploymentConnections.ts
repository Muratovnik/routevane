import { computed, onScopeDispose, ref, watch } from 'vue'

import { loadDevices, type DeviceCard } from '@/shared/api/devices'

export const MANUAL_CONNECTION = 'manual'

// Selection belongs to this send screen. It never changes an output binding or
// consent, and a registry reply contains metadata only, never a saved password.
export const useDeploymentConnections = (
  targetID: () => string,
  preferredDeviceID: () => string,
) => {
  const devices = ref<DeviceCard[]>([])
  const state = ref<'loading' | 'ready' | 'failed'>('loading')
  const secretStoreAvailable = ref(false)
  const selectedID = ref(MANUAL_CONNECTION)
  let requestVersion = 0
  let selectionVersion = 0
  let initialized = false

  watch(selectedID, () => selectionVersion++, { flush: 'sync' })

  const compatible = computed(() =>
    devices.value.filter(
      (device) => device.deployable && device.targetID === targetID(),
    ),
  )
  const selected = computed(
    () =>
      compatible.value.find((device) => device.id === selectedID.value) ?? null,
  )
  const manual = computed(() => selectedID.value === MANUAL_CONNECTION)
  // A saved password is the connection's own fact (ADR 0050), independent of
  // automatic delivery. A send that names this connection without a password
  // uses it; the server rechecks the fact under its gate. A system without a
  // store cannot hold one, whatever a card says.
  const storedCredential = computed(
    () =>
      selected.value?.credentialStored === true && secretStoreAvailable.value,
  )

  // The server confirmed a password saved for this connection. The card is
  // updated in place rather than read again, so the choice and any plan made
  // for it stay exactly as they are.
  const markCredentialStored = (id: string): void => {
    const device = devices.value.find((candidate) => candidate.id === id)
    if (device !== undefined) device.credentialStored = true
  }
  const preferredUnavailable = computed(
    () =>
      state.value === 'ready' &&
      preferredDeviceID() !== '' &&
      !compatible.value.some((device) => device.id === preferredDeviceID()),
  )

  const initialize = async (): Promise<void> => {
    const request = ++requestVersion
    const requestedTarget = targetID()
    const selectionAtStart = selectionVersion
    state.value = 'loading'
    try {
      const registry = await loadDevices()
      if (request !== requestVersion || requestedTarget !== targetID()) return
      devices.value = registry.devices
      secretStoreAvailable.value = registry.secretStoreAvailable
      if (!initialized && selectionVersion === selectionAtStart) {
        selectedID.value =
          compatible.value.find((device) => device.id === preferredDeviceID())
            ?.id ?? MANUAL_CONNECTION
      }
      initialized = true
      state.value = 'ready'
    } catch {
      if (request !== requestVersion || requestedTarget !== targetID()) return
      // A failed first read leaves manual entry available. Retrying must not
      // replace a manual draft with the preferred registration after the fact.
      initialized = true
      state.value = 'failed'
    }
  }

  onScopeDispose(() => requestVersion++)

  return {
    compatible,
    initialize,
    manual,
    markCredentialStored,
    preferredUnavailable,
    secretStoreAvailable,
    selected,
    selectedID,
    state,
    storedCredential,
  }
}
