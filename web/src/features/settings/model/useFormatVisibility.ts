import { computed, ref } from 'vue'

import {
  loadCatalog,
  localizedTargetTitle,
  type TargetOption,
} from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'
import { useSurfacePreferences } from '@/shared/model/useSurfacePreferences'

export type FormatsState = 'loading' | 'ready' | 'empty' | 'failed'

export type FormatChoice = { id: string; title: string; shown: boolean }

/**
 * Which formats the profile composer and the profile page offer when someone
 * adds one. The catalog says what exists; hiding is a display preference of
 * this browser, never a server fact — a hidden format keeps its lists, files
 * and deployments exactly as they are.
 *
 * A switch states the preference as it is applied: the preference is stored
 * synchronously, so what the switch shows is what the composer will read.
 */
export const useFormatVisibility = () => {
  const preferences = useSurfacePreferences()
  const { locale } = useLocale()
  const targets = ref<TargetOption[]>([])
  const state = ref<FormatsState>('loading')
  let request = 0

  const load = async (): Promise<void> => {
    const current = ++request
    state.value = 'loading'
    try {
      const catalog = await loadCatalog()
      if (current !== request) return
      targets.value = catalog.targets
      state.value = catalog.targets.length === 0 ? 'empty' : 'ready'
    } catch {
      if (current !== request) return
      state.value = 'failed'
    }
  }

  const choices = computed<FormatChoice[]>(() =>
    targets.value.map((target) => ({
      id: target.id,
      title: localizedTargetTitle(target, locale.value, target.id),
      shown: !preferences.hiddenTargets.value.includes(target.id),
    })),
  )

  const setShown = (id: string, shown: boolean): void => {
    preferences.setTargetHidden(id, !shown)
  }

  return { choices, load, setShown, state }
}
