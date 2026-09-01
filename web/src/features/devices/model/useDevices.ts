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
  const state = ref<DevicesState>('loading')
  const work = ref<DevicesWork>('idle')

  const busy = computed(() => work.value === 'working')
  const targets = computed(() => catalog.value?.targets ?? [])

  async function initialize(): Promise<void> {
    state.value = 'loading'
    catalog.value = null
    deployableTargets.value = []
    catalogAvailable.value = false
    deploymentCatalogAvailable.value = false
    const [loaded, loadedCatalog, loadedDeployables] = await Promise.allSettled(
      [loadDevices(), loadCatalog(), loadDeployableTargets()],
    )
    if (loadedCatalog.status === 'fulfilled') {
      catalog.value = loadedCatalog.value
      catalogAvailable.value = true
    }
    if (loadedDeployables.status === 'fulfilled') {
      deployableTargets.value = loadedDeployables.value
      deploymentCatalogAvailable.value = true
    }
    if (loaded.status === 'rejected') {
      state.value = 'failed'
      return
    }
    devices.value = loaded.value.devices
    secretStoreAvailable.value = loaded.value.secretStoreAvailable
    state.value = 'ready'
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
    secretStoreAvailable,
    state,
    targets,
    work,
  }
}
