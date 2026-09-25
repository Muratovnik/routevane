<script setup lang="ts">
import { portalTargetInjectionKey } from '@nuxt/ui/composables/usePortal'
import { computed, h, inject, ref, type FunctionalComponent } from 'vue'

import type { ChoiceGroup, ChoiceOption, IconName } from '@/shared/ui/types'
import RvSearchSelect from '@/shared/ui/RvSearchSelect.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'

/**
 * One choice from a known set.
 *
 * A native `<select>` cannot be given this product's ink, ground or metrics —
 * the operating system draws its list — so Nuxt UI's select draws it at the
 * standard field size, and the keyboard contract a select owes comes from the
 * Reka primitive underneath: arrows, Home and End, type-ahead, Enter and
 * Escape, and a panel that is a viewport overlay rather than part of whatever
 * it was opened inside. `searchable` hands the same contract to the searchable
 * choice instead.
 *
 * An empty string means nothing is chosen; the trigger reads the placeholder
 * then, exactly as an unset native select does.
 */
const props = defineProps<{
  searchable?: boolean
  size?: 'default' | 'compact'
  describedBy?: string
  disabled?: boolean
  /** Grouped choices. Groups and flat options are mutually exclusive. */
  groups?: ChoiceGroup[]
  /** The id a field label points at; a button is a labelable element. */
  inputId: string
  invalid?: boolean
  loading?: boolean
  loadingLabel?: string
  /** A heading that already names this choice, when no label element does. */
  labelledBy?: string
  options?: ChoiceOption[]
  placeholder: string
}>()

const model = defineModel<string>({ required: true })

// The library draws its icons in its own places and colours when it is handed
// components; this product's glyphs keep it from fetching or injecting a set.
// The attributes the library hands its icon — its size, colour and slot
// name — are passed on to the glyph.
const glyph =
  (name: IconName): FunctionalComponent =>
  (_props, { attrs }) =>
    h(RvIcon, { ...attrs, name })
const chevronGlyph = glyph('chevron')
const checkGlyph = glyph('check')
const workingGlyph = glyph('refresh')

const runs = computed<ChoiceGroup[]>(
  () => props.groups ?? [{ key: '', label: '', options: props.options ?? [] }],
)

// The library's items: a named run opens with a label item, and a choice
// carries its own fields under names the library does not read as an icon.
// Both carry the same fields, so the slots read them without narrowing.
type Entry = {
  disabled?: boolean
  glyph?: IconName
  label: string
  mono?: string
  note?: string
  type?: 'label'
  value?: string
  warning?: string
}
const items = computed<Entry[][]>(() =>
  runs.value.map((run) => [
    ...(run.label === '' ? [] : [{ type: 'label' as const, label: run.label }]),
    ...run.options.map((option) => ({
      disabled: option.disabled === true,
      glyph: option.icon,
      label: option.label,
      mono: option.mono,
      note: option.note,
      value: option.value,
      warning: option.warning,
    })),
  ]),
)

const choices = computed(() => runs.value.flatMap((run) => run.options))
const selected = computed(() =>
  choices.value.find((option) => option.value === model.value),
)

// A panel opened inside a dialog is portalled into it (RvDialog), and the
// dialog clips what reaches past its edge. The positioner is told the dialog
// is the room it has, so the panel flips or shortens instead of being cut.
const portalTarget = inject(portalTargetInjectionKey, undefined)
const boundary = ref<Element | null>(null)
const measureRoom = (open: boolean): void => {
  if (!open) return
  const target = portalTarget?.value
  boundary.value =
    typeof target === 'string' && target !== 'body'
      ? document.querySelector(target)
      : null
}

// The panel's own name; the library forwards it to the listbox it opens.
const panel = computed(() => ({
  align: 'start' as const,
  'aria-label': props.placeholder,
  collisionBoundary: boundary.value ?? undefined,
}))

// Reka carries "nothing chosen" as an absent value; this surface carries it as
// the empty string, because that is what an unset native select answered and
// what every caller already stores.
const chosen = computed<string | undefined>({
  get: () => (model.value === '' ? undefined : model.value),
  set: (value) => {
    model.value = value ?? ''
  },
})
</script>

<template>
  <RvSearchSelect
    v-if="searchable"
    v-bind="{ ...$props, searchable: undefined }"
    v-model="model"
    :size="size ?? 'default'"
  />
  <USelect
    v-else
    :id="inputId"
    v-model="chosen"
    class="rv-select"
    :aria-busy="loading === true ? 'true' : undefined"
    :aria-describedby="describedBy"
    :aria-invalid="invalid === true ? 'true' : undefined"
    :aria-label="loading === true ? loadingLabel : undefined"
    :aria-labelledby="labelledBy"
    :color="invalid === true ? 'error' : 'primary'"
    :content="panel"
    description-key="note"
    :disabled="disabled === true || loading === true || choices.length === 0"
    :highlight="invalid === true"
    :items="items"
    :loading="loading === true"
    :loading-icon="workingGlyph"
    :placeholder="placeholder"
    :selected-icon="checkGlyph"
    :size="size === 'compact' ? 'lg' : 'xl'"
    :trailing-icon="chevronGlyph"
    variant="outline"
    @update:open="measureRoom"
  >
    <template v-if="selected?.icon !== undefined && loading !== true" #leading>
      <RvIcon :name="selected.icon" />
    </template>
    <template #item-leading="{ item }">
      <RvIcon v-if="item.glyph" :name="item.glyph" />
    </template>
    <template #item-label="{ item }">
      {{ item.label }}
      <span v-if="item.mono" class="rv-mono">{{ item.mono }}</span>
    </template>
    <!-- A stated limit of the choice stays whole at the row's end, in words
         beside its mark, rather than in the note the library shortens. -->
    <template #item-trailing="{ item }">
      <template v-if="item.warning">
        <RvIcon name="warning" /><span>{{ item.warning }}</span>
      </template>
    </template>
  </USelect>
</template>

<!-- Layout of the root only: the library's trigger is an inline box, and a
     field fills the column it is given. The trigger is the library's own
     element, which this file's scope attribute does not reach, so the rule is
     written against the facade's unique class. -->
<style>
.rv-select {
  width: 100%;
  min-width: 0;
}
</style>
