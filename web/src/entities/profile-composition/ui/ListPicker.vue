<script setup lang="ts">
import { useWorkspaceInspection } from '@/shared/model/useWorkspacePane'
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
import type {
  CategoryDetail,
  ListDetail,
  ProfileComposition,
  TargetForecast,
} from '../model/types'
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
    <!-- The frame the table scrolls inside. It has no role and no name of its
         own, and a test reads the geometry and the scroll extent it owns, so
         it carries a test hook. -->
    <div class="picker__table-frame" data-testid="rv-list-picker-frame">
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

<style scoped>
.picker {
  display: grid;
  gap: var(--rv-space-3);
  min-width: 0;
  container-type: inline-size;
  position: relative;
}

.picker__table-frame {
  position: relative;
  min-width: 0;
  min-height: var(--rv-picker-height);
  max-height: var(--rv-picker-height);
  overflow: auto;
  overscroll-behavior: contain;
  background: var(--rv-color-canvas);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-md);
}

.picker__table {
  width: 100%;
  border-collapse: collapse;
  table-layout: fixed;
  color: var(--rv-color-ink);
  font-size: var(--rv-text-dense);
}

.picker__table thead {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--rv-color-surface);
}

.picker__table th,
.picker__table td {
  padding: 0 var(--rv-space-3);
  height: var(--rv-control-default);
  text-align: start;
  vertical-align: middle;
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.picker__table .picker__choice-heading,
.picker__table .picker__action-heading,
.picker__row > td:nth-child(2),
.picker__row > td:last-child {
  width: var(--rv-table-action-width);
  padding-inline: var(--rv-space-2);
}

.picker__table thead th {
  color: var(--rv-color-ink-muted);
  font-weight: 600;
  white-space: nowrap;
}

.picker__row--selected {
  background: var(--rv-color-surface-muted);
}

.picker:not(.picker--disabled) .picker__row:hover {
  background: var(--rv-color-surface-hover);
}

.picker__row th[scope='row'] {
  min-width: var(--rv-picker-name-width);
  font-weight: 600;
}

.picker__row--inspected {
  box-shadow: inset var(--rv-border-mark) 0 var(--rv-color-accent);
}

.picker__name {
  display: block;
  font-weight: 400;
}

.picker__table th:nth-child(3) {
  width: var(--rv-catalog-name-column);
}

.picker__category-column {
  color: var(--rv-color-ink-muted);
}

.picker__rules-column {
  width: var(--rv-picker-rule-width);
  color: var(--rv-color-ink-muted);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.picker__overlaps-column {
  width: var(--rv-picker-overlap-width);
}

.picker__column-label {
  display: inline-flex;
  align-items: center;
  gap: var(--rv-space-1);
}

.picker__tag {
  display: inline-flex;
  margin: var(--rv-space-1) var(--rv-space-1) var(--rv-space-1) 0;
  padding: var(--rv-space-1) var(--rv-space-2);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-meta);
  background: var(--rv-color-surface-selected);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-lg);
}

.picker__tag--mobile,
.picker__mobile-rules,
.picker__mobile-meta {
  display: none;
}

.picker__unknown {
  color: var(--rv-color-ink-tertiary);
  font-variant-numeric: tabular-nums;
}

.picker__checkbox {
  color-scheme: var(--rv-native-color-scheme);
  width: var(--rv-control-choice);
  height: var(--rv-control-choice);
  margin: 0;
  accent-color: var(--rv-color-accent);
}

.picker__open {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: var(--rv-control-compact);
  height: var(--rv-control-compact);
  padding: 0;
  color: var(--rv-color-ink-muted);
  font: inherit;
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.picker__open .rv-icon {
  transform: rotate(-90deg);
}

.picker__open:disabled {
  cursor: not-allowed;
  color: var(--rv-color-ink-tertiary);
  opacity: var(--rv-disabled-opacity);
}

.picker__open:focus-visible,
.picker__checkbox:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-border-hair);
}

.picker__open:hover:not(:disabled) {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-hover);
}

.picker__empty {
  padding: var(--rv-space-8) var(--rv-space-4);
  color: var(--rv-color-ink-muted);
  text-align: center;
}

.picker__visually-hidden {
  position: absolute;
  width: 0.0625rem;
  height: 0.0625rem;
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}

@container (width <= 42rem) {
  .picker__table {
    table-layout: auto;
  }

  .picker__category-column {
    display: none;
  }

  .picker__mobile-meta {
    display: block;
    color: var(--rv-color-ink-tertiary);
    font-weight: 400;
    font-size: var(--rv-text-meta);
  }
}

@container (width <= 26rem) {
  .picker__table th:nth-child(3) {
    width: auto;
  }

  .picker__rules-column {
    display: none;
  }

  .picker__mobile-rules {
    display: block;
    float: inline-end;
    margin-inline-start: var(--rv-space-2);
    color: var(--rv-color-ink-muted);
    font-weight: 400;
    font-variant-numeric: tabular-nums;
  }

  .picker__name {
    display: inline;
  }

  .picker__mobile-meta {
    clear: both;
  }

  .picker__overlaps-column {
    display: none;
  }

  .picker__tag--mobile {
    display: inline-flex;
  }

  .picker__table-frame {
    position: relative;
    min-height: var(--rv-picker-mobile-height);
    max-height: var(--rv-picker-mobile-height);
  }

  .picker__row th[scope='row'] {
    min-width: 0;
    overflow-wrap: anywhere;
  }
}

.picker__summary {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--rv-space-2) var(--rv-space-4);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-meta);
}

.picker__forecast-status {
  flex: 1 1 var(--rv-composer-field-width);
  min-width: 0;
  display: flex;
  align-items: center;
  gap: var(--rv-space-3);
  min-height: var(--rv-control-touch);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-meta);
}

.picker__table .picker__priority-column {
  width: var(--rv-picker-priority-width);
  padding-inline: var(--rv-space-1);
}

.picker__handle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--rv-space-1);
  width: 100%;
  height: var(--rv-control-compact);
  padding: 0;
  color: var(--rv-color-accent-ink);
  font: inherit;
  font-size: var(--rv-text-meta);
  font-variant-numeric: tabular-nums;
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: grab;
  touch-action: none;
}

.picker__handle:active {
  cursor: grabbing;
}

.picker__handle:disabled {
  cursor: not-allowed;
  color: var(--rv-color-ink-tertiary);
  opacity: var(--rv-disabled-opacity);
}

.picker__handle:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: calc(-1 * var(--rv-border-mark));
}

.picker__row.sortable-ghost {
  opacity: var(--rv-disabled-opacity);
}

.picker__controls {
  display: grid;
  gap: var(--rv-space-3);
  min-width: 0;
}

.picker--fill {
  /* A stacking host may restore a bounded natural-height table. */
  height: var(--rv-picker-fill-height);
  min-height: 0;
  grid-template-rows: auto minmax(0, 1fr);
}

.picker--fill .picker__table-frame {
  min-height: var(--rv-picker-fill-min);
  max-height: var(--rv-picker-fill-max);
}

@media (width <= 64rem), (height <= 36rem) {
  .picker--fill {
    height: auto;
  }

  .picker--fill .picker__table-frame {
    min-height: var(--rv-picker-mobile-height);
    max-height: var(--rv-picker-mobile-height);
  }
}

/* The fallback is portalled outside its table; retain table column geometry. */
.picker__row.sortable-fallback {
  display: table;
  table-layout: fixed;
  border-collapse: collapse;
  background: var(--rv-color-surface-muted);
}

.picker__partial {
  display: inline-flex;
  align-items: center;
  gap: var(--rv-space-1);
}
</style>
