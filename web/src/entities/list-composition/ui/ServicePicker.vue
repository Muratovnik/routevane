<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'

import {
  categorySelectionState,
  resolvedComposition,
  serviceIncluded,
  toggleCompositionCategory,
  toggleCompositionService,
} from '@/entities/list-composition/model/composition'
import type { CategoryDetail, ServiceDetail } from '@/shared/api/catalog'
import type { ListComposition } from '@/shared/api/lists'
import { useLocale } from '@/shared/i18n/useLocale'
import RvIcon from '@/shared/ui/RvIcon.vue'

import ServiceDetailDialog from './ServiceDetailDialog.vue'

/**
 * Choosing what a route carries (ADR 0028, ADR 0029).
 *
 * The operator sees two objects: a **category**, and the **lists** inside it. A
 * list belonging to no category is not a second surface — it is the last
 * category row, computed here rather than stored, because a list is found by
 * search or by its category and nowhere else.
 *
 * Selection is a checkbox and nothing else. What a category holds, what a list
 * holds, and whether either exists at all are the library's subject and are
 * edited in «Списки»; this surface writes nothing but the draft it was handed.
 */
const props = defineProps<{
  modelValue: ListComposition
  services: ServiceDetail[]
  categories: CategoryDetail[]
  disabled?: boolean
  // The route the card's footer is talking about. A draft may not be named yet,
  // so the card is prepared for a blank one rather than promised a name.
  listName?: string
  // Whether that route is still an unsaved draft, which is the only case where
  // the card's footer says membership is waiting on a save.
  pending?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: ListComposition]
}>()

// The row for lists no category claims. It is computed from the catalog, so it
// carries an identity no server object can collide with.
const uncategorizedID = 'rv:uncategorized'

type CategoryRow = {
  id: string
  label: string
  // The category this row stands for, or null for the computed last row: the
  // difference decides how selecting the row is stored.
  category: CategoryDetail | null
  // Members after the search filter — what the detail pane draws.
  members: ServiceDetail[]
  // Members before it, because a checkbox selects the whole category and a
  // count states the whole category, whatever the field is narrowing.
  all: ServiceDetail[]
  included: number
  state: 'none' | 'partial' | 'all'
  preview: string
}

const { t, tc, tor } = useLocale()
const query = ref('')
const activeCategoryID = ref('')
const activeServiceID = ref('')
const mobilePane = ref<'collections' | 'details'>('collections')
const baseId = useId()

const servicesByID = computed(
  () => new Map(props.services.map((service) => [service.id, service])),
)
const resolved = computed(() =>
  resolvedComposition(props.modelValue, props.categories),
)
const normalizedQuery = computed(() => query.value.trim().toLowerCase())

// Every list no category claims, in catalog order.
const uncategorized = computed(() => {
  const claimed = new Set(
    props.categories.flatMap((category) => category.services),
  )
  return props.services.filter((service) => !claimed.has(service.id))
})

const rows = computed<CategoryRow[]>(() => {
  const built = props.categories.map((category) =>
    row(category.id, categoryLabel(category), category, members(category)),
  )
  const loose = uncategorized.value
  if (loose.length > 0) {
    built.push(row(uncategorizedID, t('servicePicker.other'), null, loose))
  }
  return built.filter(
    (entry) => entry.members.length > 0 || matchesLabel(entry),
  )
})

const activeRow = computed(
  () => rows.value.find((entry) => entry.id === activeCategoryID.value) ?? null,
)
const activeService = computed(
  () => servicesByID.value.get(activeServiceID.value) ?? null,
)

watch(
  rows,
  (current) => {
    if (current.some((entry) => entry.id === activeCategoryID.value)) return
    activeCategoryID.value =
      current.find((entry) => entry.state !== 'none')?.id ??
      current[0]?.id ??
      ''
  },
  { immediate: true },
)

function row(
  id: string,
  label: string,
  category: CategoryDetail | null,
  all: ServiceDetail[],
): CategoryRow {
  const held = all.filter((service) => included(service.id)).length
  return {
    all,
    category,
    id,
    included: held,
    label,
    members: visible(all, label),
    preview: all.map((service) => service.title).join(', '),
    state:
      category === null
        ? looseState(all, held)
        : categorySelectionState(props.modelValue, props.categories, id),
  }
}

// A computed row has no reference to select, so its state is read from its
// members rather than from a category the composition could name.
function looseState(
  all: ServiceDetail[],
  included: number,
): 'none' | 'partial' | 'all' {
  if (all.length === 0 || included === 0) return 'none'
  return included === all.length ? 'all' : 'partial'
}

function included(serviceID: string): boolean {
  return serviceIncluded(props.modelValue, props.categories, serviceID)
}

function categoryLabel(category: CategoryDetail): string {
  return tor(`category.${category.id}`, category.title)
}

function members(category: CategoryDetail): ServiceDetail[] {
  return category.services
    .map((id) => servicesByID.value.get(id))
    .filter((service): service is ServiceDetail => service !== undefined)
}

// A row whose own name matches keeps all its members; otherwise the field
// narrows to the members that match it.
function visible(all: ServiceDetail[], label: string): ServiceDetail[] {
  const text = normalizedQuery.value
  if (text === '' || label.toLowerCase().includes(text)) return all
  return all.filter(
    (service) =>
      service.title.toLowerCase().includes(text) ||
      service.id.toLowerCase().includes(text),
  )
}

