<script setup lang="ts">
import RvIcon from '@/shared/ui/RvIcon.vue'
import type { SegmentOption } from '@/shared/ui/types'

/**
 * A two-or-three way choice the operator makes about the surface itself: how
 * much detail, which ground, which language, automatic or by hand. Radio inputs
 * carry the semantics, so keyboard and screen-reader behaviour is the platform's
 * rather than ours.
 *
 * The chosen option is told from the others by more than a shade: an accent
 * edge that stands off the track by the 3:1 non-text contrast WCAG asks for, and
 * a check mark. The mark holds its place in every option and is only drawn on
 * the chosen one, so choosing another never moves the words.
 */
const props = withDefaults(
  defineProps<{
    label: string
    modelValue: string
    name: string
    options: SegmentOption[]
    // A control whose value is still being read from the server is disabled
    // rather than hidden: the choice exists, and it is not answerable yet.
    disabled?: boolean
    labelHidden?: boolean
  }>(),
  { disabled: false },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const select = (value: string, event: Event): void => {
  // Native activation checks a radio before change fires. Restore the controlled
  // value even when its owner defers or rejects the change: an unchanged prop
  // alone does not cause Vue to patch the browser's optimistic checked state.
  const input = event.currentTarget
  if (input instanceof HTMLInputElement) {
    input
      .closest('fieldset')
      ?.querySelectorAll<HTMLInputElement>('input[type="radio"]')
      .forEach((radio) => {
        radio.checked = radio.value === props.modelValue
      })
  }
  if (props.disabled) return
  if (value !== props.modelValue) emit('update:modelValue', value)
}
</script>

<template>
  <fieldset class="rv-segmented" :disabled="disabled">
    <legend
      class="rv-segmented__legend"
      :class="{ 'rv-visually-hidden': labelHidden }"
    >
      {{ label }}
    </legend>
    <div class="rv-segmented__track">
      <label
        v-for="option in options"
        :key="option.value"
        class="rv-segmented__option"
        :class="{
          'rv-segmented__option--active': option.value === modelValue,
        }"
      >
        <input
          class="rv-segmented__input"
          :checked="option.value === modelValue"
          :name="name"
          type="radio"
          :value="option.value"
          @change="select(option.value, $event)"
        />
        <span class="rv-segmented__body">
          <span
            aria-hidden="true"
            class="rv-segmented__mark"
            :data-testid="`rv-segmented-mark-${option.value}`"
          >
            <RvIcon name="check" />
          </span>
          <span>{{ option.label }}</span>
        </span>
      </label>
    </div>
  </fieldset>
</template>

<style scoped>
.rv-segmented {
  display: grid;
  gap: var(--rv-space-2);
  justify-items: start;
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}

.rv-segmented__legend {
  padding: 0;
  color: var(--rv-color-ink);
  font-weight: 600;
  font-size: var(--rv-text-dense);
}

.rv-segmented__track {
  display: inline-flex;

  /* Four labelled options do not fit a 320px screen on one line, and a control
     that scrolled sideways would hide the option nobody scrolled to. */
  flex-wrap: wrap;
  gap: var(--rv-segmented-inset);
  max-width: 100%;
  padding: var(--rv-segmented-inset);
  background: var(--rv-color-surface-muted);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-md);
}

.rv-segmented__legend:not(.rv-visually-hidden) {
  margin-block-end: var(--rv-space-2);
}

.rv-segmented__option {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: var(--rv-control-default);
  padding: 0 var(--rv-space-4);
  color: var(--rv-color-ink-muted);
  font-weight: 600;
  font-size: var(--rv-text-dense);

  /* The edge exists on every option and is only coloured on the chosen one, so
     the chosen option is no larger than the others. */
  border: var(--rv-border-hair) solid transparent;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.rv-segmented:disabled .rv-segmented__option {
  position: relative;
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.rv-segmented:not(:disabled) .rv-segmented__option:hover {
  color: var(--rv-color-ink);
}

.rv-segmented__option--active {
  color: var(--rv-color-ink);
  background: var(--rv-color-accent-quiet);
  border-color: var(--rv-color-accent);
}

.rv-segmented__body {
  display: inline-flex;
  gap: var(--rv-space-2);
  align-items: center;
}

.rv-segmented__mark {
  display: inline-flex;
  visibility: hidden;
  color: var(--rv-color-accent-ink);
}

.rv-segmented__option--active .rv-segmented__mark {
  visibility: visible;
}

.rv-segmented__input {
  position: absolute;
  width: 0.0625rem;
  height: 0.0625rem;
  margin: 0;
  padding: 0;
  overflow: hidden;
  white-space: nowrap;
  border: 0;
  clip-path: inset(50%);
}

.rv-segmented__input:focus-visible + .rv-segmented__body {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: 0.1875rem;
}
</style>
