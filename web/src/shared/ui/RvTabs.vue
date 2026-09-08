<script setup lang="ts">
import { TabsContent, TabsList, TabsRoot, TabsTrigger } from 'reka-ui'

import type { TabItem } from '@/shared/ui/types'

/**
 * One object's facets. Reka owns the tab/panel IDs, ARIA relationships,
 * keyboard traversal and focus entry. Named slots keep each facet's existing
 * root mounted while it is hidden, preserving drafts and deferred state.
 */
const props = defineProps<{
  label: string
  modelValue: string
  tabs: TabItem[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const update = (value: string | number): void => {
  if (typeof value === 'string' && value !== props.modelValue)
    emit('update:modelValue', value)
}
</script>

<template>
  <TabsRoot
    class="rv-tabs-root"
    :model-value="modelValue"
    :unmount-on-hide="false"
    @update:model-value="update"
  >
    <TabsList :aria-label="label" class="rv-tabs">
      <TabsTrigger
        v-for="tab in tabs"
        :key="tab.id"
        :value="tab.id"
        class="rv-tabs__tab"
      >
        {{ tab.label }}
      </TabsTrigger>
    </TabsList>
    <TabsContent v-for="tab in tabs" :key="tab.id" as-child :value="tab.id">
      <slot :name="tab.id" />
    </TabsContent>
  </TabsRoot>
</template>

<style scoped>
.rv-tabs-root {
  display: contents;
}

.rv-tabs {
  display: flex;
  flex-shrink: 0;
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
  outline-offset: calc(-1 * var(--rv-border-mark));
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
  height: var(--rv-tab-indicator-size);
  background: var(--rv-color-accent);
  content: '';
}
</style>