function matchesLabel(entry: CategoryRow): boolean {
  const text = normalizedQuery.value
  return text === '' || entry.label.toLowerCase().includes(text)
}

function selectCategory(categoryID: string): void {
  activeCategoryID.value = categoryID
}

function showCategory(categoryID: string): void {
  selectCategory(categoryID)
  mobilePane.value = 'details'
}

function showCollections(): void {
  mobilePane.value = 'collections'
}

/**
 * Selecting a category selects the reference, so the route follows whatever the
 * category holds later. The computed row has no reference to follow, so it
 * selects its members one by one — the same result the operator sees, stored
 * as what it actually is.
 */
function toggleRow(entry: CategoryRow): void {
  selectCategory(entry.id)
  if (entry.category !== null) {
    emit(
      'update:modelValue',
      toggleCompositionCategory(props.modelValue, props.categories, entry.id),
    )
    return
  }
  const selectAll = entry.state !== 'all'
  let next = props.modelValue
  for (const service of entry.all) {
    if (serviceIncluded(next, props.categories, service.id) === selectAll)
      continue
    next = toggleCompositionService(next, props.categories, service.id)
  }
  emit('update:modelValue', next)
}

function toggleService(serviceID: string): void {
  emit(
    'update:modelValue',
    toggleCompositionService(props.modelValue, props.categories, serviceID),
  )
}

function openService(serviceID: string): void {
  activeServiceID.value = serviceID
}

function closeService(): void {
  activeServiceID.value = ''
}

// The card reads the list and states where it stands; the one thing it can
// change is membership in this draft, and the draft belongs to the screen that
// owns it.
function onDialogInclude(add: boolean): void {
  const service = activeService.value
  if (service === null || included(service.id) === add) return
  emit(
    'update:modelValue',
    toggleCompositionService(props.modelValue, props.categories, service.id),
  )
}
</script>

<template>
  <div class="picker">
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
      <p class="picker__count" role="status">
        {{ tc('create.resolved', resolved.length) }}
      </p>
    </div>

    <p v-if="rows.length === 0" class="picker__empty">
      {{ t('create.noMatches', { query: query.trim() }) }}
    </p>

    <div
      v-else
      class="picker__workspace"
      :class="`picker__workspace--${mobilePane}`"
    >
      <section
        :aria-labelledby="baseId + '-collections'"
        class="picker__collections"
      >
        <header class="picker__pane-header">
          <h3 :id="baseId + '-collections'">
            {{ t('servicePicker.collections') }}
          </h3>
        </header>
        <ul class="picker__groups">
          <li
            v-for="entry in rows"
            :key="entry.id"
            class="picker__group"
            :class="{
              'picker__group--active': activeCategoryID === entry.id,
            }"
          >
            <label class="picker__category">
              <input
                :checked="entry.state === 'all'"
                class="picker__checkbox"
                :disabled="disabled"
                :indeterminate.prop="entry.state === 'partial'"
                type="checkbox"
                :value="entry.id"
                @change="toggleRow(entry)"
              />
              <span class="picker__category-copy">
                <span class="picker__category-title">
                  <strong>{{ entry.label }}</strong>
                  <small>{{ entry.included }}/{{ entry.all.length }}</small>
                </span>
                <small class="picker__preview">{{ entry.preview }}</small>
              </span>
            </label>
            <button
              :aria-controls="baseId + '-details'"
              :aria-current="activeCategoryID === entry.id ? 'true' : undefined"
              :aria-label="
                t('servicePicker.configure.aria', { category: entry.label })
              "
              class="picker__configure"
              :disabled="disabled"
              type="button"
              @click="showCategory(entry.id)"
            >
              <RvIcon name="chevron" />
            </button>
          </li>
        </ul>
      </section>

      <section
        v-if="activeRow !== null"
        :id="baseId + '-details'"
        :aria-labelledby="baseId + '-details-title'"
        class="picker__details"
      >
        <header class="picker__details-header">
          <button class="picker__back" type="button" @click="showCollections">
            <RvIcon name="chevron" />
            {{ t('servicePicker.back') }}
          </button>
          <h3 :id="baseId + '-details-title'" class="picker__details-title">
            {{ activeRow.label }}
          </h3>
          <strong class="picker__details-count">
            {{ activeRow.included }}/{{ activeRow.all.length }}
          </strong>
        </header>
        <div class="picker__pane-body">
          <ul class="picker__members">
            <li
              v-for="service in activeRow.members"
              :key="service.id"
              class="picker__service-row"
            >
              <label class="picker__service">
                <input
                  :checked="included(service.id)"
                  class="picker__checkbox"
                  :disabled="disabled"
                  type="checkbox"
                  :value="service.id"
                  @change="toggleService(service.id)"
                />
                <span>{{ service.title }}</span>
              </label>
              <button
                :aria-label="
                  t('serviceDetail.open.aria', { service: service.title })
                "
                class="picker__service-info"
                type="button"
                @click="openService(service.id)"
              >
                <RvIcon name="chevron" />
              </button>
            </li>
          </ul>
        </div>
      </section>
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
