<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'

import RvIcon from '@/shared/ui/RvIcon.vue'
/**
 * Progressive detail. Addresses, reason codes and file formats live behind one
 * of these rather than on the path a first-time operator walks — and they are
 * always one click away rather than absent.
 */
const props = withDefaults(
  defineProps<{
    hint?: string
    /** Group with whitespace when a containing page already supplies structure. */
    borderless?: boolean
    open?: boolean
    summary: string
  }>(),
  { hint: undefined, open: undefined },
)

const emit = defineEmits<{
  toggle: [open: boolean]
}>()

const localOpen = ref(props.open ?? false)
const expanded = computed(() => props.open ?? localOpen.value)
const baseID = useId()

watch(
  () => props.open,
  (open) => {
    if (open !== undefined) localOpen.value = open
  },
)

const toggle = (): void => {
  const next = !expanded.value
  if (props.open === undefined) localOpen.value = next
  emit('toggle', next)
}
</script>

<template>
  <section
    class="rv-disclosure"
    :class="{ 'rv-disclosure--borderless': borderless }"
  >
    <button
      :id="`${baseID}-trigger`"
      :aria-controls="`${baseID}-panel`"
      :aria-expanded="expanded"
      class="rv-disclosure__summary"
      type="button"
      @click="toggle"
    >
      <RvIcon class="rv-disclosure__chevron" name="chevron" />
      <span class="rv-disclosure__label">
        <span class="rv-disclosure__title">{{ summary }}</span>
        <span v-if="hint !== undefined" class="rv-disclosure__hint">
          {{ hint }}
        </span>
      </span>
    </button>
    <div
      :id="`${baseID}-panel`"
      :aria-hidden="!expanded"
      :aria-labelledby="`${baseID}-trigger`"
      class="rv-disclosure__panel"
      :class="{ 'rv-disclosure__panel--open': expanded }"
      :inert="expanded ? undefined : true"
      role="region"
    >
      <div class="rv-disclosure__clip">
        <div class="rv-disclosure__body">
          <slot />
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.rv-disclosure {
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.rv-disclosure--borderless {
  border-top: 0;
}

.rv-disclosure__summary {
  display: flex;
  gap: var(--rv-space-3);
  align-items: flex-start;
  width: 100%;
  min-height: var(--rv-row-default);
  padding: var(--rv-space-4) 0;
  color: var(--rv-color-ink);
  font: inherit;
  text-align: start;
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.rv-disclosure__summary:hover {
  color: var(--rv-color-accent-ink);
}

.rv-disclosure__chevron {
  flex: none;
  margin-top: var(--rv-space-1);
  color: var(--rv-color-ink-tertiary);
  transform: rotate(-90deg);
  transition: transform var(--rv-motion-fast) var(--rv-motion-ease-out);
}

.rv-disclosure__summary[aria-expanded='true'] .rv-disclosure__chevron {
  transform: rotate(0);
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

.rv-disclosure__panel {
  display: grid;
  grid-template-rows: 0fr;
  opacity: 0;
  visibility: hidden;
  transition:
    grid-template-rows var(--rv-motion-normal) var(--rv-motion-ease-out),
    opacity var(--rv-motion-fast) var(--rv-motion-ease-out),
    visibility 0s linear var(--rv-motion-normal);
}

.rv-disclosure__panel--open {
  grid-template-rows: 1fr;
  opacity: 1;
  visibility: visible;
  transition-delay: 0s;
}

.rv-disclosure__clip {
  min-height: 0;
  overflow: hidden;
}
</style>
