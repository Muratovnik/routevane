<script setup lang="ts">
import { useWorkspaceInspection } from '@/shared/ui/workspacePane'
import { useSortable } from '@vueuse/integrations/useSortable'
import { computed, ref, useTemplateRef, watch } from 'vue'

import {
  overlapListIDs,
  resolvedComposition,
  listIdentityLabels,
  listIncluded,
  setCompositionCategoryReference,
  toggleCompositionList,
} from '@/entities/profile-composition/model/composition'
import type { CategoryDetail, ListDetail } from '@/shared/api/catalog'
import type { ProfileComposition, TargetForecast } from '@/shared/api/profiles'
import { useLocale } from '@/shared/i18n/useLocale'
import { tableDragGeometry } from '@/shared/lib/tableDrag'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvMenu from '@/shared/ui/RvMenu.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import CategoryFilters from './CategoryFilters.vue'
import { matchesCategories } from '../model/categoryFilter'
import CategoryLabel from './CategoryLabel.vue'

import ListDetailDialog from './ListDetailDialog.vue'

const inspectWorkspace = useWorkspaceInspection()
const props = defineProps<{
  initialPriority?: string[]
  fill?: boolean
  modelValue: ProfileComposition
  lists: ListDetail[]
  categories: CategoryDetail[]
  disabled?: boolean
  profileName?: string
  pending?: boolean
  forecast?: TargetForecast | null
  forecastPending?: boolean
  refreshing?: boolean
  forecastFailure?: string
  overlapUnavailable?: boolean
  overlapUnavailableLabel?: string
  retryable?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: ProfileComposition]
  reorder: [ids: string[]]
  refresh: []
  retry: []
}>()

const { formatNumber, t, tc, tor } = useLocale()
const query = ref('')
const categoryFilter = ref<string[]>([])
const activeListID = ref('')

const listsByID = computed(
  () => new Map(props.lists.map((list) => [list.id, list])),
)
const listLabels = computed(() => listIdentityLabels(props.lists))
const resolved = computed(() =>
  resolvedComposition(props.modelValue, props.categories),
)
const resolvedSet = computed(() => new Set(resolved.value))
const categoryByID = computed(
  () => new Map(props.categories.map((category) => [category.id, category])),
)
const selectedCategory = computed(() =>
  categoryFilter.value.length === 1
    ? (categoryByID.value.get(categoryFilter.value[0]!) ?? null)
    : null,
)
const followsSelectedCategory = computed(
  () =>
    selectedCategory.value !== null &&
    props.modelValue.categories.includes(selectedCategory.value.id),
)

const allRows = computed<ListDetail[]>(() => {
  const rows = [...props.lists]
  for (const id of resolved.value) {
    if (!listsByID.value.has(id)) rows.push({ categories: [], id, title: id })
  }
  return rows
})

