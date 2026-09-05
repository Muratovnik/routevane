<script setup lang="ts">
import { useWorkspaceInspection } from '@/shared/ui/workspacePane'
import { useSortable } from '@vueuse/integrations/useSortable'
import { computed, ref, useTemplateRef, watch } from 'vue'

import {
  overlapServiceIDs,
  resolvedComposition,
  serviceIdentityLabels,
  serviceIncluded,
  setCompositionCategoryReference,
  toggleCompositionService,
} from '@/entities/list-composition/model/composition'
import type { CategoryDetail, ServiceDetail } from '@/shared/api/catalog'
import type { ListComposition, TargetForecast } from '@/shared/api/lists'
import { useLocale } from '@/shared/i18n/useLocale'
import { tableDragGeometry } from '@/shared/lib/tableDrag'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvMenu from '@/shared/ui/RvMenu.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import CategoryFilters from './CategoryFilters.vue'
import CategoryLabel from './CategoryLabel.vue'

import ServiceDetailDialog from './ServiceDetailDialog.vue'

const inspectWorkspace = useWorkspaceInspection()
const props = defineProps<{
  initialPriority?: string[]
  fill?: boolean
  modelValue: ListComposition
  services: ServiceDetail[]
  categories: CategoryDetail[]
  disabled?: boolean
  listName?: string
  pending?: boolean
  forecast?: TargetForecast | null
  forecastPending?: boolean
  forecastFailure?: string
  overlapUnavailable?: boolean
  overlapUnavailableLabel?: string
  retryable?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: ListComposition]
  reorder: [ids: string[]]
  refresh: []
  retry: []
}>()

const { formatNumber, t, tc, tor } = useLocale()
const query = ref('')
const categoryFilter = ref('all')
const activeServiceID = ref('')
const uncategorizedID = 'rv:uncategorized'

const servicesByID = computed(
  () => new Map(props.services.map((service) => [service.id, service])),
)
const serviceLabels = computed(() => serviceIdentityLabels(props.services))
const resolved = computed(() =>
  resolvedComposition(props.modelValue, props.categories),
)
const resolvedSet = computed(() => new Set(resolved.value))
const categoryByID = computed(
  () => new Map(props.categories.map((category) => [category.id, category])),
)
const selectedCategory = computed(
  () => categoryByID.value.get(categoryFilter.value) ?? null,
)
const followsSelectedCategory = computed(
  () =>
    selectedCategory.value !== null &&
    props.modelValue.categories.includes(selectedCategory.value.id),
)

const allRows = computed<ServiceDetail[]>(() => {
  const rows = [...props.services]
  for (const id of resolved.value) {
    if (!servicesByID.value.has(id))
      rows.push({ categories: [], id, title: id })
  }
  return rows
})

const visibleRows = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase()
  return allRows.value.filter((service) => {
    const categories = serviceCategories(service)
    const inCategory =
      categoryFilter.value === 'all' ||
      (categoryFilter.value === uncategorizedID
        ? categories.length === 0
        : categories.some((category) => category.id === categoryFilter.value))
    if (!inCategory) return false
    if (needle === '') return true
    return [
      service.id,
      service.title,
      ...categories.map((category) => categoryLabel(category)),
    ].some((value) => value.toLocaleLowerCase().includes(needle))
  })
})

