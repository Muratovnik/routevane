<script setup lang="ts">
import {
  TooltipContent,
  TooltipPortal,
  TooltipProvider,
  TooltipRoot,
  TooltipTrigger,
} from 'reka-ui'
defineProps<{ text: string; disabled?: boolean }>()
</script>
<template>
  <TooltipProvider :delay-duration="250">
    <TooltipRoot :disabled="disabled">
      <TooltipTrigger as-child><slot /></TooltipTrigger>
      <TooltipPortal>
        <TooltipContent
          class="rv-tooltip"
          :aria-label="text"
          side="right"
          :side-offset="8"
          :collision-padding="8"
          >{{ text }}</TooltipContent
        >
      </TooltipPortal>
    </TooltipRoot>
  </TooltipProvider>
</template>
<style>
.rv-tooltip {
  z-index: 40;
  max-width: var(--rv-panel-width);
  padding: var(--rv-space-2) var(--rv-space-3);
  color: var(--rv-color-ink);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-sm);
  box-shadow: var(--rv-shadow-raised);
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}
</style>
