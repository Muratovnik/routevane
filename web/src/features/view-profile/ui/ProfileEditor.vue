<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'

import {
  cloneComposition,
  compositionSignature,
  normalizeComposition,
  resolvedComposition,
} from '@/entities/profile-composition/model/composition'
import { useCompositionForecast } from '@/entities/profile-composition/model/forecast'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvComposerForm from '@/shared/ui/RvComposerForm.vue'
import RvComposer from '@/shared/ui/RvComposer.vue'
import ListPicker from '@/entities/profile-composition/ui/ListPicker.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import { targetIcon } from '@/shared/lib/targetIcon'
import type { CategoryDetail, ListDetail } from '@/shared/api/catalog'
import type { ProfileComposition } from '@/shared/api/profiles'
import RvButton from '@/shared/ui/RvButton.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'

const props = defineProps<{
  busy: boolean
  name: string
  lists: ListDetail[]
  categories: CategoryDetail[]
  selected: string[]
  selectedCategories: string[]
  exclusions: string[]
  listDomains: Record<string, string[]>
  priority?: string[]
  // What this profile already publishes, already named in the operator's
  // language. The editor asks the server what the draft would weigh in these
  // formats; with none bound there is nothing to weigh it against.
  outputs: {
    id: string
    targetID: string
    title: string
    keepAddresses?: boolean
  }[]
}>()

const emit = defineEmits<{
  save: [name: string, composition: ProfileComposition]
}>()

const { formatNumber, t } = useLocale()
const forecast = useCompositionForecast()

const storedComposition = (): ProfileComposition =>
  cloneComposition({
    lists: props.selected,
    categories: props.selectedCategories,
    exclusions: props.exclusions,
    listDomains: props.listDomains,
    priority: props.priority ?? [],
  })

const draftName = ref(props.name)
const draftComposition = ref<ProfileComposition>(storedComposition())
// Membership may temporarily take a list out of the draft, but that gesture
// does not say to move it. Remember the order independently of the normalized
// draft, whose priority can contain included lists only, so putting the same
// list back also puts it back in the same place.
const preferredPriority = ref(
  resolvedComposition(draftComposition.value, props.categories),
)

const resolved = computed(() =>
  resolvedComposition(draftComposition.value, props.categories),
)

const forecastTargets = computed(() => [
  ...new Set(props.outputs.map((output) => output.targetID)),
])
// A format that keeps addresses beside domains is weighed with them (ADR 0057).
// The forecast answers per format, so when two of this profile's outputs share
// one and only one keeps addresses, the larger file is the one weighed.
const forecastKeepAddresses = computed(() => [
  ...new Set(
    props.outputs
      .filter((output) => output.keepAddresses === true)
      .map((output) => output.targetID),
  ),
])

watch(
  () =>
    JSON.stringify([
      draftComposition.value,
      resolved.value,
      forecastTargets.value,
      forecastKeepAddresses.value,
    ]),
  () => {
    // An empty target list asks the endpoint about every format there is,
    // which is the opposite of what a profile publishing nowhere wants: with
    // nothing bound there is nothing to weigh the draft against.
    if (forecastTargets.value.length === 0) {
      forecast.forget()
      return
    }
    forecast.request(
      draftComposition.value,
      resolved.value,
      forecastTargets.value,
      forecastKeepAddresses.value,
    )
  },
  { immediate: true },
)

// One format weighs the lists, and it is the first one this profile publishes
// in: a list's share of the rules differs per format, so mixing two would
// be adding numbers that do not belong to the same total.
const firstForecast = computed(() => {
  const first = props.outputs[0]
  return first === undefined ? null : forecast.forTarget(first.targetID)
})

// A format that would refuse this draft says so under the save control. It
// does not stop the save: the operator may be fixing one format while another
// is still over its bound, and a failed rebuild is already reported per output.
const overflowLines = computed<string[]>(() => {
  const lines: string[] = []
  for (const output of props.outputs) {
    const answer = forecast.forTarget(output.targetID)
    if (answer === null || answer.incompleteLists?.length || answer.fits)
      continue
    lines.push(
      t('profile.forecast.overflow', {
        count: formatNumber(answer.projectedRules),
        max: formatNumber(answer.maximumRules),
        target: output.title,
      }),
    )
  }
  return lines
})

