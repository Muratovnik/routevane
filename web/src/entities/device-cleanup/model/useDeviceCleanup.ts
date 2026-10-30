import { computed, ref } from 'vue'

import {
  loadOutputDeviceObjects,
  loadProfileDeviceObjects,
  type DeviceObjects,
} from '@/shared/api/deviceObjects'

/** The format or the profile whose retirement the question is about. */
export type DeviceCleanupScope = { kind: 'output' | 'profile'; id: string }

/**
 * What retiring a format or a profile would also remove from routers, and the
 * operator's choice to remove it (ADR 0060). The question reads it when it
 * opens, and again after a refusal: routers cleaned before the one that
 * refused stay clean, so what is left is no longer what was asked about.
 */
export const useDeviceCleanup = () => {
  const objects = ref<DeviceObjects | null>(null)
  const state = ref<'loading' | 'ready' | 'failed'>('loading')
  const chosen = ref(false)
  let scope: DeviceCleanupScope | null = null
  let request = 0

  const read = async (): Promise<void> => {
    const current = scope
    if (current === null) return
    const ticket = ++request
    state.value = 'loading'
    try {
      const answer =
        current.kind === 'output'
          ? await loadOutputDeviceObjects(current.id)
          : await loadProfileDeviceObjects(current.id)
      if (ticket !== request) return
      objects.value = answer
      state.value = 'ready'
      // A choice the routers can no longer honour is not left standing on.
      if (!answer.cleanable) chosen.value = false
    } catch {
      if (ticket !== request) return
      objects.value = null
      state.value = 'failed'
      chosen.value = false
    }
  }

  const open = (next: DeviceCleanupScope): void => {
    scope = next
    objects.value = null
    chosen.value = false
    void read()
  }

  return {
    chosen,
    objects,
    open,
    /** Reads what is left after an attempt that stopped. */
    reread: read,
    /** The choice as the act carries it: only when every router can be cleaned. */
    requested: computed(
      () =>
        chosen.value &&
        state.value === 'ready' &&
        objects.value?.cleanable === true,
    ),
    state,
  }
}

// The codes a removal that stopped at a router answers: the send's own, since
// each router runs the send lifecycle (ADR 0060).
const deviceRefusals = new Set([
  'device_auth_rejected',
  'device_interface_missing',
  'device_unreachable',
  'device_command_rejected',
  'device_answer_invalid',
  'verify_failed',
  'rollback_failed',
  'backup_unavailable',
  'device_incompatible',
  'connection_invalid',
  'deployer_unavailable',
])

/**
 * The message key for a retirement the removal from routers stopped, or ''
 * when the refusal is not the removal's.
 */
export const cleanupRefusalKey = (code: string): string => {
  if (code === 'device_cleanup_unavailable') return 'cleanup.error.unavailable'
  if (code === 'device_cleanup_failed') return 'cleanup.error.failed'
  return deviceRefusals.has(code) ? `error.${code}` : ''
}