// Membership never sorts the catalog. Only an explicit move changes row positions.
const rowOrder = ref<string[]>([])
watch(
  allRows,
  (rows) => {
    const catalogOrder = [
      ...new Set(props.categories.flatMap((category) => category.services)),
    ]
    const rank = new Map(catalogOrder.map((id, index) => [id, index]))
    let ids = rows
      .map((row) => row.id)
      .sort(
        (a, b) =>
          (rank.get(a) ?? catalogOrder.length) -
          (rank.get(b) ?? catalogOrder.length),
      )
    if (rowOrder.value.length === 0) {
      const initial = props.initialPriority ?? props.modelValue.priority ?? []
      const ranked = initial.filter((id) => ids.includes(id))
      const known = new Set(ranked)
      let index = 0
      ids = ids.map((id) => (known.has(id) ? ranked[index++]! : id))
    }
    rowOrder.value = [
      ...rowOrder.value.filter((id) => ids.includes(id)),
      ...ids.filter((id) => !rowOrder.value.includes(id)),
    ]
  },
  { immediate: true },
)
const tableBody = useTemplateRef<HTMLElement>('tableBody')
let draggedID = ''
const visibleOrder = computed({
  get: () =>
    rowOrder.value.filter((id) =>
      visibleRows.value.some((row) => row.id === id),
    ),
  set: (ids: string[]) => {
    if (props.disabled || !resolvedSet.value.has(draggedID)) return
    const visible = new Set(ids)
    let index = 0
    rowOrder.value = rowOrder.value.map((id) =>
      visible.has(id) ? ids[index++]! : id,
    )
    const selected = ids.filter((id) => resolvedSet.value.has(id))
    const movedIndex = selected.indexOf(draggedID)
    if (movedIndex < 0) return
    const next = selected[movedIndex + 1]
    const previous = selected[movedIndex - 1]
    const priority = resolved.value.filter((id) => id !== draggedID)
    if (next !== undefined)
      priority.splice(priority.indexOf(next), 0, draggedID)
    else if (previous !== undefined)
      priority.splice(priority.indexOf(previous) + 1, 0, draggedID)
    else return
    emit('reorder', priority)
  },
})
const orderedRows = computed(() => {
  const rows = new Map(allRows.value.map((row) => [row.id, row]))
  return visibleOrder.value.map((id) => rows.get(id)!)
})
const sortable = useSortable(tableBody, visibleOrder, {
  animation: 200,
  disabled: props.disabled,
  fallbackOnBody: true,
  fallbackTolerance: 3,
  forceFallback: true,
  handle: '.picker__handle:not(:disabled)',
  filter: '.picker__handle:disabled',
  preventOnFilter: false,
  ...tableDragGeometry(),
  watchElement: true,
  onStart: (event) => {
    draggedID = event.item.dataset.id ?? ''
  },
})
watch(
  () => props.disabled,
  (disabled) => sortable.option('disabled', Boolean(disabled)),
  { flush: 'sync' },
)

function movePriority(id: string, offset: number): void {
  if (props.disabled) return
  const priority = [...resolved.value]
  const from = priority.indexOf(id)
  const to = from + offset
  if (from < 0 || to < 0 || to >= priority.length) return
  const target = priority[to]!
  priority.splice(from, 1)
  priority.splice(to, 0, id)
  const rows = rowOrder.value.filter((row) => row !== id)
  rows.splice(rows.indexOf(target) + (offset > 0 ? 1 : 0), 0, id)
  rowOrder.value = rows
  emit('reorder', priority)
}
const overlapMessage = computed(() => {
  if (resolved.value.length < 2) return ''
  if (props.overlapUnavailable)
    return (
      props.overlapUnavailableLabel ?? t('servicePicker.overlap.unavailable')
    )
  if (props.forecastPending) return t('servicePicker.overlap.pending')
  if (props.forecastFailure) return props.forecastFailure
  if (props.forecast?.incompleteServices?.length)
    return t('forecast.partial.table', {
      n: resolved.value.length - props.forecast.incompleteServices.length,
      m: resolved.value.length,
    })
  return resolved.value.some((id) => overlapTitles(id) === null)
    ? t('servicePicker.overlap.unknown')
    : ''
})
const activeService = computed(
  () => servicesByID.value.get(activeServiceID.value) ?? null,
)
const ruleCounts = computed(
  () =>
    new Map(
      (props.forecast?.perService ?? []).map((row) => [
        row.serviceID,
        row.rules,
      ]),
    ),
)