// Editing normalises the draft — sorted, deduplicated — while the stored profile
// arrives in whatever order the server wrote it. Comparing the two as written
// therefore reported a change where none was made, so both sides are compared
// as what they select.
const dirty = computed(
  () =>
    draftName.value !== props.name ||
    compositionSignature(draftComposition.value, props.categories) !==
      compositionSignature(storedComposition(), props.categories),
)

// What a profile cannot be stored without. Pressing save with one of them
// missing states it beside what is missing and moves the keyboard there; the
// button itself is withdrawn only while there is nothing to save.
const nameMissing = computed(() => draftName.value.trim() === '')
const listsMissing = computed(() => resolved.value.length === 0)
const attempted = ref(false)
const picker = useTemplateRef<{ focus: () => void }>('picker')

const setComposition = (composition: ProfileComposition): void => {
  const next = normalizeComposition(composition, props.categories)
  const remembered = new Set(preferredPriority.value)
  const newlySelected = (next.priority ?? []).filter(
    (id) => !remembered.has(id),
  )
  preferredPriority.value = [...preferredPriority.value, ...newlySelected]
  const included = new Set(resolvedComposition(next, props.categories))
  draftComposition.value = normalizeComposition(
    {
      ...next,
      priority: preferredPriority.value.filter((id) => included.has(id)),
    },
    props.categories,
  )
}

const setPriority = (ids: string[]): void => {
  if (props.busy) return
  const next = normalizeComposition(
    { ...cloneComposition(draftComposition.value), priority: ids },
    props.categories,
  )
  preferredPriority.value = [...(next.priority ?? [])]
  draftComposition.value = next
}

const retryForecast = (): void => {
  if (forecastTargets.value.length === 0) return
  forecast.retry(
    draftComposition.value,
    resolved.value,
    forecastTargets.value,
    forecastKeepAddresses.value,
  )
}

const submit = async (): Promise<void> => {
  if (props.busy || !dirty.value) return
  if (nameMissing.value || listsMissing.value) {
    attempted.value = true
    await nextTick()
    if (nameMissing.value) document.getElementById('editor-name')?.focus()
    else picker.value?.focus()
    return
  }
  emit('save', draftName.value.trim(), draftComposition.value)
}

// Cancel restores the stored profile: the draft returns to what the server
// holds, and nothing leaves the page.
const reset = (): void => {
  attempted.value = false
  draftName.value = props.name
  draftComposition.value = storedComposition()
  preferredPriority.value = resolvedComposition(
    draftComposition.value,
    props.categories,
  )
}

// The page reads the stored profile again after a write on any of its facets
// and keeps this editor mounted through that read. A draft the operator has not
// touched follows what is stored; one they changed stays theirs, unless the
// server now holds exactly what it says, which is a save of this draft landing.
// Each side is compared under the categories it was read with.
const stored = computed(() => ({
  categories: props.categories,
  composition: storedComposition(),
  name: props.name,
}))

const draftMatches = (
  name: string,
  side: {
    categories: CategoryDetail[]
    composition: ProfileComposition
    name: string
  },
): boolean =>
  name === side.name &&
  compositionSignature(draftComposition.value, side.categories) ===
    compositionSignature(side.composition, side.categories)

watch(stored, (next, previous) => {
  if (
    draftMatches(draftName.value, previous) ||
    draftMatches(draftName.value.trim(), next)
  )
    reset()
})
</script>

