import { computed, onScopeDispose, ref, watch } from 'vue'

import { setOutputFQDNPrefix } from '@/shared/api/outputs'

export const validFQDNPrefix = (value: string): boolean =>
  value === '' ||
  (value.length <= 24 &&
    /^[a-z0-9-]+$/.test(value) &&
    /^[a-z]/.test(value) &&
    !value.endsWith('-'))

export const useOutputPrefix = (
  outputID: () => string,
  prefix: () => string,
) => {
  const draft = ref(prefix())
  const saved = ref(prefix())
  const state = ref<'idle' | 'saving' | 'saved' | 'failed'>('idle')
  let generation = 0
  onScopeDispose(() => {
    generation += 1
  })
  const valid = computed(() => validFQDNPrefix(draft.value))
  watch([outputID, prefix], () => {
    generation += 1
    draft.value = prefix()
    saved.value = prefix()
    state.value = 'idle'
  })
  const save = async (): Promise<void> => {
    if (!valid.value || state.value === 'saving') return
    const id = outputID()
    const request = ++generation
    state.value = 'saving'
    try {
      const result = await setOutputFQDNPrefix(id, draft.value)
      if (id !== outputID() || request !== generation) return
      saved.value = result.output.fqdnGroupPrefix ?? ''
      draft.value = saved.value
      state.value = 'saved'
    } catch {
      if (id === outputID() && request === generation) state.value = 'failed'
    }
  }
  return { draft, saved, state, valid, save }
}
