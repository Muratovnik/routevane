<script setup lang="ts">
import { ref } from 'vue'

import RvIcon from '@/shared/ui/RvIcon.vue'

/**
 * A file stays a browser-owned choice, but it should look and behave like the
 * rest of the product. The native input remains the source of the file picker;
 * this surface supplies its visible target, selected name and drop affordance.
 */
const props = defineProps<{
  accept?: string
  actionLabel: string
  disabled?: boolean
  emptyLabel: string
  fileName?: string
  hint?: string
  inputId: string
  label: string
}>()

const emit = defineEmits<{ select: [file: File] }>()
const input = ref<HTMLInputElement | null>(null)
const dragging = ref(false)

const openPicker = (): void => {
  if (!props.disabled) input.value?.click()
}

const choose = (file: File | null): void => {
  dragging.value = false
  if (file !== null && !props.disabled) emit('select', file)
}

const onChange = (event: Event): void => {
  const field = event.target
  if (!(field instanceof HTMLInputElement)) return
  choose(field.files?.item(0) ?? null)
  field.value = ''
}

const onDrop = (event: DragEvent): void => {
  event.preventDefault()
  choose(event.dataTransfer?.files.item(0) ?? null)
}
</script>

<template>
  <div class="rv-file-picker">
    <label :for="inputId" class="rv-file-picker__label">{{ label }}</label>
    <input
      :id="inputId"
      ref="input"
      :accept="accept"
      class="rv-file-picker__input"
      :disabled="disabled === true"
      tabindex="-1"
      type="file"
      @change="onChange"
    />
    <button
      :aria-label="actionLabel"
      class="rv-file-picker__surface"
      :class="{ 'rv-file-picker__surface--dragging': dragging }"
      :disabled="disabled === true"
      type="button"
      @click="openPicker"
      @dragenter.prevent="dragging = true"
      @dragleave.prevent="dragging = false"
      @dragover.prevent="dragging = true"
      @drop="onDrop"
    >
      <span class="rv-file-picker__icon"><RvIcon name="file" /></span>
      <span class="rv-file-picker__copy">
        <strong>{{ fileName || emptyLabel }}</strong>
        <small v-if="hint !== undefined">{{ hint }}</small>
      </span>
      <span class="rv-file-picker__action">{{ actionLabel }}</span>
    </button>
  </div>
</template>

<style scoped>
.rv-file-picker {
  container: file-picker / inline-size;
  position: relative;
  width: min(var(--rv-measure-field), 100%);
}

.rv-file-picker__input {
  position: absolute;
  width: var(--rv-border-hair);
  height: var(--rv-border-hair);
  overflow: hidden;
  clip-path: inset(50%);
}

.rv-file-picker__label {
  position: absolute;
  width: var(--rv-border-hair);
  height: var(--rv-border-hair);
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}

.rv-file-picker__surface {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: var(--rv-space-3);
  align-items: center;
  width: 100%;
  min-height: var(--rv-row-comfortable);
  padding: var(--rv-space-3) var(--rv-space-4);
  color: var(--rv-color-ink);
  font: inherit;
  text-align: start;
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) dashed var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-md);
  cursor: pointer;
  transition:
    background var(--rv-motion-fast) var(--rv-motion-ease-out),
    border-color var(--rv-motion-fast) var(--rv-motion-ease-out);
}

.rv-file-picker__surface:disabled {
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.rv-file-picker__surface:hover:not(:disabled),
.rv-file-picker__surface--dragging {
  background: var(--rv-color-surface-hover);
  border-color: var(--rv-color-accent);
}

.rv-file-picker__icon {
  display: inline-flex;
  color: var(--rv-color-accent-ink);
  font-size: var(--rv-text-section);
}

.rv-file-picker__copy {
  display: grid;
  gap: var(--rv-space-1);
  min-width: 0;
}

.rv-file-picker__copy strong {
  font-size: var(--rv-text-interface);
  overflow-wrap: anywhere;
}

.rv-file-picker__copy small {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}

.rv-file-picker__action {
  color: var(--rv-color-accent-ink);
  font-weight: 600;
  font-size: var(--rv-text-dense);
  white-space: nowrap;
}

@container file-picker (width <= 30rem) {
  .rv-file-picker__surface {
    grid-template-columns: auto minmax(0, 1fr);
  }

  .rv-file-picker__action {
    grid-column: 2;
  }
}
</style>
