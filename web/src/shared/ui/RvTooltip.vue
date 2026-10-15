<script setup lang="ts">
import { TooltipProvider } from 'reka-ui'
import { computed } from 'vue'

/**
 * A short name or fact shown beside the control it describes.
 *
 * Nuxt UI's `UTooltip` owns the opening on hover and keyboard focus, the
 * closing on Escape and on leaving, the positioning, the portal and the
 * description a reader's software hears on the control. Each tooltip keeps a
 * provider of its own, as it did before the library drew it: its quarter-second
 * delay is its own, so a neighbour that has just closed does not open it at
 * once. Everything visible is ours, named with the classes below.
 */
const props = withDefaults(
  defineProps<{
    text: string
    disabled?: boolean
    /** The side of the trigger the panel opens on. */
    side?: 'top' | 'right' | 'bottom' | 'left'
  }>(),
  { side: 'right' },
)

const parts = { content: 'rv-tooltip', text: 'rv-tooltip__text' }

// The panel opens on `side` of the trigger, to its right unless asked
// otherwise. Its hook is an attribute the library passes on to the panel
// element along with the positioning props.
const panel = computed(() => ({
  ariaLabel: props.text,
  collisionPadding: 8,
  'data-testid': 'rv-tooltip',
  side: props.side,
  sideOffset: 8,
}))
</script>

<template>
  <TooltipProvider :delay-duration="250">
    <!-- The library puts the tooltip role on an aria-hidden node inside the
         painted panel, where a reader's software finds it as the trigger's
         description. The painted panel itself therefore carries no role and no
         name of its own, and a test that reads what it draws needs a hook. -->
    <UTooltip
      :content="panel"
      :disabled="props.disabled"
      :text="props.text"
      :ui="parts"
    >
      <slot />
    </UTooltip>
  </TooltipProvider>
</template>

<!-- The panel is portalled out of this component, so its rules are global; the
     rv-tooltip names are this component's alone. They sit outside the library's
     cascade layer and replace its look: its fixed one-line height, its
     truncated text, its smaller type and its entrance. -->
<style>
.rv-tooltip {
  z-index: 40;
  display: block;
  height: auto;
  max-width: var(--rv-panel-width);
  padding: var(--rv-space-2) var(--rv-space-3);
  color: var(--rv-color-ink);
  font-size: var(--rv-text-dense);
  line-height: inherit;
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-sm);
  box-shadow: var(--rv-shadow-raised);
  overflow-wrap: anywhere;
  animation: none;
}

.rv-tooltip__text {
  overflow: visible;
  white-space: normal;
  text-overflow: clip;
}
</style>
