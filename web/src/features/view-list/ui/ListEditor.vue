<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import {
  categoryServices,
  cloneComposition,
  compositionSignature,
  normalizeComposition,
  resolvedComposition,
  serviceIncluded,
  toggleCompositionService,
} from '@/entities/list-composition/model/composition'
import { useCompositionForecast } from '@/entities/list-composition/model/forecast'
import ServiceDetailDialog from '@/entities/list-composition/ui/ServiceDetailDialog.vue'
import ServicePicker from '@/entities/list-composition/ui/ServicePicker.vue'
import CompositionOverlaps from '@/entities/list-composition/ui/CompositionOverlaps.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import type { CategoryDetail, ServiceDetail } from '@/shared/api/catalog'
import type { ListComposition } from '@/shared/api/lists'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'

const props = defineProps<{
  busy: boolean
  name: string
  services: ServiceDetail[]
  categories: CategoryDetail[]
  selected: string[]
  selectedCategories: string[]
  exclusions: string[]
  serviceDomains: Record<string, string[]>
  // What this list already publishes, already named in the operator's
  // language. The editor asks the server what the draft would weigh in these
  // formats; with none bound there is nothing to weigh it against.
  outputs: { id: string; targetID: string; title: string }[]
}>()

const emit = defineEmits<{
  save: [name: string, composition: ListComposition]
}>()

const { formatNumber, t, tc, tor } = useLocale()
const forecast = useCompositionForecast()

function storedComposition(): ListComposition {
  return cloneComposition({
    services: props.selected,
    categories: props.selectedCategories,
    exclusions: props.exclusions,
    serviceDomains: props.serviceDomains,
  })
}

const draftName = ref(props.name)
const draftComposition = ref<ListComposition>(storedComposition())
// The picker is the way to add, not the way to read: what the list holds is
// the rows above it, and the catalog opens only when the operator asks for it.
const pickerOpen = ref(false)
const activeServiceID = ref('')

const resolved = computed(() =>
  resolvedComposition(draftComposition.value, props.categories),
)

const forecastTargets = computed(() => [
  ...new Set(props.outputs.map((output) => output.targetID)),
])

watch(
  [draftComposition, forecastTargets],
  () => {
    // An empty target list asks the endpoint about every format there is,
    // which is the opposite of what a list publishing nowhere wants: with
    // nothing bound there is nothing to weigh the draft against.
    if (forecastTargets.value.length === 0) {
      forecast.forget()
      return
    }
    forecast.request(
      draftComposition.value,
      resolved.value,
      forecastTargets.value,
    )
  },
  { immediate: true },
)

// One format weighs the services, and it is the first one this list publishes
// in: a service's share of the rules differs per format, so mixing two would
// be adding numbers that do not belong to the same total.
const weights = computed<Map<string, number>>(() => {
  const first = props.outputs[0]
  if (first === undefined) return new Map()
  const answer = forecast.forTarget(first.targetID)
  if (answer === null) return new Map()
  return new Map(
    answer.perService.map((entry) => [entry.serviceID, entry.rules]),
  )
})

const categoryRows = computed(() =>
  draftComposition.value.categories.map((id) => {
    const detail = props.categories.find((category) => category.id === id)
    return {
      id,
      label: tor(`category.${id}`, detail?.title ?? id),
      size: detail?.services.length ?? 0,
    }
  }),
)

const serviceRows = computed(() =>
  resolved.value
    .map((id) => ({
      id,
      title: props.services.find((service) => service.id === id)?.title ?? id,
      weight: weights.value.get(id) ?? null,
    }))
    .sort((left, right) => left.title.localeCompare(right.title)),
)

// A format that would refuse this draft says so beside the save control. It
// does not stop the save: the operator may be fixing one format while another
// is still over its bound, and a failed rebuild is already reported per output.
const overflowLines = computed<string[]>(() => {
  const lines: string[] = []
  for (const output of props.outputs) {
    const answer = forecast.forTarget(output.targetID)
    if (answer === null || answer.fits) continue
    lines.push(
      t('list.forecast.overflow', {
        count: formatNumber(answer.projectedRules),
        max: formatNumber(answer.maximumRules),
        target: output.title,
      }),
    )
  }
  return lines
})

const activeService = computed(
  () =>
    props.services.find((service) => service.id === activeServiceID.value) ??
    null,
)

// Editing normalises the draft — sorted, deduplicated — while the stored list
// arrives in whatever order the server wrote it. Comparing the two as written
// therefore reported a change where none was made, so both sides are compared
// as what they select.
const dirty = computed(
  () =>
    draftName.value !== props.name ||
    compositionSignature(draftComposition.value, props.categories) !==
      compositionSignature(storedComposition(), props.categories),
)

const canSave = computed(
  () =>
    !props.busy && draftName.value.trim() !== '' && resolved.value.length > 0,
)

function includedInDraft(serviceID: string): boolean {
  return serviceIncluded(draftComposition.value, props.categories, serviceID)
}

function removeService(serviceID: string): void {
  if (props.busy) return
  draftComposition.value = toggleCompositionService(
    draftComposition.value,
    props.categories,
    serviceID,
  )
}

/**
 * Removing a collection removes the reference itself, so the list stops
 * following it rather than freezing today's members. An exclusion only means
 * something while something still carries the service, so the ones left
 * pointing at nothing are dropped with it.
 */
