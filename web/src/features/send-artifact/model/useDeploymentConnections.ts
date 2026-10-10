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
  // This says that the UI may request the existing consented credential. The
  // server rechecks consent and availability for the exact access under its gate.
  const storedCredential = computed(
    () => selected.value?.autoDeliver === true && secretStoreAvailable.value,
  )
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
    preferredUnavailable,
    selected,
    selectedID,
    state,
    storedCredential,
  }
}
