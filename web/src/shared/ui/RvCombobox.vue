<script setup lang="ts">
import {
  ComboboxAnchor,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxGroup,
  ComboboxInput,
  ComboboxItem,
  ComboboxLabel,
  ComboboxPortal,
  ComboboxRoot,
  ComboboxTrigger,
} from 'reka-ui'
import { computed, ref } from 'vue'

import type { ChoiceGroup, ChoiceOption } from '@/shared/ui/kinds'
import RvIcon from '@/shared/ui/RvIcon.vue'

/**
 * One choice from a set long enough to search.
 *
 * The field is the search: typing narrows the list in place, and a run that
 * keeps nothing stops being drawn instead of standing empty. Choosing puts the
 * chosen name back in the field, so a closed combobox reads as what it is —
 * a filled control, not a search box that lost its query.
 */
const props = defineProps<{
  describedBy?: string
  disabled?: boolean
  /** What to say when the query matches nothing. One line, not a lesson. */
  emptyLabel: string
  groups?: ChoiceGroup[]
  inputId: string
  invalid?: boolean
  options?: ChoiceOption[]
  placeholder: string
  /** Accessible name of the control that opens the list from the field. */
  toggleLabel: string
}>()

const model = defineModel<string>({ required: true })
const open = ref(false)
const activeDescendant = ref<string>()

const runs = computed<ChoiceGroup[]>(
  () => props.groups ?? [{ key: '', label: '', options: props.options ?? [] }],
)

const byValue = computed(
  () =>
    new Map(
      runs.value.flatMap((run) =>
        run.options.map((option) => [option.value, option] as const),
      ),
    ),
)

// Nothing chosen is the empty string here and an absent value in the primitive,
// exactly as it is for the select beside it.
const chosen = computed<string | undefined>({
  get: () => (model.value === '' ? undefined : model.value),
  set: (value) => {
    model.value = value ?? ''
  },
})

// What the field reads once the list is closed: the name of the choice, not
// its identifier.
function displayValue(value: unknown): string {
  return typeof value === 'string'
    ? (byValue.value.get(value)?.label ?? '')
    : ''
}

// What the query is matched against. The format extension is part of the
// name here, because ".bat" is how an operator recognises the file.
function searchText(option: ChoiceOption): string {
  return option.mono === undefined
    ? option.label
    : `${option.label} ${option.mono}`
}

// Reka keeps its virtual highlight when a portalled list is dismissed or an
// item is selected. Once the list unmounts that leaves aria-activedescendant
// pointing at an element that no longer exists, and the stale element also
// breaks the next keyboard navigation cycle. The root's pointer-leave path is
// its DOM path for clearing a highlight, so run it while the list is mounted.
function clearHighlight(): void {
  const element = document
    .getElementById(props.inputId)
    ?.closest('.rv-combobox')
  if (element instanceof HTMLElement) {
    element.dispatchEvent(new Event('pointerleave'))
  }
}

function rememberHighlight(item?: { ref?: HTMLElement }): void {
  activeDescendant.value = item?.ref?.id || undefined
}

function syncOpen(nextOpen: boolean): void {
  if (nextOpen) return

  activeDescendant.value = undefined
  clearHighlight()
}
</script>

<template>
  <ComboboxRoot
    v-model="chosen"
    v-model:open="open"
    class="rv-combobox"
    :disabled="disabled === true"
    open-on-click
    @highlight="rememberHighlight"
    @update:open="syncOpen"
  >
    <ComboboxAnchor class="rv-combobox__field">
      <ComboboxInput
        :id="inputId"
        :aria-activedescendant="open ? activeDescendant : undefined"
        :aria-describedby="describedBy"
        :aria-invalid="invalid === true ? 'true' : undefined"
        class="rv-combobox__input"
        :display-value="displayValue"
        :placeholder="placeholder"
      />
      <ComboboxTrigger :aria-label="toggleLabel" class="rv-combobox__toggle">
        <RvIcon name="chevron" />
      </ComboboxTrigger>
    </ComboboxAnchor>

    <ComboboxPortal>
      <ComboboxContent
        align="start"
        class="rv-combobox__panel"
        :collision-padding="8"
        position="popper"
        :side-offset="4"
      >
        <div class="rv-combobox__list">
          <ComboboxEmpty class="rv-combobox__empty">
            {{ emptyLabel }}
          </ComboboxEmpty>
          <ComboboxGroup
            v-for="run in runs"
            :key="run.key"
            class="rv-combobox__group"
          >
            <ComboboxLabel
              v-if="run.label !== ''"
              class="rv-combobox__group-label"
            >
              {{ run.label }}
            </ComboboxLabel>
            <ComboboxItem
              v-for="option in run.options"
              :key="option.value"
              class="rv-combobox__option"
              :disabled="option.disabled === true"
              :text-value="searchText(option)"
              :value="option.value"
            >
              <span class="rv-combobox__option-name">
                {{ option.label }}
                <span
                  v-if="option.mono !== undefined"
                  class="rv-combobox__mono"
                >
                  {{ option.mono }}
                </span>
              </span>
              <span v-if="option.note !== undefined" class="rv-combobox__note">
                {{ option.note }}
              </span>
              <span
                v-if="option.warning !== undefined"
                class="rv-combobox__warning"
              >
                {{ option.warning }}
              </span>
            </ComboboxItem>
          </ComboboxGroup>
        </div>
      </ComboboxContent>
    </ComboboxPortal>
  </ComboboxRoot>
