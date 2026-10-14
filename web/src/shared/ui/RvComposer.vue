<script setup lang="ts">
import { computed, inject } from 'vue'
import { workspacePane } from '@/shared/model/useWorkspacePane'
defineProps<{ settingsLabel: string }>()
const pane = inject(workspacePane, null)
const compact = computed(() => pane?.docked.value && pane.open.value)
</script>
<template>
  <div class="rv-composer" :class="{ 'rv-composer--compact': compact }">
    <div class="rv-composer__main"><slot /></div>
    <aside :aria-label="settingsLabel" class="rv-composer__settings">
      <slot name="settings" />
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

/* Beside the table the rail follows the window instead of waiting below
   whatever stands above the composer: it is pinned at the page inset and never
   taller than the window less that inset twice. The form inside gives up its
   fields to a scroll of their own before the rail outgrows that, and keeps its
   actions in view. */
.rv-composer__settings {
  view-transition-name: composer-settings;
  align-self: start;
  position: sticky;
  top: var(--rv-space-6);
  display: flex;
  flex-direction: column;
  max-height: calc(100dvh - var(--rv-space-6) * 2);
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
  position: static;
  max-height: none;
}

/* Without room for the rail beside the table, the settings stand above the
   table as a band, where the actions that end the draft are in reach, instead
   of after rows that can run to fifty. */
@container (width <= 54rem) {
  .rv-composer:not(.rv-composer--compact) {
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: auto auto;
    height: auto;
  }

  .rv-composer:not(.rv-composer--compact) .rv-composer__settings {
    grid-row: 1;
    position: static;
    max-height: none;
  }

  .rv-composer:not(.rv-composer--compact) .rv-composer__main {
    grid-row: 2;
  }
}
</style>
