<script setup lang="ts">
import { computed } from 'vue'

/**
 * One form field: label, control, hint, and — when it exists — the error, next
 * to the control that caused it rather than at the bottom of the form. The slot
 * receives the ids to point `aria-describedby` at, so the wiring cannot drift
 * from what is rendered.
 */
const props = defineProps<{
  error?: string
  hint?: string
  inputId: string
  label: string
}>()

const hintId = computed(() => `${props.inputId}-hint`)
const errorId = computed(() => `${props.inputId}-error`)
const describedBy = computed(() => {
  const ids: string[] = []
  if (props.hint !== undefined && props.hint !== '') ids.push(hintId.value)
  if (props.error !== undefined && props.error !== '') ids.push(errorId.value)
  return ids.length === 0 ? undefined : ids.join(' ')
})
</script>

<template>
  <div class="rv-field">
    <label class="rv-field__label" :for="inputId">{{ label }}</label>
    <slot
      :described-by="describedBy"
      :invalid="error !== undefined && error !== ''"
    />
    <p
      v-if="hint !== undefined && hint !== ''"
      :id="hintId"
      class="rv-field__hint"
    >
      {{ hint }}
    </p>
    <p
      v-if="error !== undefined && error !== ''"
      :id="errorId"
      class="rv-field__error"
    >
      {{ error }}
    </p>
  </div>
</template>

<style scoped>
.rv-field {
  display: grid;
  gap: var(--rv-space-2);
  min-width: 0;
}

.rv-field__label {
  color: var(--rv-color-ink);
  font-weight: 600;
  font-size: var(--rv-text-dense);
  letter-spacing: var(--rv-tracking-label);
}

.rv-field__hint {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.rv-field__error {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-status-failed);
  font-size: var(--rv-text-dense);
}
</style>
