<script setup lang="ts">
/**
 * A text control with the product's metrics. Values an operator copies from a
 * device — addresses, interface names — are mono, because that is what makes a
 * character-by-character comparison possible.
 */
defineProps<{
  autocomplete?: string
  describedBy?: string
  disabled?: boolean
  inputId: string
  invalid?: boolean
  placeholder?: string
  type?: 'text' | 'password'
}>()

const model = defineModel<string>({ required: true })
</script>

<template>
  <input
    :id="inputId"
    v-model="model"
    class="rv-input"
    :class="{ 'rv-input--invalid': invalid === true }"
    :aria-describedby="describedBy"
    :aria-invalid="invalid === true ? 'true' : undefined"
    :autocomplete="autocomplete ?? 'off'"
    :disabled="disabled === true"
    :placeholder="placeholder"
    spellcheck="false"
    :type="type ?? 'text'"
  />
</template>

<style scoped>
.rv-input {
  width: 100%;
  min-width: 0;
  min-height: var(--rv-control-touch);
  padding: 0 var(--rv-space-3);
  color: var(--rv-color-ink);
  font-size: var(--rv-text-emphasis);
  font-family: var(--rv-font-mono);
  background: var(--rv-color-canvas);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-sm);
}

.rv-input::placeholder {
  color: var(--rv-color-ink-tertiary);
}

.rv-input:disabled {
  color: var(--rv-color-ink-tertiary);
  cursor: not-allowed;
}

.rv-input--invalid {
  border-color: var(--rv-color-status-failed);
}
</style>