</template>

<style scoped>
.rv-combobox {
  display: block;
  min-width: 0;
}

.rv-combobox__field {
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
  width: 100%;
  min-width: 0;
  min-height: var(--rv-control-touch);
  padding-right: var(--rv-space-1);
  background: var(--rv-color-canvas);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-sm);
}

.rv-combobox__field:focus-within {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-border-hair);
}

.rv-combobox__input {
  flex: 1;
  min-width: 0;

  /* The wrapper owns the height; a minimum here would add the wrapper's
     border on top of it and leave the field two pixels taller than the
     controls beside it. */
  align-self: stretch;
  padding: 0 var(--rv-space-3);
  color: var(--rv-color-ink);
  font: inherit;
  font-size: var(--rv-text-interface);
  background: transparent;
  border: 0;
  outline: 0;
}

.rv-combobox__input::placeholder {
  color: var(--rv-color-ink-tertiary);
}

.rv-combobox__input:disabled {
  color: var(--rv-color-ink-tertiary);
  cursor: not-allowed;
}

.rv-combobox__toggle {
  display: inline-flex;
  flex: none;
  align-items: center;
  justify-content: center;
  width: var(--rv-control-compact);
  height: var(--rv-control-compact);
  padding: 0;
  color: var(--rv-color-ink-tertiary);
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}
</style>

<!-- The panel and everything in it are styled outside the scoped block above,
     and they have to be. `PopperContent` renders its own positioning wrapper as
     this component's root element, so the scope attribute lands on that wrapper
     while these classes land on the element inside it: a scoped rule would
     compile to `.rv-combobox__panel[data-v-…]` and match nothing, leaving the
     open list as unpainted text over the page. The `rv-combobox__` names are
     this component's alone, so an unscoped block claims nothing else. -->
<style>
.rv-combobox__panel {
  z-index: 7;

  /* The panel is never narrower than the field it opens from. */
  /* stylelint-disable custom-property-pattern -- Reka names the trigger it measured. */
  min-width: max(
    var(--rv-overlay-min-width),
    var(--reka-combobox-trigger-width, 0px)
  );
  /* stylelint-enable custom-property-pattern */
  max-width: calc(100dvw - var(--rv-space-4));
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-md);
  box-shadow: var(--rv-shadow-raised);
}

.rv-combobox__list {
  display: grid;

  /* The positioner states how much room is left on the side it chose;
     a panel taller than that would bleed past the viewport edge. */
  /* stylelint-disable custom-property-pattern -- Reka names the room it measured. */
  max-height: min(
    var(--rv-overlay-height),
    var(--reka-combobox-content-available-height, var(--rv-overlay-height))
  );
  /* stylelint-enable custom-property-pattern */
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: var(--rv-space-1);
}

.rv-combobox__empty {
  padding: var(--rv-space-3);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.rv-combobox__group-label {
  padding: var(--rv-space-2) var(--rv-space-3) var(--rv-space-1);
  color: var(--rv-color-ink-tertiary);
  font-weight: 600;
  font-size: var(--rv-text-meta);
}

.rv-combobox__option {
  display: grid;
  gap: var(--rv-space-1);
  align-content: center;
  min-height: var(--rv-control-touch);
  padding: var(--rv-space-2) var(--rv-space-3);
  color: var(--rv-color-ink);
  font-size: var(--rv-text-dense);
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.rv-combobox__option[data-highlighted] {
  background: var(--rv-color-surface-hover);
  outline: none;
}

.rv-combobox__option[data-state='checked'] {
  background: var(--rv-color-surface-selected);
}

.rv-combobox__option[data-disabled] {
  color: var(--rv-color-ink-tertiary);
  cursor: not-allowed;
}

.rv-combobox__option-name {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  align-items: baseline;
  min-width: 0;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.rv-combobox__mono {
  color: var(--rv-color-ink-muted);
  font-weight: 400;
  font-size: var(--rv-text-meta);
  font-family: var(--rv-font-mono);
}

.rv-combobox__note {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}

.rv-combobox__warning {
  color: var(--rv-color-status-warning);
  font-weight: 600;
  font-size: var(--rv-text-meta);
}
</style>