function categoryLabel(category: CategoryDetail): string {
  return tor(`category.${category.id}`, category.title)
}

function serviceCategories(service: ServiceDetail): CategoryDetail[] {
  return props.categories.filter((category) =>
    category.services.includes(service.id),
  )
}

function categoryNames(service: ServiceDetail): string {
  const categories = serviceCategories(service)
  return categories.length === 0
    ? t('servicePicker.other')
    : categories.map(categoryLabel).join(', ')
}

function included(serviceID: string): boolean {
  return serviceIncluded(props.modelValue, props.categories, serviceID)
}

const visibleSelected = computed(
  () => orderedRows.value.filter((row) => included(row.id)).length,
)
const allVisibleSelected = computed(
  () =>
    orderedRows.value.length > 0 &&
    visibleSelected.value === orderedRows.value.length,
)
function toggleVisible(): void {
  if (props.disabled) return
  const select = !allVisibleSelected.value
  let next = props.modelValue
  for (const row of orderedRows.value) {
    if (serviceIncluded(next, props.categories, row.id) !== select)
      next = toggleCompositionService(next, props.categories, row.id)
  }
  emit('update:modelValue', next)
}

function toggleService(serviceID: string): void {
  emit(
    'update:modelValue',
    toggleCompositionService(props.modelValue, props.categories, serviceID),
  )
}

function toggleCategoryReference(follow: boolean): void {
  const category = selectedCategory.value
  if (category === null) return
  emit(
    'update:modelValue',
    setCompositionCategoryReference(
      props.modelValue,
      props.categories,
      category.id,
      follow,
    ),
  )
}

function openService(serviceID: string): void {
  inspectWorkspace(true, () => {
    activeServiceID.value = serviceID
  })
}

function closeService(): void {
  inspectWorkspace(false, () => {
    activeServiceID.value = ''
  })
}

function onDialogInclude(add: boolean): void {
  const service = activeService.value
  if (service === null || included(service.id) === add) return
  toggleService(service.id)
}

function overlapCount(serviceID: string): string {
  const count = formatNumber(overlapTitles(serviceID)?.length ?? 0)
  return props.forecast?.incompleteServices?.length
    ? t('forecast.partial.count', { n: count })
    : count
}

function ruleLabel(serviceID: string): string {
  const count = ruleCounts.value.get(serviceID)
  return count === undefined
    ? t(
        props.forecastPending && resolvedSet.value.has(serviceID)
          ? 'servicePicker.rules.pending'
          : 'servicePicker.rules.unknown',
      )
    : tc('create.forecast.rules', count)
}

function overlapTitles(serviceID: string): string[] | null {
  if (
    !resolvedSet.value.has(serviceID) ||
    props.forecastFailure ||
    props.overlapUnavailable
  )
    return null
  const ids = overlapServiceIDs(props.forecast, serviceID)
  if (ids === null) return null
  return ids
    .filter((id) => resolvedSet.value.has(id))
    .map((id) => serviceLabels.value.get(id) ?? id)
}

function serviceLabel(serviceID: string): string {
  return serviceLabels.value.get(serviceID) ?? serviceID
}
</script>

