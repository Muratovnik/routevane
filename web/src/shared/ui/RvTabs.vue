<script setup lang="ts">
import { ref } from 'vue'

import type { TabItem } from '@/shared/ui/kinds'

/**
 * The tab bar of one object's facets. The host renders each panel itself with
 * role="tabpanel", id `rv-panel-{tab.id}` and aria-labelledby `rv-tab-{tab.id}`
 * — the two halves meet through those ids.
 *
 * Selection follows focus: arrows move between tabs and select as they go,
 * which is the native tabs behavior when panels are cheap to show.
 */
const props = defineProps<{
  label: string
  modelValue: string
  tabs: TabItem[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const buttons = ref<HTMLButtonElement[]>([])

const select = (id: string): void => {
  if (id !== props.modelValue) emit('update:modelValue', id)
}

const onKeydown = (event: KeyboardEvent, index: number): void => {
  const last = props.tabs.length - 1
  let next: number | null = null
  if (event.key === 'ArrowRight') next = index === last ? 0 : index + 1
  if (event.key === 'ArrowLeft') next = index === 0 ? last : index - 1
  if (event.key === 'Home') next = 0
  if (event.key === 'End') next = last
  if (next === null) return
  event.preventDefault()
  const tab = props.tabs[next]
  if (tab === undefined) return
  select(tab.id)
  buttons.value[next]?.focus()
}
</script>

<template>
  <div :aria-label="label" class="rv-tabs" role="tablist">
    <button
      v-for="(tab, index) in tabs"
      :id="`rv-tab-${tab.id}`"
      :key="tab.id"
      ref="buttons"
      :aria-controls="`rv-panel-${tab.id}`"
      :aria-selected="tab.id === modelValue"
      class="rv-tabs__tab"
      role="tab"
      :tabindex="tab.id === modelValue ? 0 : -1"
      type="button"
      @click="select(tab.id)"
      @keydown="onKeydown($event, index)"
    >
      {{ tab.label }}
    </button>
  </div>
</template>

<style scoped>
.rv-tabs {
  display: flex;
  gap: var(--rv-space-2);
  max-width: 100%;
  overflow-x: auto;
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.rv-tabs__tab {
  position: relative;
  flex: none;
  min-height: var(--rv-control-touch);
  padding: 0 var(--rv-space-3);
  color: var(--rv-color-ink-muted);
  font-weight: 600;
  font-size: var(--rv-text-interface);
  font-family: inherit;
  background: transparent;
  border: 0;
  cursor: pointer;
}

.rv-tabs__tab:hover {
  color: var(--rv-color-ink);
}

.rv-tabs__tab:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: -0.1875rem;
  border-radius: var(--rv-radius-sm);
}

.rv-tabs__tab[aria-selected='true'] {
  color: var(--rv-color-ink);
}

/* The selected marker is drawn inside the control so selecting a tab never
   moves the line the bar sits on. */
.rv-tabs__tab[aria-selected='true']::after {
  position: absolute;
  right: 0;
  bottom: 0;
  left: 0;
  height: 0.125rem;
  background: var(--rv-color-accent);
  content: '';
}
</style>
