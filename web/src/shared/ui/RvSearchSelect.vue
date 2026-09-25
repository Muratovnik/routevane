<script setup lang="ts" generic="T extends string | string[]">
import type { CommandPaletteGroup } from '@nuxt/ui'
import { portalTargetInjectionKey } from '@nuxt/ui/composables/usePortal'
import { computed, h, inject, ref, watch, type FunctionalComponent } from 'vue'

import type { ChoiceGroup, ChoiceOption, IconName } from '@/shared/ui/types'
import { useLocale } from '@/shared/i18n/useLocale'
import RvIcon from '@/shared/ui/RvIcon.vue'

/**
 * A choice with a search field above its list.
 *
 * Nuxt UI draws it as a command palette in a popover: a labelled trigger, a
 * portalled panel with the search at its top, named runs and a bounded
 * scrolling list. The palette keeps its search outside the list it drives,
 * which is what the accessibility audit asks of a listbox. At field size the
 * trigger is drawn as a field and the panel is as wide as it; at compact size
 * the trigger stands among filter chips and the panel takes the panel measure,
 * because a panel as narrow as a chip would cut the choices it lists. The
 * matching, what may be chosen and when the panel closes are decided here.
 */
const props = defineProps<{
  modelValue: T
  options?: ChoiceOption[]
  groups?: ChoiceGroup[]
  label?: string
  toggleLabel?: string
  inputId?: string
  describedBy?: string
  labelledBy?: string
  invalid?: boolean
  loading?: boolean
  loadingLabel?: string
  size?: 'default' | 'compact'
  placeholder: string
  triggerLabel?: string
  searchLabel?: string
  emptyLabel?: string
  disabled?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: T] }>()
defineSlots<{ option?: (props: { option: ChoiceOption }) => unknown }>()
const { t } = useLocale()

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
const searchGlyph = glyph('search')
const workingGlyph = glyph('refresh')

// The palette's own search is not used — the groups it is handed are already
// the matches — but it still caps each group, so the cap is lifted.
const paletteFuse = { resultLimit: Number.POSITIVE_INFINITY }

const multiple = computed(() => Array.isArray(props.modelValue))
const field = computed(() => props.size === 'default')
const open = ref(false)
const query = ref('')
const unavailable = computed(() => props.disabled === true || props.loading)
watch(unavailable, (value) => {
  if (value) open.value = false
})
// A closed panel forgets what was typed into it, so it reopens on everything.
watch(open, (value) => {
  query.value = ''
  measureRoom(value)
})

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

const runs = computed(
  () => props.groups ?? [{ key: '', label: '', options: props.options ?? [] }],
)
const choices = computed(() => runs.value.flatMap((group) => group.options))
const selected = computed(() =>
  choices.value.find((option) => option.value === props.modelValue),
)
const heading = computed(
  () => props.label ?? props.toggleLabel ?? props.placeholder,
)
const search = computed(() => props.searchLabel ?? t('choice.search'))
const empty = computed(() => props.emptyLabel ?? t('choice.empty'))
const shown = computed(
  () => props.triggerLabel ?? selected.value?.label ?? props.placeholder,
)

// Matching is by name, by the compared fact and by the run's own name, the
// same way in both sizes; the library is told not to filter again.
const matches = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase()
  return runs.value
    .map((group) => ({
      ...group,
      options: group.options.filter((option) =>
        [option.label, option.value, option.mono ?? '', group.label]
          .join(' ')
          .toLocaleLowerCase()
          .includes(needle),
      ),
    }))
    .filter((group) => group.options.length > 0)
})

// One choice as the library lists it, with the facts the slots draw.
type Entry = {
  description?: string
  disabled?: boolean
  glyph?: IconName
  label: string
  mono?: string
  note?: string
  option?: ChoiceOption
  value?: string
  warning?: string
}

const item = (option: ChoiceOption): Entry => ({
  description: option.note,
  disabled: option.disabled === true,
  glyph: option.icon,
  label: option.label,
  mono: option.mono,
  note: option.note,
  option,
  value: option.value,
  warning: option.warning,
})
const paletteGroups = computed<CommandPaletteGroup[]>(() =>
  matches.value.map((group) => ({
    id: group.key === '' ? 'choices' : group.key,
    ignoreFilter: true,
    items: group.options.map(item),
    label: group.label === '' ? undefined : group.label,
  })),
)

const choose = (value: unknown): void => {
  const valid = (entry: unknown): entry is string =>
    typeof entry === 'string' &&
    choices.value.some((choice) => choice.value === entry && !choice.disabled)
  if (unavailable.value) return
  if (multiple.value) {
    if (Array.isArray(value) && value.every(valid))
      emit('update:modelValue', value as T)
    return
  }
  if (valid(value)) emit('update:modelValue', value as T)
}

// The panel's own attributes. It is named for what it chooses. The library
// also points the panel at the id it gives its trigger; a field's trigger
// carries the caller's id instead, which the field's label needs, so that
// reference finds nothing and the panel's name is this one. The mark tells the
// accessibility audit which list is the palette's own
// (docs/adr/0043-accept-two-command-palette-findings.md).
const panelContent = computed(() => ({
  align: 'start' as const,
  'aria-label': heading.value,
  collisionBoundary: boundary.value ?? undefined,
  'data-rv-choice-panel': 'palette',
}))