<template>
  <div class="editor">
    <RvComposer :settings-label="t('create.settings')">
      <section aria-labelledby="editor-composition" class="editor__composition">
        <h2 id="editor-composition" class="rv-visually-hidden">
          {{ t('profile.composition.lists') }}
        </h2>
        <div class="editor__composition-body">
          <ListPicker
            ref="picker"
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
            :overlap-unavailable-label="t('profile.overlap.unavailable')"
            :forecast-pending="forecast.pending.value"
            :refreshing="forecast.observing.value"
            :lists="props.lists"
            :model-value="draftComposition"
            :retryable="forecastTargets.length > 0"
            @update:model-value="setComposition"
            @reorder="setPriority"
            @refresh="
              forecast.refresh(
                draftComposition,
                resolved,
                forecastTargets,
                forecastKeepAddresses,
              )
            "
            @retry="retryForecast"
          />
        </div>
      </section>

      <template #settings>
        <RvComposerForm class="editor__settings" @submit.prevent="submit">
          <RvField
            v-slot="{ describedBy, invalid }"
            class="editor__field"
            :error="attempted && nameMissing ? t('create.name.required') : ''"
            input-id="editor-name"
            :label="t('profile.edit.name')"
          >
            <RvTextInput
              v-model="draftName"
              :described-by="describedBy"
              :disabled="props.busy"
              input-id="editor-name"
              :invalid="invalid"
              maxlength="120"
            />
          </RvField>

          <!-- What this profile already publishes, stated one connection per
               line with the glyph of the format it is delivered in. It is a
               fact about the stored profile, not a choice this form offers; the
               caption names the list, so a reader of the list hears it. -->
          <div v-if="outputs.length" class="editor__facts">
            <span id="editor-formats" class="editor__label">{{
              t('profile.edit.connections')
            }}</span>
            <ul aria-labelledby="editor-formats" class="editor__connections">
              <li
                v-for="output in outputs"
                :key="output.id"
                class="editor__connection"
              >
                <RvIcon :name="targetIcon(output.targetID)" />
                {{ output.title }}
              </li>
            </ul>
          </div>
          <!-- Above the act, and only while there is something to say: that the
               draft differs from what is stored, leaving it, and what stands
               in the way of storing it. The dirty region is always present so
               the change is announced when it appears, and it takes no room
               while it is empty. -->
          <template #status>
            <span class="editor__dirty" role="status">
              <RvStatus
                v-if="dirty"
                :label="t('profile.edit.dirty')"
                tone="warning"
              />
            </span>
            <!-- Its name says what it undoes: "cancel" alone, next to a save,
                 reads as though it might cancel the save that is running. -->
            <RvButton
              v-if="dirty"
              :disabled="props.busy"
              variant="quiet"
              @click="reset"
            >
              {{ t('profile.edit.discard') }}
            </RvButton>
            <RvStatus
              v-if="attempted && listsMissing"
              class="editor__problem"
              role="alert"
              tone="failed"
              :label="t('create.lists.required')"
            />
          </template>
          <template #actions>
            <!-- What saving does is in the name of the control that does it.
                 A note beside it could only restate that name, so there is
                 none. -->
            <RvButton
              block
              :disabled="props.busy || !dirty"
              :loading="props.busy"
              :loading-label="t('profile.edit.saving')"
              type="submit"
              variant="primary"
            >
              {{
                props.busy ? t('profile.edit.saving') : t('profile.edit.save')
              }}
            </RvButton>
            <div
              v-if="overflowLines.length > 0"
              class="editor__forecast"
              role="status"
            >
              <p v-for="line in overflowLines" :key="line">{{ line }}</p>
            </div>
          </template>
        </RvComposerForm>
      </template>
    </RvComposer>
  </div>
</template>

<style scoped>
.editor {
  height: 100%;
  min-height: 0;
  display: grid;
  gap: var(--rv-space-6);
  width: 100%;
}

.editor__field {
  min-width: 0;
  max-width: var(--rv-measure-field);
}

/* The published connections stand beside the name field and read as a field
   of their own: the caption keeps the field label's weight, size and the gap
   the field draws under it. */
.editor__facts {
  display: grid;
  gap: var(--rv-space-1);
  min-width: 0;
  max-width: var(--rv-measure-field);
}

.editor__label {
  font-weight: 500;
  font-size: var(--rv-text-dense);
}

.editor__composition {
  height: 100%;
  min-height: 0;
  min-width: 0;
}

.editor__composition-body {
  height: 100%;
  min-height: 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--rv-space-5);
  align-items: start;
  min-width: 0;
}

.editor__forecast {
  display: grid;
  gap: var(--rv-space-1);
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-status-warning);
  font-size: var(--rv-text-dense);
}

.editor__dirty {
  margin-inline-end: auto;
}

.editor__problem {
  flex: 1 1 100%;
}

/* The block keeps a control row's height so the two-column band layout
   still aligns this summary with the name field beside it. */
.editor__connections {
  display: grid;
  gap: var(--rv-space-1);
  align-content: center;
  min-height: var(--rv-control-default);
  min-width: 0;
}

.editor__connection {
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
  min-width: 0;
  overflow-wrap: anywhere;
}
</style>