<template>
  <div
    class="picker"
    :class="{ 'picker--disabled': disabled, 'picker--fill': fill }"
  >
    <div class="picker__controls">
      <CategoryFilters
        v-model="categoryFilter"
        v-model:query="query"
        :categories="categories"
        :services="services"
        :disabled="disabled"
      />
      <div class="picker__summary">
        <RvButton
          size="compact"
          variant="quiet"
          :disabled="disabled || forecastPending || resolved.length === 0"
          @click="emit('refresh')"
        >
          {{ t('serviceCard.refresh') }}
        </RvButton>
        <RvMenu
          v-if="selectedCategory"
          :disabled="disabled"
          :label="
            t('servicePicker.follow', {
              category: categoryLabel(selectedCategory),
            })
          "
          :trigger-text="
            t(
              followsSelectedCategory
                ? 'servicePicker.future.auto'
                : 'servicePicker.future.manual',
            )
          "
          :items="[
            { key: 'auto', label: t('servicePicker.future.enable') },
            { key: 'manual', label: t('servicePicker.future.disable') },
          ]"
          @select="toggleCategoryReference($event === 'auto')"
        />
        <RvInfoTip
          v-if="forecast?.incompleteServices?.length"
          :label="t('forecast.partial.missing')"
          :text="forecast.incompleteServices.map(serviceLabel).join(', ')"
        />
        <span role="status">{{ tc('create.resolved', resolved.length) }}</span>
        <div class="picker__forecast-status" role="status">
          <span>{{ overlapMessage || t('list.priority.body') }}</span>
          <RvButton
            v-if="
              overlapMessage !== '' &&
              !overlapUnavailable &&
              !forecastPending &&
              retryable
            "
            size="compact"
            @click="emit('retry')"
            >{{ t('action.retry') }}</RvButton
          >
        </div>

        <RvInfoTip
          :label="t('servicePicker.column.overlaps')"
          :text="t('servicePicker.overlap.legend')"
        />
      </div>
    </div>
    <div class="picker__table-frame">
      <table class="picker__table">
        <thead>
          <tr>
            <th class="picker__priority-column" scope="col">
              <span class="picker__visually-hidden">{{
                t('servicePicker.column.priority')
              }}</span>
            </th>
            <th class="picker__choice-heading" scope="col">
              <input
                type="checkbox"
                class="picker__checkbox"
                :checked="allVisibleSelected"
                :indeterminate="visibleSelected > 0 && !allVisibleSelected"
                :disabled="disabled || orderedRows.length === 0"
                :aria-label="t('servicePicker.selectVisible')"
                @change="toggleVisible"
              />
            </th>
            <th scope="col">
              {{ t('servicePicker.column.list')
              }}<span class="picker__mobile-rules">{{
                t('servicePicker.column.rules')
              }}</span>
            </th>
            <th class="picker__category-column" scope="col">
              {{ t('servicePicker.column.category') }}
            </th>
            <th class="picker__rules-column" scope="col">
              {{ t('servicePicker.column.rules') }}
            </th>
            <th class="picker__overlaps-column" scope="col">
              {{ t('servicePicker.column.overlaps') }}
            </th>
            <th class="picker__action-heading" scope="col">
              <span class="picker__visually-hidden">{{
                t('servicePicker.column.open')
              }}</span>
            </th>
          </tr>
        </thead>
        <tbody ref="tableBody">
          <tr
            v-for="service in orderedRows"
            :key="service.id"
            :data-id="service.id"
            :data-priority="
              included(service.id)
                ? resolved.indexOf(service.id) + 1
                : undefined
            "
            class="picker__row"
            :class="{
              'picker__row--selected': included(service.id),
              'picker__row--inspected': activeServiceID === service.id,
            }"
          >
            <td class="picker__priority-column">
              <button
                class="picker__handle"
                type="button"
                :disabled="disabled || !included(service.id)"
                :aria-label="
                  t(
                    included(service.id)
                      ? 'list.priority.move.aria'
                      : 'list.priority.unselected',
                    {
                      list: serviceLabel(service.id),
                      position: resolved.indexOf(service.id) + 1,
                      total: resolved.length,
                    },
                  )
                "
                @keydown.up.prevent="movePriority(service.id, -1)"
                @keydown.down.prevent="movePriority(service.id, 1)"
                @keydown.home.prevent="
                  movePriority(service.id, -resolved.indexOf(service.id))
                "
                @keydown.end.prevent="
                  movePriority(
                    service.id,
                    resolved.length - resolved.indexOf(service.id) - 1,
                  )
                "
              >
                <RvIcon name="drag" />
              </button>
            </td>
            <td>
              <input
                :aria-label="
                  t(
                    included(service.id)
                      ? 'servicePicker.exclude.aria'
                      : 'servicePicker.include.aria',
                    {
                      list: serviceLabel(service.id),
                    },
                  )
                "
                :checked="included(service.id)"
                class="picker__checkbox"
                :disabled="disabled"
                type="checkbox"
                :value="service.id"
                @change="toggleService(service.id)"
              />
            </td>
            <th scope="row">
              <strong class="picker__name">{{
                serviceLabel(service.id)
              }}</strong>
              <span
                class="picker__mobile-rules"
                :aria-label="ruleLabel(service.id)"
                >{{
                  ruleCounts.has(service.id)
                    ? formatNumber(ruleCounts.get(service.id)!)
                    : '—'
                }}</span
              >
              <span class="picker__mobile-meta">{{
                categoryNames(service)
              }}</span>
              <span
                v-for="title in overlapTitles(service.id) ?? []"
                :key="`mobile-${service.id}-${title}`"
                class="picker__tag picker__tag--mobile"
              >
                {{ t('servicePicker.overlap.tag', { list: title }) }}
              </span>
            </th>
            <td class="picker__category-column">
              <CategoryLabel
                v-for="category in serviceCategories(service)"
                :id="category.id"
                :key="category.id"
                :label="categoryLabel(category)"
              />
              <span v-if="serviceCategories(service).length === 0">{{
                t('servicePicker.other')
              }}</span>
            </td>
            <td
              class="picker__rules-column"
              :aria-label="ruleLabel(service.id)"
              :title="ruleLabel(service.id)"
            >
              {{
                ruleCounts.has(service.id)
                  ? formatNumber(ruleCounts.get(service.id)!)
                  : '—'
              }}
            </td>
            <td class="picker__overlaps-column">
              <RvInfoTip
                v-if="(overlapTitles(service.id)?.length ?? 0) > 0"
                numeric
                :label="
                  t('servicePicker.overlap.tag', {
                    list: overlapTitles(service.id)?.join(', ') ?? '',
                  })
                "
                :text="overlapTitles(service.id)?.join(', ') ?? ''"
                >{{ overlapCount(service.id) }}</RvInfoTip
              >
              <span
                v-if="
                  !included(service.id) || overlapTitles(service.id) === null
                "
                class="picker__unknown"
              >
                <span aria-hidden="true">—</span>
                <span class="picker__visually-hidden">
                  {{
                    overlapUnavailable && overlapUnavailableLabel
                      ? overlapUnavailableLabel
                      : t(
                          overlapUnavailable
                            ? 'servicePicker.overlap.unavailable'
                            : forecastPending
                              ? 'servicePicker.overlap.pending'
                              : 'servicePicker.overlap.unknown',
                        )
                  }}
                </span>
              </span>
              <span
                v-else-if="overlapTitles(service.id)?.length === 0"
                class="picker__unknown"
              >
                {{ overlapCount(service.id) }}
              </span>
            </td>
            <td>
              <button
                :aria-label="
                  t('serviceDetail.open.aria', {
                    service: serviceLabel(service.id),
                  })
                "
                class="picker__open"
                :disabled="disabled"
                type="button"
                @click="openService(service.id)"
              >
                <RvIcon name="chevron" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>

      <p v-if="orderedRows.length === 0" class="picker__empty">
        {{ t('create.noMatches', { query: query.trim() }) }}
      </p>
    </div>

    <ServiceDetailDialog
      :disabled="disabled"
      :included="activeService === null ? false : included(activeService.id)"
      :list-name="props.listName"
      mode="compose"
      :pending="props.pending"
      :service="activeService"
      @close="closeService"
      @include="onDialogInclude"
    />
  </div>
</template>

<style scoped src="./ServicePicker.css"></style>
