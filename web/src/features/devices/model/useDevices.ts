import { computed, ref } from 'vue'

import { loadCatalog, type Catalog } from '@/shared/api/catalog'
import {
  loadDeployableTargets,
  type DeployableTarget,
} from '@/shared/api/deploy'
import {
  disableAutoDelivery,
  enableAutoDelivery,
  forgetDevice,
  loadDevices,
  registerDevice,
  type DeviceCard,
} from '@/shared/api/devices'

export type DevicesState = 'loading' | 'ready' | 'failed'
export type DevicesWork = 'idle' | 'working' | 'failed'
export type RequirementsState = 'unknown' | 'loading' | 'ready' | 'failed'

/**
 * The devices this operator installed, as opposed to the formats this product
 * supports. Registration is by hand: a product that scanned the network would
 * be inspecting a network it was not invited to inspect.
 *
 * Whether a credential can be kept at all is the platform's answer, and it
 * arrives with the list. A screen that offered unattended delivery where
 * nothing can hold a password would be promising something it cannot keep.
 */
export function useDevices() {
  const devices = ref<DeviceCard[]>([])
  const catalog = ref<Catalog | null>(null)
  const secretStoreAvailable = ref(false)
  const deployableTargets = ref<DeployableTarget[]>([])
  const catalogAvailable = ref(false)
  const deploymentCatalogAvailable = ref(false)
  const requirementsState = ref<RequirementsState>('unknown')
  const state = ref<DevicesState>('loading')
  const work = ref<DevicesWork>('idle')
  let deviceRead = false
  let initializeRequest = 0
  let requirementsRequest = 0

  const busy = computed(() => work.value === 'working')
  const targets = computed(() => catalog.value?.targets ?? [])

  // Requirements are a separate read from the device registry. `ready` means
  // this tab has a current authoritative answer; an empty deployable list is a
  // successful answer that a target has no deployer, not an unknown failure.
  async function initialize(): Promise<void> {
    const request = ++initializeRequest
    const requirements = ++requirementsRequest
    if (!deviceRead) state.value = 'loading'
    requirementsState.value = 'loading'
    const [loaded, loadedCatalog, loadedDeployables] = await Promise.allSettled(
      [loadDevices(), loadCatalog(), loadDeployableTargets()],
    )
    if (request !== initializeRequest) return
    if (loadedCatalog.status === 'fulfilled') {
      catalog.value = loadedCatalog.value
      catalogAvailable.value = true
    } else {
      catalogAvailable.value = false
    }
    if (
      loadedDeployables.status === 'fulfilled' &&
      requirements === requirementsRequest
    ) {
      deployableTargets.value = loadedDeployables.value
      deploymentCatalogAvailable.value = true
      requirementsState.value = 'ready'
    } else if (requirements === requirementsRequest) {
      deploymentCatalogAvailable.value = false
      requirementsState.value = 'failed'
    }
    if (loaded.status === 'rejected') {
      if (!deviceRead) state.value = 'failed'
      return
    }
    devices.value = loaded.value.devices
    secretStoreAvailable.value = loaded.value.secretStoreAvailable
    state.value = 'ready'
    deviceRead = true
  }

  async function retryRequirements(): Promise<boolean> {
    // A dependency retry must not blank either the known device cards or the
    // draft fields owned by the view. Only this endpoint is called here.
    if (requirementsState.value === 'loading') return false
    const request = ++requirementsRequest
    requirementsState.value = 'loading'
    try {
      const loaded = await loadDeployableTargets()
      if (request !== requirementsRequest) return false
      deployableTargets.value = loaded
      deploymentCatalogAvailable.value = true
      requirementsState.value = 'ready'
      return true
    } catch {
      if (request !== requirementsRequest) return false
      deploymentCatalogAvailable.value = false
      requirementsState.value = 'failed'
      return false
    }
  }

  async function run(action: () => Promise<unknown>): Promise<boolean> {
    if (busy.value) return false
    work.value = 'working'
    try {
      await action()
      await initialize()
      work.value = 'idle'
      return true
    } catch {
      work.value = 'failed'
      return false
    }
  }

  function register(
    targetID: string,
    name: string,
    address: string,
    account: string,
    interfaceName: string,
  ): Promise<boolean> {
    if (requirementsState.value !== 'ready') return Promise.resolve(false)
    return run(() =>
      registerDevice(targetID, name, address, account, interfaceName),
    )
  }

  function forget(id: string): Promise<boolean> {
    return run(() => forgetDevice(id))
  }

  /**
   * Turning delivery on carries the password once, to the server, which hands
   * it to the operating system. It is never stored in this tab and never read
   * back: the surface can say that a credential is held, and nothing more.
   */
  function enable(id: string, credential: string): Promise<boolean> {
    const device = devices.value.find((candidate) => candidate.id === id)
    if (
      requirementsState.value !== 'ready' ||
      device === undefined ||
      !device.deployable ||
      !deployableTargets.value.some(
        (target) => target.targetID === device.targetID,
      )
    )
      return Promise.resolve(false)
    return run(() => enableAutoDelivery(id, credential))
  }

  function disable(id: string): Promise<boolean> {
    return run(() => disableAutoDelivery(id))
  }

  return {
    busy,
    catalogAvailable,
    deployableTargets,
    deploymentCatalogAvailable,
    devices,
    disable,
    enable,
    forget,
    initialize,
    register,
    requirementsState,
    retryRequirements,
    secretStoreAvailable,
    state,
    targets,
    work,
  }
}
