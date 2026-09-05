<script setup lang="ts">
import { useSortable } from '@vueuse/integrations/useSortable'
import { computed, ref, useId, useTemplateRef, watch } from 'vue'

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
import type { ChoiceOption } from '@/shared/ui/kinds'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'

import ServiceDetailDialog from './ServiceDetailDialog.vue'

const props = defineProps<{
  modelValue: ListComposition
  services: ServiceDetail[]
  categories: CategoryDetail[]
  disabled?: boolean
  listName?: string
  pending?: boolean
  forecast?: TargetForecast | null
  forecastPending?: boolean
  overlapUnavailable?: boolean
  overlapUnavailableLabel?: string
  retryable?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: ListComposition]
  reorder: [ids: string[]]
  retry: []
}>()

const { formatNumber, t, tc, tor } = useLocale()
const query = ref('')
const categoryFilter = ref('all')
const overflowFilter = computed({
  get: () =>
    filterOptions.value
      .slice(0, 6)
      .some((option) => option.value === categoryFilter.value)
      ? ''
      : categoryFilter.value,
  set: (value: string) => {
    categoryFilter.value = value
  },
})
const activeServiceID = ref('')
const baseId = useId()
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
const claimed = computed(
  () => new Set(props.categories.flatMap((category) => category.services)),
)

const filterOptions = computed<ChoiceOption[]>(() => {
  const options = [
    { label: t('servicePicker.filter.all'), value: 'all' },
    ...props.categories.map((category) => ({
      label: categoryLabel(category),
      value: category.id,
    })),
  ]
  if (props.services.some((service) => !claimed.value.has(service.id))) {
    options.push({ label: t('servicePicker.other'), value: uncategorizedID })
  }
  return options
})

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
    const ids = rows.map((row) => row.id)
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
  handle: '.picker__handle',
  watchElement: true,
  onStart: (event) => {
    draggedID = event.item.dataset.id ?? ''
  },
})
watch(
  () => props.disabled,
  (disabled) => sortable.option('disabled', disabled),
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
  return resolved.value.some((id) => overlapTitles(id) === null)
    ? t('servicePicker.overlap.unknown')
    : ''
})
const categoryTones: Record<string, string> = {
  ai: 'violet',
  'ai-tools': 'cyan',
  development: 'amber',
  education: 'green',
  games: 'pink',
  google: 'blue',
  infrastructure: 'slate',
  music: 'pink',
  video: 'red',
  messengers: 'cyan',
  socials: 'violet',
  work: 'blue',
  torrents: 'amber',
}
function categoryTone(id: string): string {
  return (
    categoryTones[id] ??
    ['violet', 'cyan', 'amber', 'green', 'pink', 'blue'][
      [...id].reduce((value, char) => value + char.codePointAt(0)!, 0) % 6
    ]!
  )
}
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

function toggleService(serviceID: string): void {
  emit(
    'update:modelValue',
    toggleCompositionService(props.modelValue, props.categories, serviceID),
  )
}

function toggleCategoryReference(event: Event): void {
  const category = selectedCategory.value
  if (category === null) return
  emit(
    'update:modelValue',
    setCompositionCategoryReference(
      props.modelValue,
      props.categories,
      category.id,
      (event.currentTarget as HTMLInputElement).checked,
    ),
  )
}

function openService(serviceID: string): void {
  activeServiceID.value = serviceID
}

function closeService(): void {
  activeServiceID.value = ''
}

function onDialogInclude(add: boolean): void {
  const service = activeService.value
  if (service === null || included(service.id) === add) return
  toggleService(service.id)
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
  if (!resolvedSet.value.has(serviceID)) return null
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
  <div class="picker" :class="{ 'picker--disabled': disabled }">
    <div class="picker__toolbar">
      <label class="picker__search">
        <RvIcon name="search" />
        <span class="picker__visually-hidden">{{ t('create.search') }}</span>
        <input
          v-model="query"
          class="picker__search-input"
          :disabled="disabled"
          :placeholder="t('create.search')"
          type="search"
        />
      </label>
    </div>
    <div
      class="picker__filters"
      :aria-label="t('servicePicker.filter.label')"
      role="group"
    >
      <div class="picker__quick-filters">
        <button
          v-for="option in filterOptions.slice(0, 6)"
          :key="option.value"
          type="button"
          class="picker__chip"
          :aria-pressed="categoryFilter === option.value"
          :disabled="disabled"
          @click="categoryFilter = option.value"
        >
          {{ option.label }}
          <small v-if="categoryByID.has(option.value)">{{
            categoryByID.get(option.value)?.services.length
          }}</small>
        </button>
      </div>
      <label :for="baseId + '-category'" class="picker__visually-hidden">{{
        t('servicePicker.filter.label')
      }}</label>
      <div class="picker__filter">
        <RvSelect
          v-model="overflowFilter"
          :disabled="disabled"
          size="compact"
          :input-id="baseId + '-category'"
          :options="filterOptions"
          :placeholder="t('servicePicker.filter.more')"
        />
      </div>
    </div>

    <label v-if="selectedCategory !== null" class="picker__category-reference">
      <input
        :checked="followsSelectedCategory"
        class="picker__checkbox"
        :disabled="disabled"
        type="checkbox"
        @change="toggleCategoryReference"
      />
      <span>
        <strong>{{
          t('servicePicker.follow', {
            category: categoryLabel(selectedCategory),
          })
        }}</strong>
        <small>{{
          tc('servicePicker.follow.members', selectedCategory.services.length)
        }}</small>
      </span>
    </label>

    <div class="picker__summary">
      <span role="status">{{ tc('create.resolved', resolved.length) }}</span>
      <span>{{ t('list.priority.body') }}</span>
      <RvInfoTip
        :label="t('servicePicker.column.overlaps')"
        :text="t('servicePicker.overlap.legend')"
      />
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
              <span class="picker__visually-hidden">{{
                t('servicePicker.column.include')
              }}</span>
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
            :class="{ 'picker__row--selected': included(service.id) }"
          >
            <td class="picker__priority-column">
              <button
                v-if="included(service.id)"
                class="picker__handle"
                type="button"
                :disabled="disabled"
                :aria-label="
                  t('list.priority.move.aria', {
                    list: serviceLabel(service.id),
                    position: resolved.indexOf(service.id) + 1,
                    total: resolved.length,
                  })
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
                <RvIcon name="drag" /><span aria-hidden="true">{{
                  resolved.indexOf(service.id) + 1
                }}</span>
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
              <span
                v-for="category in serviceCategories(service)"
                :key="category.id"
                class="picker__category"
              >
                <span
                  class="picker__category-dot"
                  :data-tone="categoryTone(category.id)"
                  aria-hidden="true"
                />{{ categoryLabel(category) }}
              </span>
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
                :label="
                  t('servicePicker.overlap.tag', {
                    list: overlapTitles(service.id)?.join(', ') ?? '',
                  })
                "
                :text="overlapTitles(service.id)?.join(', ') ?? ''"
                >{{
                  formatNumber(overlapTitles(service.id)?.length ?? 0)
                }}</RvInfoTip
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
                0
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

    <div class="picker__forecast-status" role="status">
      <span>{{ overlapMessage }}</span>
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
