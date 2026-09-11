<script setup lang="ts">
import { useResizeObserver } from '@vueuse/core'
import { computed, provide, ref, useId, useTemplateRef } from 'vue'

import { workspacePane } from '@/shared/model/useWorkspacePane'

const host = useTemplateRef<HTMLElement>('host')
const target = `workspace-pane-${useId().replaceAll(':', '-')}`
const open = ref(false)
const locked = ref(false)
const roomy = ref(false)
const docked = computed(() => roomy.value)
useResizeObserver(host, ([entry]) => {
  if (!entry) return
  const rem = Number.parseFloat(
    getComputedStyle(document.documentElement).fontSize,
  )
  roomy.value = entry.contentRect.width >= 80 * rem
})
provide(workspacePane, { target: `#${target}`, docked, open, locked })
</script>

<template>
  <div
    ref="host"
    class="rv-workspace"
    :class="{
      'rv-workspace--inspecting': docked && open,
    }"
  >
    <!-- The pane a docked sheet locks. It has no role and no name of its own,
         and its `inert` state is what a test reads, so it carries a hook. -->
    <div
      class="rv-workspace__main"
      data-testid="rv-workspace-main"
      :inert="docked && locked"
    >
      <slot />
    </div>
    <div :id="target" class="rv-workspace__detail" :hidden="!docked || !open" />
  </div>
</template>

<style scoped>
.rv-workspace {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  align-items: stretch;
  width: 100%;
  max-width: var(--rv-composition-width);
  margin-inline: auto;
  min-width: 0;
  min-height: 0;
  height: 100%;
}

.rv-workspace__main {
  min-width: 0;
  min-height: 0;
}

.rv-workspace--inspecting {
  grid-template-columns: minmax(0, 1fr) minmax(
      var(--rv-detail-min-width),
      0.9fr
    );
  gap: var(--rv-space-6);
}

/* The docked sheet is pinned at the page inset, which is where it rests when
   the page fits the window, so a page that scrolls under a notice carries the
   sheet along without taking that inset away from it. Its height leaves the
   same inset below, and it ends where the table beside it ends. */
.rv-workspace__detail {
  position: sticky;
  top: var(--rv-space-6);
  min-width: 0;
  min-height: var(--rv-picker-mobile-height);
  height: calc(100dvh - var(--rv-space-6) * 2);
  overflow: hidden;
}
</style>
