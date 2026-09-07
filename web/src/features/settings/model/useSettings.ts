import { computed, ref } from 'vue'

import { loadSettings, saveDefaultRefreshInterval } from '@/shared/api/settings'
import type { RefreshInterval } from '@/shared/api/profiles'

export type SettingsReadState = 'loading' | 'ready' | 'failed'
export type SettingsWriteState = 'idle' | 'saving' | 'failed'

/**
 * The refresh rule belongs to the server. A null value means this tab has not
 * received an authoritative answer yet, so it is deliberately different from
 * the server's explicit `off` value.
 */
export function useSettings() {
  const refreshInterval = ref<RefreshInterval | null>(null)
  const readState = ref<SettingsReadState>('loading')
  const writeState = ref<SettingsWriteState>('idle')
  const canChange = computed(
    () =>
      refreshInterval.value !== null &&
      readState.value === 'ready' &&
      writeState.value !== 'saving',
  )
  let readRequest = 0
  let writeRequest = 0

  // A retry changes the read state but never replaces a confirmed value until
  // the server answers. That lets the view show stale-but-honest data while it
  // is checking again, and leaves the first-read value as genuinely unknown.
  async function initialize(): Promise<boolean> {
    const request = ++readRequest
    readState.value = 'loading'
    try {
      const loaded = await loadSettings()
      if (request !== readRequest) return false
      refreshInterval.value = loaded.refreshInterval
      readState.value = 'ready'
      return true
    } catch {
      if (request !== readRequest) return false
      readState.value = 'failed'
      return false
    }
  }

  async function retry(): Promise<boolean> {
    return initialize()
  }

  async function setRefreshInterval(value: RefreshInterval): Promise<boolean> {
    if (!canChange.value) return false
    const request = ++writeRequest
    writeState.value = 'saving'
    try {
      const saved = await saveDefaultRefreshInterval(value)
      if (request !== writeRequest) return false
      refreshInterval.value = saved.refreshInterval
      writeState.value = 'idle'
      return true
    } catch {
      if (request !== writeRequest) return false
      writeState.value = 'failed'
      return false
    }
  }

  return {
    canChange,
    initialize,
    readState,
    refreshInterval,
    retry,
    setRefreshInterval,
    writeState,
  }
}
