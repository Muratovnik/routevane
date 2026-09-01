<script setup lang="ts">
/**
 * Progressive detail. Addresses, reason codes and file formats live behind one
 * of these rather than on the path a first-time operator walks — and they are
 * always one click away rather than absent.
 */
const props = defineProps<{
  hint?: string
  open?: boolean
  summary: string
}>()

const emit = defineEmits<{
  toggle: [open: boolean]
}>()

function onToggle(event: Event): void {
  const target = event.currentTarget
  if (target instanceof HTMLDetailsElement && target.open !== props.open) {
    emit('toggle', target.open)
  }
}
</script>

<template>
  <details class="rv-disclosure" :open="open === true" @toggle="onToggle">
    <summary class="rv-disclosure__summary">
      <span class="rv-disclosure__label">
        <span class="rv-disclosure__title">{{ summary }}</span>
        <span v-if="hint !== undefined" class="rv-disclosure__hint">
          {{ hint }}
        </span>
      </span>
    </summary>
    <div class="rv-disclosure__body">
      <slot />
    </div>
  </details>
</template>

<style scoped>
.rv-disclosure {
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.rv-disclosure__summary {
  min-height: var(--rv-row-default);
  padding: var(--rv-space-4) 0;
  cursor: pointer;
}

.rv-disclosure__summary::marker {
  color: var(--rv-color-ink-tertiary);
}

.rv-disclosure__label {
  display: inline-grid;
  gap: var(--rv-space-1);
  vertical-align: top;
}

.rv-disclosure__title {
  font-weight: 600;
  font-size: var(--rv-text-interface);
}

.rv-disclosure__hint {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.rv-disclosure__body {
  display: grid;
  gap: var(--rv-space-4);
  padding-bottom: var(--rv-space-5);
}
</style>
