<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import {
  cloneComposition,
  compositionSignature,
  normalizeComposition,
  resolvedComposition,
} from '@/entities/list-composition/model/composition'
import { useCompositionForecast } from '@/entities/list-composition/model/forecast'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import RvComposer from '@/shared/ui/RvComposer.vue'
import ServicePicker from '@/entities/list-composition/ui/ServicePicker.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import type { CategoryDetail, ServiceDetail } from '@/shared/api/catalog'
import type { ListComposition } from '@/shared/api/lists'
import RvButton from '@/shared/ui/RvButton.vue'

const props = defineProps<{
  busy: boolean
  name: string
  services: ServiceDetail[]
  categories: CategoryDetail[]
  selected: string[]
  selectedCategories: string[]
  exclusions: string[]
  serviceDomains: Record<string, string[]>
  priority?: string[]
  // What this list already publishes, already named in the operator's
  // language. The editor asks the server what the draft would weigh in these
  // formats; with none bound there is nothing to weigh it against.
  outputs: { id: string; targetID: string; title: string }[]
}>()

const emit = defineEmits<{
  save: [name: string, composition: ListComposition]
}>()

const { formatNumber, t } = useLocale()
const forecast = useCompositionForecast()

function storedComposition(): ListComposition {
  return cloneComposition({
    services: props.selected,
    categories: props.selectedCategories,
    exclusions: props.exclusions,
    serviceDomains: props.serviceDomains,
    priority: props.priority ?? [],
  })
}

const draftName = ref(props.name)
const draftComposition = ref<ListComposition>(storedComposition())

const resolved = computed(() =>
  resolvedComposition(draftComposition.value, props.categories),
)

const forecastTargets = computed(() => [
  ...new Set(props.outputs.map((output) => output.targetID)),
])

watch(
  () =>
    JSON.stringify([
      draftComposition.value,
      resolved.value,
      forecastTargets.value,
    ]),
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
const firstForecast = computed(() => {
  const first = props.outputs[0]
  return first === undefined ? null : forecast.forTarget(first.targetID)
})

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

function setPriority(ids: string[]): void {
  if (props.busy) return
  draftComposition.value = normalizeComposition(
    { ...cloneComposition(draftComposition.value), priority: ids },
    props.categories,
  )
}

function retryForecast(): void {
  if (forecastTargets.value.length === 0) return
  forecast.retry(draftComposition.value, resolved.value, forecastTargets.value)
}

function submit(): void {
  if (!canSave.value) return
  emit('save', draftName.value.trim(), draftComposition.value)
}

// Cancel restores the stored list: the draft returns to what the server
// holds, and nothing leaves the page.
function reset(): void {
  draftName.value = props.name
  draftComposition.value = storedComposition()
}
</script>

<template>
  <div class="editor">
    <RvComposer :settings-label="t('create.settings')">
      <section aria-labelledby="editor-composition" class="editor__composition">
        <h2 id="editor-composition" class="editor__legend">
          {{ t('list.composition.services') }}
        </h2>
        <div class="editor__composition-body">
          <ServicePicker
            v-model="draftComposition"
            fill
            :categories="props.categories"
            :disabled="props.busy"
            :forecast="firstForecast"
            :forecast-failure="
              forecast.failure.value
                ? t(`forecast.failure.${forecast.failure.value}`)
                : undefined
            "
            :overlap-unavailable="forecastTargets.length === 0"
            :overlap-unavailable-label="t('list.overlap.unavailable')"
            :forecast-pending="forecast.pending.value"
            :list-name="draftName"
            :pending="dirty"
            :services="props.services"
            :retryable="forecastTargets.length > 0"
            @reorder="setPriority"
            @refresh="
              forecast.refresh(draftComposition, resolved, forecastTargets)
            "
            @retry="retryForecast"
          />
        </div>
      </section>

      <template #settings="{ compact }">
        <form
          class="editor__settings"
          :class="{ 'editor__settings--compact': compact }"
          @submit.prevent="submit"
        >
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

          <div v-if="outputs.length" class="editor__field">
            <span class="editor__label">{{ t('create.target') }}</span>
            <p>{{ outputs.map((output) => output.title).join(', ') }}</p>
          </div>
          <p v-if="!compact" class="editor__note">{{ t('list.edit.note') }}</p>

          <div
            v-if="overflowLines.length > 0"
            class="editor__forecast"
            role="status"
          >
            <p v-for="line in overflowLines" :key="line">{{ line }}</p>
          </div>

          <div
            class="editor__actions"
            :class="{ 'editor__actions--compact': compact }"
          >
            <RvInfoTip
              v-if="compact"
              :label="t('list.edit.effect')"
              :text="t('list.edit.note')"
            />
            <RvButton
              :disabled="!canSave || !dirty"
              :loading="props.busy"
              :loading-label="t('list.edit.saving')"
              type="submit"
              variant="primary"
            >
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
        </form>
      </template>
    </RvComposer>
  </div>
</template>

<style scoped src="./ListEditor.css"></style>
