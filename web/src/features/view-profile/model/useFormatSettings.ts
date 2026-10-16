import { computed } from 'vue'

import { useOutputKeepAddresses } from '@/features/view-profile/model/useOutputKeepAddresses'
import { useOutputPrefix } from '@/features/view-profile/model/useOutputPrefix'

/** What one press of Save came to for the settings together. */
export type FormatSettingsOutcome = 'invalid' | 'unchanged' | 'saved' | 'failed'

/**
 * The settings of one format as one draft with one Save. Save writes what
 * changed, a setting at a time, and stops at the first the server refuses: what
 * was already written stays written, the refused draft stays on screen, and the
 * next Save sends only what is still different.
 */
export const useFormatSettings = (
  outputID: () => string,
  stored: () => { keepAddresses: boolean; prefix: string },
) => {
  const prefix = useOutputPrefix(outputID, () => stored().prefix)
  const keepAddresses = useOutputKeepAddresses(
    outputID,
    () => stored().keepAddresses,
  )
  const saving = computed(
    () =>
      prefix.state.value === 'saving' || keepAddresses.state.value === 'saving',
  )
  // Whether a write reached the server since the dialog was last read, so the
  // page can read the stored values again even when a later setting was refused.
  let wrote = false

  const reset = (): void => {
    prefix.reset()
    keepAddresses.reset()
    wrote = false
  }

  const save = async (): Promise<FormatSettingsOutcome> => {
    const prefixOutcome = await prefix.save()
    if (prefixOutcome === 'invalid' || prefixOutcome === 'failed')
      return prefixOutcome
    if (prefixOutcome === 'saved') wrote = true
    const keepOutcome = await keepAddresses.save()
    if (keepOutcome === 'failed') return 'failed'
    if (keepOutcome === 'saved') wrote = true
    return prefixOutcome === 'saved' || keepOutcome === 'saved'
      ? 'saved'
      : 'unchanged'
  }

  /** Whether anything was written since the last call, then forgets it. */
  const takeWrote = (): boolean => {
    const result = wrote
    wrote = false
    return result
  }
  return { keepAddresses, prefix, reset, save, saving, takeWrote }
}
