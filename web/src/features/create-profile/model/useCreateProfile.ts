import { computed, ref, watch } from 'vue'

import {
  applyDefaultPriority,
  cloneComposition,
  normalizeComposition,
  resolvedComposition,
  listIncluded,
  toggleCompositionList,
} from '@/entities/profile-composition/model/composition'
import { useCompositionForecast } from '@/entities/profile-composition/model/forecast'
import {
  loadCatalogCached,
  localizedTargetTitle,
  type Catalog,
  type CategoryDetail,
  type ListDetail,
  type TargetOption,
} from '@/shared/api/catalog'
import { loadDeployableTargets } from '@/shared/api/deploy'
import {
  createProfile,
  type ProfileComposition,
  type TargetForecast,
} from '@/shared/api/profiles'
import { useLocale } from '@/shared/i18n/useLocale'
import { useFreshCatalog } from '@/shared/model/useFreshCatalog'
import { useSurfacePreferences } from '@/shared/model/useSurfacePreferences'

export type CatalogState = 'loading' | 'ready' | 'empty' | 'failed'
export type FlowState = 'idle' | 'creating' | 'failed'
export type CreatedProfileSetup = { profileID: string; targetID: string }

/**
 * Building one profile: pick what goes in it and the first consumer it should
 * serve. The target still belongs to an output, not to the profile; carrying its
 * id to the profile page lets that page create and publish the output while it can
 * retain the one-time subscription URL.
 *
 * Two kinds of pick, and the difference is the point. A category is a
 * reference: the profile follows it, so a list the category gains later arrives
 * without an edit. A list is named outright and stays whatever the operator
 * chose. Neither is a copy, so a list carried by two categories is still one
 * list in the result.
 *
 * The search field is a view over the catalog, never a selection: a list
 * picked and then filtered out of view stays picked, and the counter says so.
 * The name is proposed from what was picked and stays editable, so creating a
 * profile never waits on the operator inventing a title.
 */
