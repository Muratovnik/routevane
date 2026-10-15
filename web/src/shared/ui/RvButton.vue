<script setup lang="ts">
import { computed } from 'vue'

type Props = {
  block?: boolean
  disabled?: boolean
  href?: string
  loading?: boolean
  loadingLabel?: string
  /**
   * `default` is the standard control height a field shares with its row;
   * `touch` is the larger target a page header's principal action stands at.
   */
  size?: 'compact' | 'default' | 'touch'
  to?: string
  type?: 'button' | 'submit'
  /**
   * `danger` is the final act of something that cannot be taken back: a
   * deletion, a revoked link, a forgotten connection. A reversible act — an
   * archive, a restore — keeps `primary`.
   */
  variant?: 'primary' | 'secondary' | 'quiet' | 'danger'
}

/**
 * A button is named by its words. The icon-only form of a compact row action —
 * a square at the compact height with the glyph from the slot and no text — has
 * no words, so it cannot be drawn without `label`, which is its accessible name
 * (and its only name: the glyph is decoration).
 */
const props = defineProps<
  | (Props & { iconOnly?: false; label?: undefined })
  | (Props & { iconOnly: true; label: string })
>()

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
      `rv-button--${iconOnly === true ? 'compact' : (size ?? 'default')}`,
      {
        'rv-button--block': block === true,
        'rv-button--icon-only': iconOnly === true,
      },
    ]"
    :color="variant === 'danger' ? 'error' : 'neutral'"
    variant="link"
    :block="block"
    :disabled="unavailable"
    :external="href !== undefined"
    :loading="loading"
    :href="href"
    :to="href === undefined ? to : undefined"
    :type="type ?? 'button'"
    :aria-busy="loading === true ? 'true' : undefined"
    :aria-label="
      loading === true ? (loadingLabel ?? label) : (label ?? undefined)
    "
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
  min-height: var(--rv-control-default);
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

.rv-button.rv-button--icon-only {
  width: var(--rv-control-compact);
  padding: 0;
}

.rv-button--touch {
  min-height: var(--rv-control-touch);
}

.rv-button--block {
  width: 100%;
}

.rv-button.rv-button--primary {
  color: var(--rv-color-ink-inverted);
  background: var(--rv-color-accent);
}

/* An unavailable primary is still the primary act: it keeps the accent at low
   emphasis rather than becoming the secondary button standing beside it. */
.rv-button.rv-button--primary:disabled,
.rv-button.rv-button--primary[aria-disabled='true'] {
  color: var(--rv-color-ink-muted);
  background: var(--rv-color-accent-quiet);
  border-color: transparent;
}

.rv-button.rv-button--primary:hover:not(:disabled, [aria-disabled='true']) {
  background: var(--rv-color-accent-hover);
}

.rv-button.rv-button--danger {
  color: var(--rv-color-ink-inverted);
  background: var(--rv-color-danger);
}

/* Unavailable, the act stays the destructive one at low emphasis, as the
   primary does, so a confirmation that is working does not turn neutral. */
.rv-button.rv-button--danger:disabled,
.rv-button.rv-button--danger[aria-disabled='true'] {
  color: var(--rv-color-ink-muted);
  background: var(--rv-color-danger-quiet);
  border-color: transparent;
}

.rv-button.rv-button--danger:hover:not(:disabled, [aria-disabled='true']) {
  background: var(--rv-color-danger-hover);
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

/* A quiet button has no edge to give up, so unavailable it keeps none. */
.rv-button.rv-button--quiet:disabled,
.rv-button.rv-button--quiet[aria-disabled='true'] {
  border-color: transparent;
}

.rv-button.rv-button--quiet:hover:not(:disabled, [aria-disabled='true']) {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-muted);
}

/* An unavailable button gives up its ground and its strong edge: it is flat,
   dimmer than any available one and drawn with the faint rule, so it cannot be
   taken for the enabled button it was a moment ago. The library lowers an
   unavailable button's opacity with a utility in its cascade layer; this rule
   sits outside that layer, so its full opacity is the one that holds, for a
   disabled button and an unavailable link alike. */
.rv-button:disabled,
.rv-button[aria-disabled='true'] {
  color: var(--rv-color-ink-disabled);
  background: transparent;
  border-color: var(--rv-color-rule);
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