// The trigger takes the caller's id only when there is one: an absent id must
// not replace the one the library gives a trigger nobody labels.
const triggerId = computed(() =>
  props.inputId === undefined ? {} : { id: props.inputId },
)
const paletteInput = computed(() => ({
  'aria-label': search.value,
  autocomplete: 'off',
}))

// The field's trigger takes its name from the field's label; a trigger with
// no label names the choice and what it shows, and a busy one says so.
const triggerName = computed(() => {
  if (props.loading === true) return props.loadingLabel ?? props.placeholder
  if (props.inputId !== undefined || props.labelledBy !== undefined)
    return undefined
  return `${heading.value}: ${shown.value}`
})
const selectedGlyph = computed(() =>
  selected.value?.icon === undefined ? undefined : glyph(selected.value.icon),
)

// The palette lists and reports its entries themselves; they are compared by
// their value, and a report is turned back into the values this surface keeps.
const paletteChosen = computed<Entry | Entry[] | undefined>(() => {
  const entries = choices.value
    .filter((option) =>
      Array.isArray(props.modelValue)
        ? props.modelValue.includes(option.value)
        : option.value === props.modelValue,
    )
    .map(item)
  return multiple.value ? entries : entries[0]
})
const choosePalette = (value: unknown): void => {
  const valueOf = (entry: unknown) => (entry as Entry | undefined)?.value
  choose(Array.isArray(value) ? value.map(valueOf) : valueOf(value))
  // A single choice is made once; a filter stays open for the next one.
  if (!multiple.value) open.value = false
}
</script>

<template>
  <UPopover v-model:open="open" :content="panelContent">
    <!-- The field's trigger is drawn as a field: its ground and edge, the value
         at the leading edge and the chevron at the end, at the field height.
         The compact trigger stands among the filter chips at their size, on the
         same outlined ground, which is the library variant nearest to them. -->
    <UButton
      v-bind="triggerId"
      :class="field ? 'rv-search-select' : 'rv-search-select__trigger'"
      :aria-busy="loading === true ? 'true' : undefined"
      :aria-describedby="describedBy"
      :aria-invalid="invalid === true ? 'true' : undefined"
      :aria-label="triggerName"
      :aria-labelledby="labelledBy"
      :block="field"
      :color="invalid === true ? 'error' : 'neutral'"
      :disabled="unavailable || choices.length === 0"
      :leading-icon="loading === true ? undefined : selectedGlyph"
      :loading="loading === true"
      :loading-icon="workingGlyph"
      :size="field ? 'xl' : 'lg'"
      :trailing-icon="chevronGlyph"
      variant="outline"
    >
      {{ loading === true ? (loadingLabel ?? placeholder) : shown }}
    </UButton>
    <template #content>
      <UCommandPalette
        v-model:search-term="query"
        class="rv-search-select__palette"
        :class="{ 'rv-search-select__palette--field': field }"
        by="value"
        :fuse="paletteFuse"
        :groups="paletteGroups"
        :icon="searchGlyph"
        :input="paletteInput"
        :model-value="paletteChosen"
        :multiple="multiple"
        :placeholder="search"
        :selected-icon="checkGlyph"
        :selection-behavior="multiple ? 'toggle' : 'replace'"
        @update:model-value="choosePalette"
      >
        <template #item-leading="{ item: entry }">
          <RvIcon v-if="entry.glyph" :name="entry.glyph" />
        </template>
        <template #item-label="{ item: entry }">
          <slot name="option" :option="(entry as Entry).option!">
            {{ entry.label }}
            <span v-if="entry.mono" class="rv-mono">{{ entry.mono }}</span>
          </slot>
        </template>
        <!-- A stated limit of the choice stays whole at the row's end, in words
             beside its mark, rather than in the note the library shortens. -->
        <template #item-trailing="{ item: entry }">
          <template v-if="entry.warning">
            <RvIcon name="warning" /><span>{{ entry.warning }}</span>
          </template>
        </template>
        <template #empty>{{ empty }}</template>
      </UCommandPalette>
    </template>
  </UPopover>
</template>

<!-- Layout of the roots only. The field's trigger fills its column, like a
     field; the compact trigger stands at the chips' compact height, which the
     library's line box for its size falls a fraction of a pixel short of. The
     triggers and the palette are the library's own elements, and the palette
     is portalled, so this file's scope attribute reaches none of them and the
     rules are written against the facade's unique classes.

     The compact panel takes the panel measure rather than the chip's width, so
     no category is cut to a chip; the field's panel is exactly as wide as its
     field. Either is never narrower than its trigger. Its list holds as much as
     the Reka list did — the overlay height, below the search row — and never
     more than the room the positioner reports. -->
<style>
.rv-search-select {
  width: 100%;
  min-width: 0;
}

.rv-search-select__trigger {
  min-height: var(--rv-control-compact);
}

.rv-search-select__palette {
  width: var(--rv-panel-width);
  /* stylelint-disable custom-property-pattern -- Reka measures the trigger and the room. */
  min-width: var(--reka-popover-trigger-width);
  max-width: calc(100vw - var(--rv-space-4));
  max-height: min(
    calc(var(--rv-overlay-height) + var(--rv-space-12)),
    var(--reka-popover-content-available-height, var(--rv-overlay-height))
  );
}

.rv-search-select__palette--field {
  width: var(--reka-popover-trigger-width);
  /* stylelint-enable custom-property-pattern */
}
</style>
