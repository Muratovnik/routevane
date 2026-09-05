<script setup lang="ts">
import { computed, ref, useId } from 'vue'

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
}>()

const emit = defineEmits<{
  'update:modelValue': [value: ListComposition]
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

const selectedRows = computed(() => {
  const visible = new Map(
    visibleRows.value.map((service) => [service.id, service]),
  )
  return resolved.value
    .map((id) => visible.get(id))
    .filter((service): service is ServiceDetail => service !== undefined)
})
const availableRows = computed(() =>
  visibleRows.value.filter((service) => !resolvedSet.value.has(service.id)),
)
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

    <div class="picker__table-frame">
      <table class="picker__table">
        <thead>
          <tr>
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
        <tbody v-if="selectedRows.length > 0">
          <tr class="picker__group-row">
            <th colspan="6" scope="rowgroup">
              {{ t('servicePicker.group.selected') }}
              <small>{{ formatNumber(selectedRows.length) }}</small>
            </th>
          </tr>
          <tr
            v-for="service in selectedRows"
            :key="`selected-${service.id}`"
            class="picker__row picker__row--selected"
          >
            <td>
              <input
                :aria-label="
                  t('servicePicker.exclude.aria', {
                    list: serviceLabel(service.id),
                  })
                "
                checked
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
              {{ categoryNames(service) }}
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
                v-if="overlapTitles(service.id) === null"
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
                {{ t('servicePicker.overlap.none') }}
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
        <tbody v-if="availableRows.length > 0">
          <tr class="picker__group-row">
            <th colspan="6" scope="rowgroup">
              {{ t('servicePicker.group.available') }}
              <small>{{ formatNumber(availableRows.length) }}</small>
            </th>
          </tr>
          <tr
            v-for="service in availableRows"
            :key="`available-${service.id}`"
            class="picker__row"
          >
            <td>
              <input
                :aria-label="
                  t('servicePicker.include.aria', {
                    list: serviceLabel(service.id),
                  })
                "
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
            </th>
            <td class="picker__category-column">
              {{ categoryNames(service) }}
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
            <td class="picker__overlaps-column picker__unknown">—</td>
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

      <p
        v-if="selectedRows.length === 0 && availableRows.length === 0"
        class="picker__empty"
      >
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