function removeCategory(categoryID: string): void {
  if (props.busy) return
  const next: ListComposition = {
    services: [...draftComposition.value.services],
    categories: draftComposition.value.categories.filter(
      (id) => id !== categoryID,
    ),
    exclusions: [...draftComposition.value.exclusions],
    serviceDomains: draftComposition.value.serviceDomains,
  }
  const carried = categoryServices(next, props.categories)
  next.exclusions = next.exclusions.filter((id) => carried.has(id))
  draftComposition.value = normalizeComposition(next, props.categories)
}

function openService(serviceID: string): void {
  activeServiceID.value = serviceID
}

function closeService(): void {
  activeServiceID.value = ''
}

// The card's footer speaks about this draft; its contents belong to the
// service and were already persisted by the card itself.
function onDialogInclude(add: boolean): void {
  const service = activeService.value
  if (service === null || includedInDraft(service.id) === add) return
  removeService(service.id)
}

function submit(): void {
  if (!canSave.value) return
  pickerOpen.value = false
  emit('save', draftName.value.trim(), draftComposition.value)
}

// Cancel restores the stored list: the draft returns to what the server
// holds, and nothing leaves the page.
function reset(): void {
  draftName.value = props.name
  draftComposition.value = storedComposition()
  pickerOpen.value = false
}
</script>

<template>
  <form class="editor" @submit.prevent="submit">
    <div class="editor__field">
      <label class="editor__label" for="editor-name">
        {{ t('list.edit.name') }}
      </label>
      <input
        id="editor-name"
        v-model="draftName"
        class="editor__input"
        :disabled="props.busy"
        maxlength="120"
        type="text"
      />
    </div>

    <section aria-labelledby="editor-composition" class="editor__composition">
      <h2 id="editor-composition" class="editor__label">
        {{ t('list.composition.services') }}
      </h2>

      <p
        v-if="categoryRows.length === 0 && serviceRows.length === 0"
        class="editor__note"
      >
        {{ t('list.composition.empty') }}
      </p>
      <ul v-else class="editor__rows">
        <li
          v-for="row in categoryRows"
          :key="`category-${row.id}`"
          class="editor__row"
        >
          <span class="editor__row-copy">
            <strong>{{ row.label }}</strong>
            <small>{{ tc('create.category.size', row.size) }}</small>
          </span>
          <button
            :aria-label="
              t('list.composition.remove.aria', { service: row.label })
            "
            class="editor__row-action"
            :disabled="props.busy"
            type="button"
            @click="removeCategory(row.id)"
          >
            <RvIcon name="trash" />
          </button>
        </li>
        <li
          v-for="row in serviceRows"
          :key="`service-${row.id}`"
          class="editor__row"
        >
          <span class="editor__row-copy">
            <strong>{{ row.title }}</strong>
            <small v-if="row.weight !== null">
              {{ tc('create.forecast.rules', row.weight) }}
            </small>
          </span>
          <button
            :aria-label="t('serviceDetail.open.aria', { service: row.title })"
            class="editor__row-open"
            type="button"
            @click="openService(row.id)"
          >
            <RvIcon name="chevron" />
          </button>
          <button
            :aria-label="
              t('list.composition.remove.aria', { service: row.title })
            "
            class="editor__row-action"
            :disabled="props.busy"
            type="button"
            @click="removeService(row.id)"
          >
            <RvIcon name="trash" />
          </button>
        </li>
      </ul>

      <RvButton
        v-if="!pickerOpen"
        :disabled="props.busy"
        type="button"
        variant="secondary"
        @click="pickerOpen = true"
      >
        <RvIcon name="plus" />
        {{ t('list.composition.add') }}
      </RvButton>

      <fieldset
        v-show="pickerOpen"
        class="editor__services"
        :disabled="props.busy"
      >
        <legend class="editor__visually-hidden">
          {{ t('list.composition.add') }}
        </legend>
        <header class="editor__picker-header">
          <h3 class="editor__picker-title">{{ t('list.composition.add') }}</h3>
          <RvButton
            :aria-label="t('list.composition.hide.aria')"
            size="compact"
            type="button"
            variant="quiet"
            @click="pickerOpen = false"
          >
            <RvIcon name="close" />
            {{ t('list.composition.hide') }}
          </RvButton>
        </header>
        <ServicePicker
          v-model="draftComposition"
          :categories="props.categories"
          :disabled="props.busy"
          :list-name="draftName"
          :pending="dirty"
          :services="props.services"
        />
      </fieldset>
    </section>

    <CompositionOverlaps
      v-if="resolved.length > 1 && props.outputs[0] !== undefined"
      :forecast="forecast.forTarget(props.outputs[0].targetID)"
      :pending="forecast.pending.value"
      :services="props.services"
      :target-title="props.outputs[0].title"
      @retry="forecast.request(draftComposition, resolved, forecastTargets)"
    />

    <p class="editor__note">{{ t('list.edit.note') }}</p>

    <div v-if="overflowLines.length > 0" class="editor__forecast" role="status">
      <p v-for="line in overflowLines" :key="line">{{ line }}</p>
    </div>

    <div class="editor__actions">
      <RvButton :disabled="!canSave || !dirty" type="submit" variant="primary">
        {{ props.busy ? t('list.edit.saving') : t('list.edit.save') }}
      </RvButton>
      <RvButton
        v-if="dirty"
        :disabled="props.busy"
        variant="quiet"
        @click="reset"
      >
        {{ t('action.cancel') }}
      </RvButton>
    </div>

    <ServiceDetailDialog
      :disabled="props.busy"
      :included="
        activeService === null ? false : includedInDraft(activeService.id)
      "
      :list-name="draftName"
      mode="compose"
      :pending="dirty"
      :service="activeService"
      @close="closeService"
      @include="onDialogInclude"
    />
  </form>
</template>

<style scoped src="./ListEditor.css"></style>
