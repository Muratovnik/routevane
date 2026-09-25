<script setup lang="ts">
import {
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectItemIndicator,
  SelectItemText,
  SelectLabel,
  SelectPortal,
  SelectRoot,
  SelectTrigger,
  SelectValue,
} from 'reka-ui'
import { computed } from 'vue'

import type { ChoiceGroup, ChoiceOption } from '@/shared/ui/types'
import RvSearchSelect from '@/shared/ui/RvSearchSelect.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'

/**
 * One choice from a known set.
 *
 * A native `<select>` cannot be given this product's ink, ground or metrics —
 * the operating system draws its list — so the list is drawn here and the
 * keyboard contract a select owes is supplied by the primitive underneath:
 * arrows, Home and End, type-ahead, Enter and Escape, and a panel that is a
 * viewport overlay rather than part of whatever it was opened inside.
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

const runs = computed<ChoiceGroup[]>(
  () => props.groups ?? [{ key: '', label: '', options: props.options ?? [] }],
)

// Reka carries "nothing chosen" as an absent value; this surface carries it as
// the empty string, because that is what an unset native select answered and
// what every caller already stores.
const chosen = computed<string | undefined>({
  get: () => (model.value === '' ? undefined : model.value),
  set: (value) => {
    model.value = value ?? ''
  },
})

const empty = computed(() =>
  runs.value.every((run) => run.options.length === 0),
)
</script>

<template>
  <RvSearchSelect
    v-if="searchable"
    v-bind="$props"
    v-model="model"
    :size="size ?? 'default'"
  />
  <SelectRoot
    v-else
    v-model="chosen"
    :disabled="disabled === true || loading === true || empty"
  >
    <SelectTrigger
      :id="inputId"
      :aria-describedby="describedBy"
      :aria-invalid="invalid === true ? 'true' : undefined"
      :aria-labelledby="labelledBy"
      :aria-busy="loading === true ? 'true' : undefined"
      :aria-label="loading === true ? loadingLabel : undefined"
      class="rv-select__trigger"
      :class="{
        'rv-select__trigger--invalid': invalid === true,
        'rv-select__trigger--compact': size === 'compact',
      }"
    >
      <SelectValue class="rv-select__value" :placeholder="placeholder" />
      <span
        v-if="loading === true"
        aria-hidden="true"
        class="rv-select__spinner"
      />
      <RvIcon v-else class="rv-select__chevron" name="chevron" />
    </SelectTrigger>
    <SelectPortal>
      <SelectContent
        align="start"
        class="rv-select__panel"
        :collision-padding="8"
        position="popper"
        :side-offset="4"
      >
        <div class="rv-select__list">
          <SelectGroup
            v-for="run in runs"
            :key="run.key"
            class="rv-select__group"
          >
            <SelectLabel v-if="run.label !== ''" class="rv-select__group-label">
              {{ run.label }}
            </SelectLabel>
            <SelectItem
              v-for="option in run.options"
              :key="option.value"
              class="rv-select__option"
              :disabled="option.disabled === true"
              :value="option.value"
            >
              <SelectItemIndicator class="rv-select__mark">
                <RvIcon name="check" />
              </SelectItemIndicator>
              <span class="rv-select__option-copy">
                <!-- Only the name lives in the item's text: it is what the
                     closed trigger reads back. -->
                <SelectItemText class="rv-select__option-name">
                  {{ option.label }}
                  <span
                    v-if="option.mono !== undefined"
                    class="rv-select__mono"
                  >
                    {{ option.mono }}
                  </span>
                </SelectItemText>
                <span v-if="option.note !== undefined" class="rv-select__note">
                  {{ option.note }}
                </span>
                <span
                  v-if="option.warning !== undefined"
                  class="rv-select__warning"
                >
                  {{ option.warning }}
                </span>
              </span>
            </SelectItem>
          </SelectGroup>
        </div>
      </SelectContent>
    </SelectPortal>
  </SelectRoot>
</template>

<style scoped>
/* No hover state: a `<label for>` forwards its own `:hover` to the control it
   names, so a rule here would light the trigger up from anywhere on the label
   line above it — a field the pointer is nowhere near. */
