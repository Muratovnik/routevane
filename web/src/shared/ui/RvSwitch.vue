<script setup lang="ts">
import { useId } from 'vue'

/**
 * One standing thing that is either on or off.
 *
 * A switch is for a state the operator sets and leaves set — membership, a
 * preference — and it says what it is by its own position, so nothing beside it
 * has to restate the state in words. A control whose label changes with the
 * state ("Add" / "Remove") is a command, not this: use a button for those.
 *
 * Nuxt UI's `USwitch` owns the role, the checked state it reports, Space and
 * Enter, the label association and the cursor an unavailable switch shows.
 * Everything visible is ours: its parts are named with the classes below and
 * drawn with Routevane tokens.
 */
const props = withDefaults(
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

// The library wires a description to its control only inside one of its own
// form fields, so the facade names the note and points the control at it.
const noteId = `${id}-note`

const parts = {
  base: 'rv-switch__control',
  thumb: 'rv-switch__thumb',
  wrapper: 'rv-switch__copy',
  label: 'rv-switch__label',
  description: 'rv-switch__note',
}
</script>

<template>
  <USwitch
    :id="id"
    v-model="checked"
    :aria-describedby="props.description === undefined ? undefined : noteId"
    :aria-label="props.label"
    class="rv-switch"
    :class="{ 'rv-switch--disabled': props.disabled === true }"
    :description="props.description"
    :disabled="props.disabled === true"
    :label="props.label"
    :ui="parts"
  >
    <template v-if="props.description !== undefined" #description>
      <span :id="noteId">{{ props.description }}</span>
    </template>
  </USwitch>
</template>

<!-- The library's own parts sit outside this component's scope, so they are
     reached through the root it renders. These rules sit outside the library's
     cascade layer and replace its look: its lowered opacity, its shadowed thumb,
     its smaller copy and the gap it leaves as a margin. -->
<style scoped>
.rv-switch {
  display: flex;
  gap: var(--rv-space-3);
  align-items: center;
  min-width: 0;

  /* An unavailable switch is not faded; it states itself below. */
  opacity: 1;
}

.rv-switch :deep(.rv-switch__control) {
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

.rv-switch :deep(.rv-switch__control[data-state='checked']) {
  background: var(--rv-color-accent);
  border-color: var(--rv-color-accent);
}

.rv-switch :deep(.rv-switch__control:disabled) {
  background: transparent;
  border-color: var(--rv-color-rule);
  cursor: not-allowed;
}

/* An unavailable switch is flat, as an unavailable button is: no ground and the
   faint rule for an edge. Its position still says whether it is on, and an
   unavailable one that is on keeps the accent at low emphasis, as an
   unavailable primary button does. */
.rv-switch :deep(.rv-switch__control[data-state='checked']:disabled) {
  background: var(--rv-color-accent-quiet);
  border-color: var(--rv-color-rule);
}

/* The thumb travels with the `translate` property, which is the one the
   library moves it with too; stating it here replaces the library's travel
   rather than adding to it. */
.rv-switch :deep(.rv-switch__thumb) {
  display: block;
  width: 0.875rem;
  height: 0.875rem;
  margin-inline-start: 0.1875rem;
  background: var(--rv-color-ink);
  border-radius: var(--rv-radius-lg);
  box-shadow: none;
  translate: none;
  transition: translate var(--rv-motion-fast) var(--rv-motion-ease-out);
}

/* The thumb travels the track's own width minus its own, so the geometry holds
   when the reader enlarges the text and the rem values grow with it. */
.rv-switch :deep(.rv-switch__control[data-state='checked'] .rv-switch__thumb) {
  background: var(--rv-color-canvas);
  translate: calc(2.25rem - 0.875rem - 0.375rem) 0;
}

.rv-switch :deep(.rv-switch__control:disabled .rv-switch__thumb) {
  background: var(--rv-color-ink-disabled);
}

.rv-switch :deep(.rv-switch__copy) {
  display: grid;
  gap: var(--rv-space-1);
  min-width: 0;
  margin: 0;
  font-size: inherit;
  line-height: inherit;
}

.rv-switch :deep(.rv-switch__label) {
  color: inherit;
  font-weight: 600;
  cursor: pointer;
  overflow-wrap: anywhere;
}

.rv-switch--disabled :deep(.rv-switch__label) {
  cursor: not-allowed;
}

.rv-switch :deep(.rv-switch__note) {
  margin: 0;
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}
</style>
