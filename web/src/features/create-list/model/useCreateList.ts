import { computed, ref, watch } from 'vue'

import {
  applyDefaultPriority,
  cloneComposition,
  normalizeComposition,
  resolvedComposition,
  serviceIncluded,
  toggleCompositionService,
} from '@/entities/list-composition/model/composition'
import { useCompositionForecast } from '@/entities/list-composition/model/forecast'
import {
  loadCatalogCached,
  localizedTargetTitle,
  type Catalog,
  type CategoryDetail,
  type ServiceDetail,
  type TargetOption,
} from '@/shared/api/catalog'
import { loadDeployableTargets } from '@/shared/api/deploy'
import {
  createList,
  type ListComposition,
  type TargetForecast,
} from '@/shared/api/lists'
import { useLocale } from '@/shared/i18n/useLocale'
import { useFreshCatalog } from '@/shared/model/useFreshCatalog'
import { useSurfacePreferences } from '@/shared/model/useSurfacePreferences'

export type CatalogState = 'loading' | 'ready' | 'empty' | 'failed'
export type FlowState = 'idle' | 'creating' | 'failed'
export type CreatedListSetup = { listID: string; targetID: string }

/**
 * Building one list: pick what goes in it and the first consumer it should
 * serve. The target still belongs to an output, not to the list; carrying its
 * id to the list page lets that page create and publish the output while it can
 * retain the one-time subscription URL.
 *
 * Two kinds of pick, and the difference is the point. A category is a
 * reference: the list follows it, so a service the category gains later arrives
 * without an edit. A service is named outright and stays whatever the operator
 * chose. Neither is a copy, so a service carried by two categories is still one
 * service in the result.
 *
 * The search field is a view over the catalog, never a selection: a service
 * picked and then filtered out of view stays picked, and the counter says so.
 * The name is proposed from what was picked and stays editable, so creating a
 * list never waits on the operator inventing a title.
 */
export function useCreateList() {
  const preferences = useSurfacePreferences()
  const { locale, tor } = useLocale()
  const forecast = useCompositionForecast()
  const catalog = ref<Catalog | null>(null)
  const catalogState = ref<CatalogState>('loading')
  const flowState = ref<FlowState>('idle')
  const composition = ref<ListComposition>({
    services: [],
    categories: [],
    exclusions: [],
    serviceDomains: {},
    priority: [],
  })
  const selectedServiceIDs = computed(() => composition.value.services)
  const selectedCategoryIDs = computed(() => composition.value.categories)
  const name = ref('')
  const nameEdited = ref(false)
  const selectedTargetID = ref('')
  const deployableIDs = ref<Set<string>>(new Set())
  const defaultPriority = ref<string[]>([])
  const priorityCustomized = ref(false)

  const services = computed<ServiceDetail[]>(
    () => catalog.value?.serviceDetails ?? [],
  )
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
   * What the list will publish: named services plus every selected category's
   * members, deduplicated. The server resolves this again at build time; the
   * screen computes it so the operator sees the real size before saving.
   */
  const resolvedServiceIDs = computed<string[]>(() =>
    resolvedComposition(composition.value, categories.value),
  )

  function titleOf(serviceID: string): string {
    return (
      services.value.find((detail) => detail.id === serviceID)?.title ??
      serviceID
    )
  }

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
      ...selectedServiceIDs.value.map(titleOf),
    ]
    return parts.join(', ')
  })

  watch(composition, () => {
    if (!nameEdited.value) name.value = proposedName.value
  })

  // A new route starts in the library's order. Once the operator moves a row,
  // that route owns its snapshot and later selections append through the
  // ordinary composition normalizer instead of rewriting the chosen order.
  watch([resolvedServiceIDs, defaultPriority], () => {
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
    [composition, resolvedServiceIDs],
    () => {
      forecast.request(composition.value, resolvedServiceIDs.value)
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
      !selectedForecast.value.incompleteServices?.length &&
      !selectedForecast.value.fits,
  )

  /**
   * A format that would hold this list, offered when the chosen one would not.
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
      resolvedServiceIDs.value.length > 0 &&
      name.value.trim() !== '' &&
      selectedTargetID.value !== '',
  )

  async function initialize(): Promise<void> {
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
      loadedCatalog.value.serviceDetails.map((service) => service.id)
    catalogState.value =
      loadedCatalog.value.serviceDetails.length === 0 ? 'empty' : 'ready'
    if (deployables.status === 'fulfilled') {
      deployableIDs.value = new Set(
        deployables.value.map((entry) => entry.targetID),
      )
    }
  }

  function setName(value: string): void {
    nameEdited.value = true
    name.value = value
  }

  function setPriority(ids: string[]): void {
    if (busy.value) return
    priorityCustomized.value = true
    composition.value = normalizeComposition(
      { ...cloneComposition(composition.value), priority: ids },
      categories.value,
    )
  }

  function removeService(id: string): void {
    if (busy.value || !serviceIncluded(composition.value, categories.value, id))
      return
    composition.value = toggleCompositionService(
      composition.value,
      categories.value,
      id,
    )
  }

  /**
   * registerCatalog replaces the whole copy this screen holds rather than
   * patching it: a membership change reaches services, categories and the
   * routes that follow them at once, and this screen owns exactly one copy of
   * that fact. Composing writes nothing global (ADR 0029), so the change always
   * came from «Списки» in another tab.
   */
  function registerCatalog(next: Catalog): void {
    catalog.value = next
    defaultPriority.value =
      next.defaultPriority ?? next.serviceDetails.map((service) => service.id)
  }

  useFreshCatalog(registerCatalog, () => busy.value)

  function setTarget(id: string): void {
    if (busy.value || !targets.value.some((target) => target.id === id)) return
    selectedTargetID.value = id
  }

  function deployable(targetID: string): boolean {
    return deployableIDs.value.has(targetID)
  }

  function forecastFor(targetID: string): TargetForecast | null {
    return forecast.forTarget(targetID)
  }

  function targetTitle(target: TargetOption): string {
    return localizedTargetTitle(target, locale.value)
  }

  const selectedTargetTitle = computed(() =>
    localizedTargetTitle(
      targets.value.find((target) => target.id === selectedTargetID.value),
      locale.value,
    ),
  )

  /** create stores the list and carries the chosen first output to its page. */
  async function create(): Promise<CreatedListSetup | null> {
    if (!canCreate.value) return null
    try {
      flowState.value = 'creating'
      const list = await createList(name.value.trim(), composition.value)
      flowState.value = 'idle'
      return {
        listID: list.id,
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
      forecast.refresh(composition.value, resolvedServiceIDs.value),
    retryForecast: () =>
      forecast.retry(composition.value, resolvedServiceIDs.value),
    registerCatalog,
    removeService,
    resolvedServiceIDs,
    selectedCategoryIDs,
    selectedForecast,
    selectedServiceIDs,
    selectedTargetID,
    selectedTargetTitle,
    services,
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
