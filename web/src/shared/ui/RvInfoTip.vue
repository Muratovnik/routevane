<script setup lang="ts">
import {
  PopoverContent,
  PopoverPortal,
  PopoverRoot,
  PopoverTrigger,
} from 'reka-ui'

import RvIcon from '@/shared/ui/RvIcon.vue'

/**
 * The informer: a small "i" whose panel explains one nearby thing on demand.
 * It exists for background a reader may want, never for facts a decision
 * depends on — those stay inline where they are read without a click.
 *
 * The panel is a viewport overlay portalled to the document body, so a long
 * explanation scrolls inside its own box instead of running off the screen or
 * stretching whatever the trigger sits in.
 */
defineProps<{
  /** The accessible name of the trigger: what this explains. */
  numeric?: boolean
  label: string
  text: string
  items?: string[]
}>()
</script>

<template>
  <PopoverRoot>
    <PopoverTrigger
      :aria-label="label"
      class="rv-infotip__trigger"
      :class="{ 'rv-infotip__trigger--numeric': numeric }"
    >
      <slot><RvIcon name="info" /></slot>
    </PopoverTrigger>
    <PopoverPortal>
      <PopoverContent
        align="start"
        class="rv-infotip__panel"
        :collision-padding="8"
        :side-offset="6"
      >
        <template v-if="items"
          ><strong>{{ text }}</strong>
          <ul class="rv-infotip__items">
            <li v-for="item in items" :key="item">{{ item }}</li>
          </ul></template
        >
        <template v-else>{{ text }}</template>
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot>
</template>

<style scoped>
.rv-infotip__trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: var(--rv-control-compact);
  height: var(--rv-control-compact);
  padding: 0;
  color: var(--rv-color-ink-tertiary);
  font: inherit;
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.rv-infotip__trigger--numeric {
  justify-content: flex-start;
  padding-inline: var(--rv-space-2);
  margin-inline-start: calc(-1 * var(--rv-space-2));
  font-variant-numeric: tabular-nums;
  color: var(--rv-color-ink-muted);
}

.rv-infotip__trigger:hover {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-hover);
}
</style>

<!-- The panel is styled outside the scoped block above, and it has to be.
     `PopperContent` renders its own positioning wrapper as this component's root
     element, so the scope attribute lands on that wrapper while this class lands
     on the element inside it: a scoped rule would compile to
     `.rv-infotip__panel[data-v-…]` and match nothing, leaving the explanation as
     unpainted text over the page. The `rv-infotip__` names are this component's
     alone, so an unscoped block claims nothing else. -->
<style>
.rv-infotip__panel {
  z-index: 8;
  display: block;
  width: max-content;
  max-width: min(20rem, 80vw);

  /* The positioner states how much room is left on the side it chose;
     a panel taller than that would bleed past the viewport edge. */
  /* stylelint-disable custom-property-pattern -- Reka names the room it measured. */
  max-height: min(
    var(--rv-overlay-height),
    var(--reka-popover-content-available-height, var(--rv-overlay-height))
  );
  /* stylelint-enable custom-property-pattern */
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: var(--rv-space-3) var(--rv-space-4);
  color: var(--rv-color-ink);
  font-weight: 400;
  font-size: var(--rv-text-dense);
  line-height: var(--rv-leading-normal);
  text-transform: none;
  letter-spacing: normal;
  background: var(--rv-color-canvas);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-md);
  box-shadow: var(--rv-shadow-raised);
}

.rv-infotip__items {
  display: grid;
  gap: var(--rv-space-2);
  margin: var(--rv-space-3) 0 0;
  padding: 0;
  list-style: none;
}
</style>
