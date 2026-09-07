<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  block?: boolean
  disabled?: boolean
  href?: string
  loading?: boolean
  loadingLabel?: string
  size?: 'compact' | 'default'
  to?: string
  type?: 'button' | 'submit'
  variant?: 'primary' | 'secondary' | 'quiet'
}>()

const unavailable = computed(
  () => props.disabled === true || props.loading === true,
)
</script>

<template>
  <!-- Nuxt UI owns element selection, disabled-link navigation guards, async
       loading semantics and keyboard behavior. Feature code keeps this stable
       facade and the Routevane visual language. -->
  <UButton
    class="rv-button"
    :class="[
      `rv-button--${variant ?? 'secondary'}`,
      `rv-button--${size ?? 'default'}`,
      { 'rv-button--block': block === true },
    ]"
    color="neutral"
    variant="link"
    :block="block"
    :disabled="unavailable"
    :external="href !== undefined"
    :loading="loading"
    :href="href"
    :to="href === undefined ? to : undefined"
    :type="type ?? 'button'"
    :ui="{
      base: 'disabled:opacity-100 aria-disabled:opacity-100',
    }"
    :aria-busy="loading === true ? 'true' : undefined"
    :aria-label="loading === true ? loadingLabel : undefined"
  >
    <template #leading>
      <!-- Decoration beside the accessible name, so it is aria-hidden and
           carries a test hook rather than a role a reader would hear. -->
      <span
        v-if="loading === true"
        aria-hidden="true"
        class="rv-button__spinner"
        data-testid="rv-button-spinner"
      />
    </template>
    <slot />
  </UButton>
</template>

<!-- UButton forwards the facade class through ULink to its final native root;
     that deep boundary cannot retain Vue's component scope marker. These
     globally unique rv-button classes therefore own the facade styling. -->
<style>
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
  line-height: inherit;
  text-align: center;
  text-decoration: none;
  background: transparent;
  border: var(--rv-border-hair) solid transparent;
  border-radius: var(--rv-radius-md);
  cursor: pointer;
  transition:
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

.rv-button.rv-button--primary {
  color: var(--rv-color-ink-inverted);
  background: var(--rv-color-accent);
}

.rv-button.rv-button--primary:hover:not(:disabled, [aria-disabled='true']) {
  background: var(--rv-color-accent-ink);
}

.rv-button.rv-button--secondary {
  background: var(--rv-color-surface-muted);
  border-color: var(--rv-color-rule-strong);
}

.rv-button.rv-button--secondary:hover:not(:disabled, [aria-disabled='true']) {
  background: var(--rv-color-surface-hover);
}

.rv-button.rv-button--quiet {
  color: var(--rv-color-ink-muted);
}

.rv-button.rv-button--quiet:hover:not(:disabled, [aria-disabled='true']) {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-muted);
}

.rv-button:disabled,
.rv-button[aria-disabled='true'] {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-muted);
  border-color: var(--rv-color-rule-strong);
  opacity: 1;
  cursor: not-allowed;
}

.rv-button:active:not(:disabled, [aria-disabled='true']) {
  transform: translateY(var(--rv-border-hair));
}

@keyframes rv-button-spin {
  to {
    transform: rotate(1turn);
  }
}
</style>