export const useCreateProfile = () => {
  const preferences = useSurfacePreferences()
  const { locale, tor } = useLocale()
  const forecast = useCompositionForecast()
  const catalog = ref<Catalog | null>(null)
  const catalogState = ref<CatalogState>('loading')
  const flowState = ref<FlowState>('idle')
  const composition = ref<ProfileComposition>({
    lists: [],
    categories: [],
    exclusions: [],
    listDomains: {},
    priority: [],
  })
  const selectedListIDs = computed(() => composition.value.lists)
  const selectedCategoryIDs = computed(() => composition.value.categories)
  const name = ref('')
  const nameEdited = ref(false)
  const selectedTargetID = ref('')
  const deployableIDs = ref<Set<string>>(new Set())
  const defaultPriority = ref<string[]>([])
  const priorityCustomized = ref(false)

  const lists = computed<ListDetail[]>(() => catalog.value?.listDetails ?? [])
  const categories = computed<CategoryDetail[]>(
    () => catalog.value?.categories ?? [],
  )
  const targets = computed<TargetOption[]>(() =>
    (catalog.value?.targets ?? []).filter(
      (target) => !preferences.hiddenTargets.value.includes(target.id),
    ),
  )
  const targetGroups = computed<{ kind: string; targets: TargetOption[] }[]>(
    () =>
      (['router', 'app'] as const)
        .map((kind) => ({
          kind,
          targets: targets.value.filter((target) => target.kind === kind),
        }))
        .filter((group) => group.targets.length > 0),
  )

  /**
   * What the profile will publish: named lists plus every selected category's
   * members, deduplicated. The server resolves this again at build time; the
   * screen computes it so the operator sees the real size before saving.
   */
  const resolvedListIDs = computed<string[]>(() =>
    resolvedComposition(composition.value, categories.value),
  )

  const titleOf = (listID: string): string =>
    lists.value.find((detail) => detail.id === listID)?.title ?? listID

  // The proposal speaks the interface's language: a collection is named by the
  // dictionary where it has a word for it, by the catalog otherwise — the same
  // rule the picker renders it with.
  const proposedName = computed(() => {
    const parts = [
      ...selectedCategoryIDs.value.map((id) =>
        tor(
          `category.${id}`,
          categories.value.find((entry) => entry.id === id)?.title ?? id,
        ),
      ),
      ...selectedListIDs.value.map(titleOf),
    ]
    return parts.join(', ')
  })

  watch(composition, () => {
    if (!nameEdited.value) name.value = proposedName.value
  })

  // A new profile starts in the library's order. Once the operator moves a row,
  // that profile owns its snapshot and later selections append through the
  // ordinary composition normalizer instead of rewriting the chosen order.
  watch([resolvedListIDs, defaultPriority], () => {
    if (priorityCustomized.value) return
    const next = applyDefaultPriority(
      composition.value,
      categories.value,
      defaultPriority.value,
    )
    if (
      (next.priority ?? []).join('\0') ===
      (composition.value.priority ?? []).join('\0')
    )
      return
    composition.value = next
  })

  // Every change to the draft asks the server what it would weigh, in every
  // format at once: the operator is choosing between formats here, so a
  // forecast for the selected one alone would leave the alternatives blank.
  watch(
    [composition, resolvedListIDs],
    () => {
      forecast.request(composition.value, resolvedListIDs.value)
    },
    { immediate: true },
  )

  const selectedForecast = computed<TargetForecast | null>(() =>
    selectedTargetID.value === ''
      ? null
      : forecast.forTarget(selectedTargetID.value),
  )

  // Only a delivered answer stops anything. An unknown forecast — not asked
  // for yet, still in flight, or refused — leaves creating available.
  const blocked = computed(
    () =>
      selectedForecast.value !== null &&
      !selectedForecast.value.incompleteLists?.length &&
      !selectedForecast.value.fits,
  )

  /**
   * A format that would hold this profile, offered when the chosen one would not.
   * A router is proposed before an application when the operator was already
   * choosing a router, because swapping kind is a different decision from
   * swapping format.
   */
  const suggestedTarget = computed<TargetOption | null>(() => {
    if (!blocked.value) return null
    const selectedKind = targets.value.find(
      (target) => target.id === selectedTargetID.value,
    )?.kind
    const fitting = targets.value.filter(
      (target) => forecast.forTarget(target.id)?.fits === true,
    )
    return (
      fitting.find((target) => target.kind === selectedKind) ??
      fitting[0] ??
      null
    )
  })

  const suggestedTargetTitle = computed(() =>
    localizedTargetTitle(suggestedTarget.value, locale.value),
  )

  const busy = computed(() => flowState.value === 'creating')
  const canCreate = computed(
    () =>
      catalogState.value === 'ready' &&
      !busy.value &&
      !blocked.value &&
      resolvedListIDs.value.length > 0 &&
      name.value.trim() !== '' &&
      selectedTargetID.value !== '',
  )

  const initialize = async (): Promise<void> => {
    catalogState.value = 'loading'
    const [loadedCatalog, deployables] = await Promise.allSettled([
      loadCatalogCached(),
      loadDeployableTargets(),
    ])
    if (loadedCatalog.status === 'rejected') {
      catalogState.value = 'failed'
      return
    }
    catalog.value = loadedCatalog.value
    defaultPriority.value =
      loadedCatalog.value.defaultPriority ??
      loadedCatalog.value.listDetails.map((list) => list.id)
    catalogState.value =
      loadedCatalog.value.listDetails.length === 0 ? 'empty' : 'ready'
    if (deployables.status === 'fulfilled') {
      deployableIDs.value = new Set(
        deployables.value.map((entry) => entry.targetID),
      )
    }
  }

  const setName = (value: string): void => {
    nameEdited.value = true
    name.value = value
  }

  const setPriority = (ids: string[]): void => {
    if (busy.value) return
    priorityCustomized.value = true
    composition.value = normalizeComposition(
      { ...cloneComposition(composition.value), priority: ids },
      categories.value,
    )
  }

  const removeList = (id: string): void => {
    if (busy.value || !listIncluded(composition.value, categories.value, id))
      return
    composition.value = toggleCompositionList(
      composition.value,
      categories.value,
      id,
    )
  }

  /**
   * registerCatalog replaces the whole copy this screen holds rather than
   * patching it: a membership change reaches lists, categories and the
   * profiles that follow them at once, and this screen owns exactly one copy of
   * that fact. Composing writes nothing global (ADR 0029), so the change always
   * came from «Списки» in another tab.
   */
  const registerCatalog = (next: Catalog): void => {
    catalog.value = next
    defaultPriority.value =
      next.defaultPriority ?? next.listDetails.map((list) => list.id)
  }

  useFreshCatalog(registerCatalog, () => busy.value)

  const setTarget = (id: string): void => {
    if (busy.value || !targets.value.some((target) => target.id === id)) return
    selectedTargetID.value = id
  }

  const deployable = (targetID: string): boolean =>
    deployableIDs.value.has(targetID)

  const forecastFor = (targetID: string): TargetForecast | null =>
    forecast.forTarget(targetID)

  const targetTitle = (target: TargetOption): string =>
    localizedTargetTitle(target, locale.value)

  const selectedTargetTitle = computed(() =>
    localizedTargetTitle(
      targets.value.find((target) => target.id === selectedTargetID.value),
      locale.value,
    ),
  )

  /** create stores the profile and carries the chosen first output to its page. */
  const create = async (): Promise<CreatedProfileSetup | null> => {
    if (!canCreate.value) return null
    try {
      flowState.value = 'creating'
      const profile = await createProfile(name.value.trim(), composition.value)
      flowState.value = 'idle'
      return {
        profileID: profile.id,
        targetID: selectedTargetID.value,
      }
    } catch {
      flowState.value = 'failed'
      return null
    }
  }

  return {
    blocked,
    busy,
    canCreate,
    catalogState,
    categories,
    composition,
    create,
    deployable,
    defaultPriority,
    flowState,
    forecastFor,
    initialize,
    name,
    observing: forecast.observing,
    forecastPending: forecast.pending,
    forecastFailure: forecast.failure,
    refreshSources: () =>
      forecast.refresh(composition.value, resolvedListIDs.value),
    retryForecast: () =>
      forecast.retry(composition.value, resolvedListIDs.value),
    registerCatalog,
    removeList,
    resolvedListIDs,
    selectedCategoryIDs,
    selectedForecast,
    selectedListIDs,
    selectedTargetID,
    selectedTargetTitle,
    lists,
    setName,
    setPriority,
    setTarget,
    suggestedTarget,
    suggestedTargetTitle,
    targetGroups,
    targets,
    targetTitle,
  }
}
