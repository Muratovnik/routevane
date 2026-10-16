import { onScopeDispose, ref, watch } from 'vue'

import { setOutputKeepAddresses } from '@/shared/api/outputs'

/** What one press of Save came to for this setting. */
export type KeepAddressesOutcome = 'unchanged' | 'saved' | 'failed'

/**
 * Whether one format keeps the addresses its domains already cover (ADR 0057),
 * as a draft the settings dialog edits. A change is a request: the draft
 * becomes the stored value only when the server says it did.
 */
export const useOutputKeepAddresses = (
  outputID: () => string,
  keep: () => boolean,
) => {
  const draft = ref(keep())
  const saved = ref(keep())
  const state = ref<'idle' | 'saving' | 'failed'>('idle')
  let generation = 0
  onScopeDispose(() => {
    generation += 1
  })

  const reset = (): void => {
    generation += 1
    draft.value = keep()
    saved.value = keep()
    state.value = 'idle'
  }
  watch([outputID, keep], reset)

  const save = async (): Promise<KeepAddressesOutcome> => {
    if (state.value === 'saving') return 'failed'
    if (draft.value === saved.value) return 'unchanged'
    const id = outputID()
    const request = ++generation
    state.value = 'saving'
    try {
      const result = await setOutputKeepAddresses(id, draft.value)
      if (id !== outputID() || request !== generation) return 'failed'
      saved.value = result.output.keepAddresses
      draft.value = saved.value
      state.value = 'idle'
      return 'saved'
    } catch {
      if (id === outputID() && request === generation) state.value = 'failed'
      return 'failed'
    }
  }
  return { draft, reset, saved, save, state }
}
