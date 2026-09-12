<script setup lang="ts">
import { SwitchRoot, SwitchThumb } from 'reka-ui'
import { useId } from 'vue'

/**
 * One standing thing that is either on or off.
 *
 * A switch is for a state the operator sets and leaves set — membership, a
 * preference — and it says what it is by its own position, so nothing beside it
 * has to restate the state in words. A control whose label changes with the
 * state ("Add" / "Remove") is a command, not this: use a button for those.
 *
 * The primitive underneath owns the role, the checked state it reports, Space
 * and Enter, and the label association. Everything visible is ours.
 */
withDefaults(
  defineProps<{
    disabled?: boolean
    /** Standing background for the choice, read out with its label. */
    description?: string
    label: string
  }>(),
  { description: undefined },
)

const checked = defineModel<boolean>({ required: true })
const id = useId()
</script>

<template>
  <div class="rv-switch">
    <SwitchRoot
      :id="id"
      v-model="checked"
      :aria-describedby="description === undefined ? undefined : `${id}-note`"
      :aria-label="label"
      class="rv-switch__control"
      :disabled="disabled"
    >
      <SwitchThumb class="rv-switch__thumb" />
    </SwitchRoot>
    <div class="rv-switch__copy">
      <label class="rv-switch__label" :for="id">{{ label }}</label>
      <small v-if="description !== undefined" :id="`${id}-note`">
        {{ description }}
      </small>
    </div>
  </div>
</template>

<style scoped>
.rv-switch {
  display: flex;
  gap: var(--rv-space-3);
  align-items: center;
  min-width: 0;
}

.rv-switch__control {
  flex: none;
  position: relative;
  width: 2.25rem;
  height: 1.25rem;
  padding: 0;
  background: var(--rv-color-surface-muted);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-lg);
  cursor: pointer;
  transition: background var(--rv-motion-fast) var(--rv-motion-ease-out);
}

.rv-switch__control[data-state='checked'] {
  background: var(--rv-color-accent);
  border-color: var(--rv-color-accent);
}

.rv-switch__control:disabled {
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.rv-switch__thumb {
  display: block;
  width: 0.875rem;
  height: 0.875rem;
  margin-inline-start: 0.1875rem;
  background: var(--rv-color-ink);
  border-radius: var(--rv-radius-lg);
  transition: transform var(--rv-motion-fast) var(--rv-motion-ease-out);
}

/* The thumb travels the track's own width minus its own, so the geometry holds
   when the reader enlarges the text and the rem values grow with it. */
.rv-switch__control[data-state='checked'] .rv-switch__thumb {
  background: var(--rv-color-canvas);
  transform: translateX(calc(2.25rem - 0.875rem - 0.375rem));
}

.rv-switch__copy {
  display: grid;
  gap: var(--rv-space-1);
  min-width: 0;
}

.rv-switch__label {
  font-weight: 600;
  cursor: pointer;
  overflow-wrap: anywhere;
}

.rv-switch__copy small {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}
</style>
