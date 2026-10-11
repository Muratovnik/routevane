import { onScopeDispose, ref } from 'vue'

import { loadRuntimeVersion } from '@/shared/api/runtime'

/**
 * The version the running service reports about itself. Null means no answer
 * has established one: the read has not finished, or it failed. An unavailable
 * or older service does not establish a version, so a failed read leaves it
 * unknown rather than inferring one from the interface package.
 */
export const useRuntimeVersion = () => {
  const version = ref<string | null>(null)
  // A reply that arrives after the screen is gone belongs to nobody.
  let current = true
  onScopeDispose(() => {
    current = false
  })

  const load = async (): Promise<void> => {
    try {
      const reported = await loadRuntimeVersion()
      if (current) version.value = reported
    } catch {
      // Left unknown: the row that states it stays hidden.
    }
  }

  return { load, version }
}
