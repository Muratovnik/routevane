<script setup lang="ts">
defineProps<{
  describedBy?: string
  disabled?: boolean
  inputId: string
  invalid?: boolean
  rows?: number
}>()

const model = defineModel<string>({ required: true })
</script>

<template>
  <textarea
    :id="inputId"
    v-model="model"
    class="rv-textarea"
    :class="{ 'rv-textarea--invalid': invalid === true }"
    :aria-describedby="describedBy"
    :aria-invalid="invalid === true ? 'true' : undefined"
    :disabled="disabled === true"
    :rows="rows ?? 10"
    spellcheck="false"
  />
</template>

<style scoped>
.rv-textarea {
  width: 100%;
  min-width: 0;
  min-height: var(--rv-editor-min-height);
  padding: var(--rv-space-3);
  color: var(--rv-color-ink);
  font-size: var(--rv-text-dense);
  font-family: var(--rv-font-mono);
  line-height: var(--rv-leading-normal);
  resize: vertical;
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-md);
}

.rv-textarea:disabled {
  color: var(--rv-color-ink-tertiary);
  cursor: not-allowed;
}

.rv-textarea--invalid {
  border-color: var(--rv-color-status-failed);
}
</style>