.rv-select__trigger {
  display: flex;
  gap: var(--rv-space-3);
  align-items: center;
  justify-content: space-between;
  width: 100%;
  min-width: 0;
  min-height: var(--rv-control-default);
  padding: 0 var(--rv-space-3);
  color: var(--rv-color-ink);
  font: inherit;
  font-size: var(--rv-text-interface);
  text-align: start;
  background: var(--rv-color-field);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.rv-select__trigger--compact {
  min-height: var(--rv-control-compact);
  font-size: var(--rv-text-dense);
  border-radius: var(--rv-radius-md);
}

.rv-select__trigger:disabled {
  color: var(--rv-color-ink-tertiary);
  cursor: not-allowed;
}

.rv-select__trigger--invalid {
  border-color: var(--rv-color-status-failed);
}

.rv-select__value {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rv-select__value[data-placeholder] {
  color: var(--rv-color-ink-tertiary);
}

.rv-select__chevron {
  flex: none;
  color: var(--rv-color-ink-tertiary);
}

.rv-select__spinner {
  flex: none;
  width: var(--rv-control-choice);
  height: var(--rv-control-choice);
  border: var(--rv-border-hair) solid currentcolor;
  border-right-color: transparent;
  border-radius: 50%;
  animation: rv-select-spin var(--rv-motion-working) linear infinite;
}

@keyframes rv-select-spin {
  to {
    transform: rotate(1turn);
  }
}
</style>

<!-- The panel and everything in it are styled outside the scoped block above,
     and they have to be. `PopperContent` renders its own positioning wrapper as
     this component's root element, so the scope attribute lands on that wrapper
     while these classes land on the element inside it: a scoped rule would
     compile to `.rv-select__panel[data-v-…]` and match nothing, leaving the open
     list as unpainted text over the page. The `rv-select__` names are this
     component's alone, so an unscoped block claims nothing else. -->
<style>
/* The panel is a viewport overlay: it is bounded by the screen rather than by
   the table or sheet its trigger happens to sit in, and it is never wider than
   the screen at 320 pixels. */
.rv-select__panel {
  z-index: 7;

  /* The panel is never narrower than the field it opens from. */
  /* stylelint-disable custom-property-pattern -- Reka names the trigger it measured. */
  min-width: max(
    var(--rv-overlay-min-width),
    var(--reka-select-trigger-width, 0px)
  );
  /* stylelint-enable custom-property-pattern */
  max-width: calc(100dvw - var(--rv-space-4));
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-md);
  box-shadow: var(--rv-shadow-raised);
  animation: rv-select-in var(--rv-motion-fast) var(--rv-motion-ease-out);
}

@keyframes rv-select-in {
  from {
    box-shadow: var(--rv-shadow-panel);
  }
}

.rv-select__list {
  display: grid;

  /* The positioner states how much room is left on the side it chose;
     a panel taller than that would bleed past the viewport edge. */
  /* stylelint-disable custom-property-pattern -- Reka names the room it measured. */
  max-height: min(
    var(--rv-overlay-height),
    var(--reka-select-content-available-height, var(--rv-overlay-height))
  );
  /* stylelint-enable custom-property-pattern */
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: var(--rv-space-1);
}

.rv-select__group-label {
  padding: var(--rv-space-2) var(--rv-space-3) var(--rv-space-1);
  color: var(--rv-color-ink-tertiary);
  font-weight: 600;
  font-size: var(--rv-text-meta);
}

/* The trailing mark never shifts the aligned option names. */
.rv-select__option {
  display: grid;
  grid-template-columns: minmax(0, 1fr) var(--rv-control-choice);
  gap: var(--rv-space-2);
  align-items: center;
  min-height: var(--rv-control-touch);
  padding: var(--rv-space-2) var(--rv-space-3);
  color: var(--rv-color-ink);
  font-size: var(--rv-text-dense);
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.rv-select__option[data-highlighted] {
  background: var(--rv-color-surface-hover);
  outline: none;
}

.rv-select__option[data-state='checked'] {
  background: var(--rv-color-surface-selected);
}

.rv-select__option[data-disabled] {
  color: var(--rv-color-ink-tertiary);
  cursor: not-allowed;
}

.rv-select__mark {
  display: inline-flex;
  grid-column: 2;
  grid-row: 1;
  align-items: center;
  justify-content: center;
  color: var(--rv-color-accent-ink);
}

.rv-select__option-copy {
  display: grid;
  grid-column: 1;
  grid-row: 1;
  gap: var(--rv-space-1);
  min-width: 0;
}

.rv-select__option-name {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  align-items: baseline;
  min-width: 0;
  overflow-wrap: anywhere;
}

.rv-select__mono {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-meta);
  font-family: var(--rv-font-mono);
}

.rv-select__note {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}

.rv-select__warning {
  color: var(--rv-color-status-warning);
  font-weight: 600;
  font-size: var(--rv-text-meta);
}
</style>
