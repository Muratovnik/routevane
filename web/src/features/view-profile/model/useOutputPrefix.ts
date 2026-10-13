import { computed, onScopeDispose, ref, watch } from 'vue'

import { setOutputFQDNPrefix } from '@/shared/api/outputs'

export const validFQDNPrefix = (value: string): boolean =>
  value === '' ||
  (value.length <= 24 &&
    /^[a-z0-9-]+$/.test(value) &&
    /^[a-z]/.test(value) &&
    !value.endsWith('-'))

/** What one press of Save came to. */
export type PrefixOutcome = 'invalid' | 'unchanged' | 'saved' | 'failed'

/**
 * The FQDN group prefix of one format, as a draft the settings dialog edits.
 * The rule for a valid prefix is stated only when a save is refused for it:
 * the field shows its placeholder until then, not a paragraph of rules.
 */
export const useOutputPrefix = (
  outputID: () => string,
  prefix: () => string,
) => {
  const draft = ref(prefix())
  const saved = ref(prefix())
  const state = ref<'idle' | 'saving' | 'failed'>('idle')
  // Whether a save was asked for since the draft was opened; the rule is
  // reported from then on, and only then.
  const attempted = ref(false)
  let generation = 0
  onScopeDispose(() => {
    generation += 1
  })
  const valid = computed(() => validFQDNPrefix(draft.value))
  const invalid = computed(() => attempted.value && !valid.value)

  const reset = (): void => {
    generation += 1
    draft.value = prefix()
    saved.value = prefix()
    state.value = 'idle'
    attempted.value = false
  }
  watch([outputID, prefix], reset)

  const save = async (): Promise<PrefixOutcome> => {
    attempted.value = true
    if (!valid.value) return 'invalid'
    if (state.value === 'saving') return 'failed'
    if (draft.value === saved.value) return 'unchanged'
    const id = outputID()
    const request = ++generation
    state.value = 'saving'
    try {
      const result = await setOutputFQDNPrefix(id, draft.value)
      if (id !== outputID() || request !== generation) return 'failed'
      saved.value = result.output.fqdnGroupPrefix ?? ''
      draft.value = saved.value
      state.value = 'idle'
      return 'saved'
    } catch {
      if (id === outputID() && request === generation) state.value = 'failed'
      return 'failed'
    }
  }
  return { attempted, draft, invalid, reset, saved, save, state, valid }
}
