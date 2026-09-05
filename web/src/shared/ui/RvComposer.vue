<script setup lang="ts">
import { computed, inject } from 'vue'
import { workspacePane } from '@/shared/ui/workspacePane'
defineProps<{ settingsLabel: string }>()
const pane = inject(workspacePane, null)
const compact = computed(() => pane?.docked.value && pane.open.value)
</script>
<template>
  <div class="rv-composer" :class="{ 'rv-composer--compact': compact }">
    <div class="rv-composer__main"><slot /></div>
    <aside :aria-label="settingsLabel" class="rv-composer__settings">
      <slot name="settings" :compact="compact" />
    </aside>
  </div>
</template>
<style scoped>
.rv-composer {
  display: grid;
  grid-template-columns: minmax(0, 1fr) var(--rv-composer-rail-width);
  grid-template-rows: minmax(0, 1fr);
  gap: var(--rv-space-4) var(--rv-space-6);
  height: 100%;
  min-height: 0;
  min-width: 0;
}

.rv-composer__main {
  view-transition-name: composer-content;
  min-height: 0;
  min-width: 0;
}

.rv-composer__settings {
  view-transition-name: composer-settings;
  align-self: start;
  padding: var(--rv-space-5);
  min-width: 0;
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-md);
}

.rv-composer--compact {
  grid-template-columns: minmax(0, 1fr);
  grid-template-rows: auto minmax(0, 1fr);
}

.rv-composer--compact .rv-composer__settings {
  view-transition-name: composer-settings;
  grid-row: 1;
}

@container (width <= 54rem) {
  .rv-composer:not(.rv-composer--compact) {
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: auto auto;
    height: auto;
  }
}
</style>
