<script setup lang="ts">
import { computed, resolveComponent, type Component } from 'vue'

/**
 * The only button in the product.
 *
 * One primitive rather than a set of look-alike classes per feature: a download
 * link and a submit control differ in element, not in appearance, and that
 * distinction stays honest here instead of in each screen's stylesheet.
 */
const props = defineProps<{
  block?: boolean
  disabled?: boolean
  href?: string
  loading?: boolean
  loadingLabel?: string
  /**
   * `default` is the height every field on the surface has, so a button beside
   * an input lines up with it. `compact` is the denser row control used inside
   * tables and card rows.
   */
  size?: 'compact' | 'default'
  to?: string
  type?: 'button' | 'submit'
  variant?: 'primary' | 'secondary' | 'quiet'
}>()

const element = computed<string | Component>(() => {
  if (props.href !== undefined) return 'a'
  if (props.to !== undefined) return resolveComponent('NuxtLink')
  return 'button'
})

const attributes = computed<Record<string, unknown>>(() => {
  if (props.href !== undefined)
    return {
      'aria-disabled': props.disabled === true || props.loading === true,
      href: props.href,
    }
  if (props.to !== undefined)
    return {
      'aria-disabled': props.disabled === true || props.loading === true,
      to: props.to,
    }
  return {
    disabled: props.disabled === true || props.loading === true,
    type: props.type ?? 'button',
  }
})

function guardDisabledLink(event: MouseEvent): void {
  if (
    (props.href !== undefined || props.to !== undefined) &&
    (props.disabled === true || props.loading === true)
  ) {
    event.preventDefault()
    event.stopImmediatePropagation()
  }
}
</script>

<template>
  <component
    :is="element"
    class="rv-button"
    :class="[
      `rv-button--${variant ?? 'secondary'}`,
      `rv-button--${size ?? 'default'}`,
      { 'rv-button--block': block === true },
    ]"
    :aria-busy="loading === true ? 'true' : undefined"
    :aria-label="loading === true ? loadingLabel : undefined"
    v-bind="attributes"
    @click.capture="guardDisabledLink"
  >
    <span
      v-if="loading === true"
      aria-hidden="true"
      class="rv-button__spinner"
    />
    <slot />
  </component>
</template>

<style scoped>
/* One control height across the surface: a button, a text field, a select
   trigger and a search box are the same height, because a row that mixes them
   is the common case and a row of mismatched boxes is what it looks like. */
.rv-button {
  display: inline-flex;
  gap: var(--rv-space-2);
  align-items: center;
  justify-content: center;
  min-height: var(--rv-control-touch);
  padding: 0 var(--rv-space-4);
  color: var(--rv-color-ink);
  font-weight: 600;
  font-size: var(--rv-text-interface);
  font-family: inherit;
  text-align: center;
  text-decoration: none;
  background: transparent;
  border: var(--rv-border-hair) solid transparent;
  border-radius: var(--rv-radius-md);
  cursor: pointer;
  transition:
    color var(--rv-motion-fast) var(--rv-motion-ease-out),
    background var(--rv-motion-fast) var(--rv-motion-ease-out),
    border-color var(--rv-motion-fast) var(--rv-motion-ease-out),
    transform var(--rv-motion-fast) var(--rv-motion-ease-out);
}

.rv-button__spinner {
  width: var(--rv-control-choice);
  height: var(--rv-control-choice);
  border: var(--rv-border-hair) solid currentcolor;
  border-right-color: transparent;
  border-radius: 50%;
  animation: rv-button-spin var(--rv-motion-working) linear infinite;
}

.rv-button--compact {
  min-height: var(--rv-control-compact);
  padding: 0 var(--rv-space-3);
  font-size: var(--rv-text-dense);
}

.rv-button--block {
  width: 100%;
}

.rv-button--primary {
  color: var(--rv-color-ink-inverted);
  background: var(--rv-color-accent);
}

.rv-button--primary:hover:not(:disabled) {
  background: var(--rv-color-accent-ink);
}

.rv-button--secondary {
  background: var(--rv-color-surface-muted);
  border-color: var(--rv-color-rule-strong);
}

.rv-button--secondary:hover:not(:disabled) {
  background: var(--rv-color-surface-hover);
}

.rv-button--quiet {
  color: var(--rv-color-ink-muted);
}

.rv-button--quiet:hover:not(:disabled) {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-muted);
}

.rv-button:disabled,
.rv-button[aria-disabled='true'] {
  opacity: 0.5;
  cursor: not-allowed;
}

.rv-button:active:not(:disabled) {
  transform: translateY(var(--rv-border-hair));
}

@keyframes rv-button-spin {
  to {
    transform: rotate(1turn);
  }
}
</style>
