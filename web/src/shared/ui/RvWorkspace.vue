<script setup lang="ts">
import { useResizeObserver } from '@vueuse/core'
import { computed, provide, ref, useId, useTemplateRef } from 'vue'

import { workspacePane } from '@/shared/ui/workspacePane'

defineProps<{ settingsLabel?: string }>()

const host = useTemplateRef<HTMLElement>('host')
const target = `workspace-pane-${useId().replaceAll(':', '-')}`
const open = ref(false)
const roomy = ref(false)
const docked = computed(() => roomy.value)
useResizeObserver(host, ([entry]) => {
  if (!entry) return
  const rem = Number.parseFloat(
    getComputedStyle(document.documentElement).fontSize,
  )
  roomy.value = entry.contentRect.width >= 80 * rem
})
provide(workspacePane, { target: `#${target}`, docked, open })
</script>

<template>
  <div ref="host" class="rv-workspace">
    <div
      class="rv-workspace__layout"
      :class="{ 'rv-workspace__layout--inspecting': docked && open }"
    >
      <div class="rv-workspace__main"><slot /></div>
      <aside
        v-if="$slots.settings"
        v-show="!docked || !open"
        :aria-label="settingsLabel"
        class="rv-workspace__settings"
      >
        <slot name="settings" />
      </aside>
      <div v-show="docked && open" :id="target" class="rv-workspace__detail" />
    </div>
  </div>
</template>

<style scoped>
.rv-workspace {
  min-width: 0;
  min-height: 0;
  height: 100%;
  container-type: inline-size;
}

.rv-workspace__layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) var(--rv-composer-rail-width);
  gap: var(--rv-space-6);
  align-items: start;
  max-width: var(--rv-composition-width);
  height: 100%;
  min-height: 0;
  margin-inline: 0;
}

.rv-workspace__main {
  height: 100%;
  min-width: 0;
  min-height: 0;
}

.rv-workspace__settings {
  display: grid;
  gap: var(--rv-space-6);
  min-width: 0;
  padding: var(--rv-space-6);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-md);
}

.rv-workspace__layout--inspecting {
  grid-template-columns: minmax(0, 1fr) minmax(
      var(--rv-detail-min-width),
      0.9fr
    );
  max-width: var(--rv-composition-inspecting-width);
}

.rv-workspace__detail {
  position: sticky;
  top: var(--rv-space-6);
  height: 100%;
  min-height: var(--rv-picker-mobile-height);
  min-width: 0;
  overflow: hidden;
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-md);
}

@container (width <= 54rem) {
  .rv-workspace__layout {
    grid-template-columns: minmax(0, 1fr);
    height: auto;
  }
}
</style>
