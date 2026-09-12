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
  updateDevice,
  type DeviceCard,
  type DeviceWrite,
} from '@/shared/api/devices'

export type DevicesState = 'loading' | 'ready' | 'stale' | 'failed'
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
export const useDevices = () => {
  const devices = ref<DeviceCard[]>([])
  const catalog = ref<Catalog | null>(null)
  const secretStoreAvailable = ref(false)
  const deployableTargets = ref<DeployableTarget[]>([])
  const catalogAvailable = ref(false)
  const deploymentCatalogAvailable = ref(false)
  const requirementsState = ref<RequirementsState>('unknown')
  const state = ref<DevicesState>('loading')
  const work = ref<DevicesWork>('idle')
  const registeredID = ref('')
  let deviceRead = false
  let initializeRequest = 0
  let requirementsRequest = 0

  const busy = computed(() => work.value === 'working')
  const targets = computed(() => catalog.value?.targets ?? [])

  // Requirements are a separate read from the device registry. `ready` means
  // this tab has a current authoritative answer; an empty deployable list is a
  // successful answer that a target has no deployer, not an unknown failure.
  const initialize = async (): Promise<void> => {
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
      state.value = deviceRead ? 'stale' : 'failed'
      return
    }
    devices.value = loaded.value.devices
    secretStoreAvailable.value = loaded.value.secretStoreAvailable
    state.value = 'ready'
    deviceRead = true
  }

  const retryDevices = async (): Promise<void> => {
    const request = ++initializeRequest
    try {
      const loaded = await loadDevices()
      if (request !== initializeRequest) return
      devices.value = loaded.devices
      secretStoreAvailable.value = loaded.secretStoreAvailable
      state.value = 'ready'
      deviceRead = true
    } catch {
      if (request === initializeRequest)
        state.value = deviceRead ? 'stale' : 'failed'
    }
  }

  const retryRequirements = async (): Promise<boolean> => {
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

  const applyWrite = (
    saved: DeviceWrite,
    fallback: Partial<DeviceCard>,
  ): void => {
    const previous = devices.value.find((device) => device.id === saved.id)
    const targetID =
      saved.targetID ?? fallback.targetID ?? previous?.targetID ?? ''
    const target = catalog.value?.targets.find(
      (candidate) => candidate.id === targetID,
    )
    const next: DeviceCard = {
      id: saved.id,
      targetID,
      targetTitle: previous?.targetTitle ?? target?.title ?? targetID,
      name: saved.name ?? fallback.name ?? previous?.name ?? '',
      address: saved.address ?? fallback.address ?? previous?.address ?? '',
      account: saved.account ?? fallback.account ?? previous?.account ?? '',
      interfaceName:
        saved.interfaceName ??
        fallback.interfaceName ??
        previous?.interfaceName ??
        '',
      autoDeliver: saved.autoDeliver ?? previous?.autoDeliver ?? false,
      deployable:
        previous?.deployable ??
        deployableTargets.value.some(
          (candidate) => candidate.targetID === targetID,
        ),
    }
    const index = devices.value.findIndex((device) => device.id === saved.id)
    if (index === -1) devices.value = [...devices.value, next]
    else devices.value[index] = next
  }

  const run = async <T>(
    action: () => Promise<T>,
    onSuccess?: (result: T) => void,
  ): Promise<boolean> => {
    if (busy.value || state.value === 'stale') return false
    work.value = 'working'
    try {
      const result = await action()
      onSuccess?.(result)
      // The write response is authoritative for the submitted device even if
      // the follow-up registry read is stale. `state` keeps that read failure
      // visible and retryable, while the operation itself remains confirmed.
      await initialize()
      work.value = 'idle'
      return true
    } catch {
      work.value = 'failed'
      return false
    }
  }

  const register = (
    targetID: string,
    name: string,
    address: string,
    account: string,
    interfaceName: string,
  ): Promise<boolean> => {
    if (requirementsState.value !== 'ready') return Promise.resolve(false)
    return run(
      () => registerDevice(targetID, name, address, account, interfaceName),
      (saved) => {
        registeredID.value = saved.id
        applyWrite(saved, { targetID, name, address, account, interfaceName })
      },
    )
  }

  /**
   * Changing a registered connection's parameters. It goes through the same
   * guard and the same authoritative re-read as registration, because the
   * server may answer the change by revoking automatic delivery, and the card
   * this tab shows afterwards has to be the server's own.
   */
  const update = (
    id: string,
    name: string,
    address: string,
    account: string,
    interfaceName: string,
  ): Promise<boolean> => {
    if (requirementsState.value !== 'ready') return Promise.resolve(false)
    return run(
      () => updateDevice(id, name, address, account, interfaceName),
      (result) =>
        applyWrite(result, {
          targetID: devices.value.find((device) => device.id === id)?.targetID,
          name,
          address,
          account,
          interfaceName,
        }),
    )
  }

  const forget = (id: string): Promise<boolean> => run(() => forgetDevice(id))

  /**
   * Turning delivery on carries the password once, to the server, which hands
   * it to the operating system. It is never stored in this tab and never read
   * back: the surface can say that a credential is held, and nothing more.
   */
  const enable = (id: string, credential: string): Promise<boolean> => {
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

  const disable = (id: string): Promise<boolean> =>
    run(() => disableAutoDelivery(id))

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
    registeredID,
    requirementsState,
    retryRequirements,
    retryDevices,
    secretStoreAvailable,
    state,
    targets,
    update,
    work,
  }
}

/**
 * What the connections screen reads and acts through. The list and the detail
 * are two views of one registry, so they take the same model rather than each
 * holding a copy of its state.
 */
export type DevicesModel = ReturnType<typeof useDevices>