const visibleRows = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase()
  return allRows.value.filter((list) => {
    const categories = listCategories(list)
    const inCategory = matchesCategories(
      list.id,
      categoryFilter.value,
      props.categories,
    )
    if (!inCategory) return false
    if (needle === '') return true
    return [
      list.id,
      list.title,
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
      ...new Set(props.categories.flatMap((category) => category.lists)),
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

const movePriority = (id: string, offset: number): void => {
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
    return props.overlapUnavailableLabel ?? t('listPicker.overlap.unavailable')
  if (props.forecastPending) return t('listPicker.overlap.pending')
  if (props.forecastFailure) return props.forecastFailure
  if (props.forecast?.incompleteLists?.length) return ''
  return resolved.value.some((id) => overlapTitles(id) === null)
    ? t('listPicker.overlap.unknown')
    : ''
})
const activeList = computed(
  () => listsByID.value.get(activeListID.value) ?? null,
)
const ruleCounts = computed(
  () =>
    new Map(
      (props.forecast?.perList ?? []).map((row) => [row.listID, row.rules]),
    ),
)

const categoryLabel = (category: CategoryDetail): string =>
  tor(`category.${category.id}`, category.title)

const listCategories = (list: ListDetail): CategoryDetail[] =>
  props.categories.filter((category) => category.lists.includes(list.id))

const categoryNames = (list: ListDetail): string => {
  const categories = listCategories(list)
  return categories.length === 0
    ? t('listPicker.other')
    : categories.map(categoryLabel).join(', ')
}

const included = (listID: string): boolean =>
  listIncluded(props.modelValue, props.categories, listID)

const visibleSelected = computed(
  () => orderedRows.value.filter((row) => included(row.id)).length,
)
const allVisibleSelected = computed(
  () =>
    orderedRows.value.length > 0 &&
    visibleSelected.value === orderedRows.value.length,
)
const toggleVisible = (): void => {
  if (props.disabled) return
  const select = !allVisibleSelected.value
  let next = props.modelValue
  for (const row of orderedRows.value) {
    if (listIncluded(next, props.categories, row.id) !== select)
      next = toggleCompositionList(next, props.categories, row.id)
  }
  emit('update:modelValue', next)
}

const toggleList = (listID: string): void => {
  emit(
    'update:modelValue',
    toggleCompositionList(props.modelValue, props.categories, listID),
  )
}

const toggleCategoryReference = (follow: boolean): void => {
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

const openList = (listID: string): void => {
  inspectWorkspace(true, () => {
    activeListID.value = listID
  })
}

const closeList = (): void => {
  inspectWorkspace(false, () => {
    activeListID.value = ''
  })
}

const onDialogInclude = (add: boolean): void => {
  const list = activeList.value
  if (list === null || included(list.id) === add) return
  toggleList(list.id)
}

const overlapCount = (listID: string): string => {
  const count = formatNumber(overlapTitles(listID)?.length ?? 0)
  return count
}

const ruleLabel = (listID: string): string => {
  const count = ruleCounts.value.get(listID)
  if (count !== undefined) return tc('create.forecast.rules', count)
  const pending = props.forecastPending && resolvedSet.value.has(listID)
  return t(pending ? 'listPicker.rules.pending' : 'listPicker.rules.unknown')
}

const overlapTitles = (listID: string): string[] | null => {
  if (
    !resolvedSet.value.has(listID) ||
    props.forecastFailure ||
    props.overlapUnavailable
  )
    return null
  const ids = overlapListIDs(props.forecast, listID)
  if (ids === null) return null
  return ids
    .filter((id) => resolvedSet.value.has(id))
    .map((id) => listLabels.value.get(id) ?? id)
}

const listLabel = (listID: string): string =>
  listLabels.value.get(listID) ?? listID
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
        :lists="lists"
        :disabled="disabled"
      />
      <div class="picker__summary">
        <RvButton
          size="compact"
          :loading="refreshing"
          :disabled="disabled || resolved.length === 0"
          @click="emit('refresh')"
        >
          {{ t('listCard.refresh') }}
        </RvButton>
        <RvMenu
          v-if="selectedCategory"
          :disabled="disabled"
          :label="
            t('listPicker.follow', {
              category: categoryLabel(selectedCategory),
            })
          "
          :trigger-text="
            t(
              followsSelectedCategory
                ? 'listPicker.future.auto'
                : 'listPicker.future.manual',
            )
          "
          :items="[
            { key: 'auto', label: t('listPicker.future.enable') },
            { key: 'manual', label: t('listPicker.future.disable') },
          ]"
          @select="toggleCategoryReference($event === 'auto')"
        />
        <span role="status">{{ tc('create.resolved', resolved.length) }}</span>
        <div class="picker__forecast-status" role="status">
          <span>{{ overlapMessage }}</span>
          <span
            v-if="
              forecast?.incompleteLists?.length &&
              !forecastPending &&
              !forecastFailure
            "
            class="picker__partial"
          >
            {{ t('forecast.partial.target') }}
            <RvInfoTip
              :label="t('forecast.partial.missing')"
              :text="t('forecast.partial.explanation')"
              :items="forecast.incompleteLists.map(listLabel)"
            />
          </span>
          <RvButton
            v-if="
              overlapMessage !== '' &&
              !overlapUnavailable &&
              !forecastPending &&
              retryable
            "
            size="compact"
            @click="emit('retry')"
            >{{ t('forecast.recalculate') }}</RvButton
          >
        </div>
      </div>
    </div>
    <div class="picker__table-frame">
      <table class="picker__table">
        <thead>
          <tr>
            <th class="picker__priority-column" scope="col">
              <span class="picker__visually-hidden">{{
                t('listPicker.column.priority')
              }}</span>
            </th>
            <th class="picker__choice-heading" scope="col">
              <input
                type="checkbox"
                class="picker__checkbox"
                :checked="allVisibleSelected"
                :indeterminate="visibleSelected > 0 && !allVisibleSelected"
                :disabled="disabled || orderedRows.length === 0"
                :aria-label="t('listPicker.selectVisible')"
                @change="toggleVisible"
              />
            </th>
            <th scope="col">
              {{ t('listPicker.column.list')
              }}<span class="picker__mobile-rules">{{
                t('listPicker.column.rules')
              }}</span>
            </th>
            <th class="picker__category-column" scope="col">
              {{ t('listPicker.column.category') }}
            </th>
            <th class="picker__rules-column" scope="col">
              {{ t('listPicker.column.rules') }}
            </th>
            <th class="picker__overlaps-column" scope="col">
              <span class="picker__column-label">
                {{ t('listPicker.column.overlaps') }}
                <RvInfoTip
                  :label="t('listPicker.column.overlaps')"
                  :text="t('listPicker.overlap.legend')"
                />
              </span>
            </th>
            <th class="picker__action-heading" scope="col">
              <span class="picker__visually-hidden">{{
                t('listPicker.column.open')
              }}</span>
            </th>
          </tr>
        </thead>
        <tbody ref="tableBody">
          <tr
            v-for="list in orderedRows"
            :key="list.id"
            :data-id="list.id"
            :data-priority="
              included(list.id) ? resolved.indexOf(list.id) + 1 : undefined
            "
            class="picker__row"
            :class="{
              'picker__row--selected': included(list.id),
              'picker__row--inspected': activeListID === list.id,
            }"
          >
            <td class="picker__priority-column">
              <button
                class="picker__handle"
                type="button"
                :disabled="disabled || !included(list.id)"
                :aria-label="
                  t(
                    included(list.id)
                      ? 'profile.priority.move.aria'
                      : 'profile.priority.unselected',
                    {
                      list: listLabel(list.id),
                      position: resolved.indexOf(list.id) + 1,
                      total: resolved.length,
                    },
                  )
                "
                @keydown.up.prevent="movePriority(list.id, -1)"
                @keydown.down.prevent="movePriority(list.id, 1)"
                @keydown.home.prevent="
                  movePriority(list.id, -resolved.indexOf(list.id))
                "
                @keydown.end.prevent="
                  movePriority(
                    list.id,
                    resolved.length - resolved.indexOf(list.id) - 1,
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
                    included(list.id)
                      ? 'listPicker.exclude.aria'
                      : 'listPicker.include.aria',
                    {
                      list: listLabel(list.id),
                    },
                  )
                "
                :checked="included(list.id)"
                class="picker__checkbox"
                :disabled="disabled"
                type="checkbox"
                :value="list.id"
                @change="toggleList(list.id)"
              />
            </td>
            <th scope="row">
              <strong class="picker__name">{{ listLabel(list.id) }}</strong>
              <span
                class="picker__mobile-rules"
                :aria-label="ruleLabel(list.id)"
                >{{
                  ruleCounts.has(list.id)
                    ? formatNumber(ruleCounts.get(list.id)!)
                    : '—'
                }}</span
              >
              <span class="picker__mobile-meta">{{ categoryNames(list) }}</span>
              <span
                v-for="title in overlapTitles(list.id) ?? []"
                :key="`mobile-${list.id}-${title}`"
                class="picker__tag picker__tag--mobile"
              >
                {{ t('listPicker.overlap.tag', { list: title }) }}
              </span>
            </th>
            <td class="picker__category-column">
              <CategoryLabel
                v-for="category in listCategories(list)"
                :id="category.id"
                :key="category.id"
                :label="categoryLabel(category)"
              />
              <span v-if="listCategories(list).length === 0">{{
                t('listPicker.other')
              }}</span>
            </td>
            <td
              class="picker__rules-column"
              :aria-label="ruleLabel(list.id)"
              :title="ruleLabel(list.id)"
            >
              {{
                ruleCounts.has(list.id)
                  ? formatNumber(ruleCounts.get(list.id)!)
                  : '—'
              }}
            </td>
            <td class="picker__overlaps-column">
              <RvInfoTip
                v-if="(overlapTitles(list.id)?.length ?? 0) > 0"
                numeric
                :label="
                  t('listPicker.overlap.tag', {
                    list: overlapTitles(list.id)?.join(', ') ?? '',
                  })
                "
                :text="
                  t(
                    forecast?.incompleteLists?.length
                      ? 'forecast.partial.found'
                      : 'listPicker.overlap.heading',
                  )
                "
                :items="overlapTitles(list.id) ?? []"
                >{{ overlapCount(list.id) }}</RvInfoTip
              >
              <span
                v-if="!included(list.id) || overlapTitles(list.id) === null"
                class="picker__unknown"
              >
                <span aria-hidden="true">—</span>
                <span class="picker__visually-hidden">
                  {{
                    overlapUnavailable && overlapUnavailableLabel
                      ? overlapUnavailableLabel
                      : t(
                          overlapUnavailable
                            ? 'listPicker.overlap.unavailable'
                            : forecastPending
                              ? 'listPicker.overlap.pending'
                              : 'listPicker.overlap.unknown',
                        )
                  }}
                </span>
              </span>
              <span
                v-else-if="overlapTitles(list.id)?.length === 0"
                class="picker__unknown"
              >
                {{
                  forecast?.incompleteLists?.length
                    ? '—'
                    : overlapCount(list.id)
                }}
              </span>
            </td>
            <td>
              <button
                :aria-label="
                  t('listDetail.open.aria', {
                    list: listLabel(list.id),
                  })
                "
                class="picker__open"
                :disabled="disabled"
                type="button"
                @click="openList(list.id)"
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

    <ListDetailDialog
      :disabled="disabled"
      :included="activeList === null ? false : included(activeList.id)"
      :profile-name="props.profileName"
      mode="compose"
      :pending="props.pending"
      :list="activeList"
      @close="closeList"
      @include="onDialogInclude"
    />
  </div>
</template>

<style scoped src="./ListPicker.css"></style>
