<script setup lang="ts">
import { ref } from 'vue'

import RvButton from '@/shared/ui/RvButton.vue'

/**
 * Copying with its answer in the same place as the question.
 *
 * The confirmation sits beside the control that produced it, in a slot that is
 * already there before the click, so a copied secret never pushes the page
 * around. WCAG 2.2 asks that a secret be copyable rather than retyped; this is
 * that affordance.
 */
const props = defineProps<{
  copiedLabel: string
  failedLabel: string
  label: string
  value: string
  variant?: 'primary' | 'secondary' | 'quiet'
}>()

// The outcome stays until the next attempt: a confirmation that erases itself
// leaves the operator unsure whether the secret reached the clipboard or was
// never put there.
const outcome = ref<'idle' | 'copied' | 'failed'>('idle')

/**
 * The write is made against the platform clipboard directly. VueUse's
 * `useClipboard` was tried and rejected: when the asynchronous write is refused
 * it falls back to `document.execCommand('copy')`, ignores what that returns,
 * and resolves as a success anyway — so a denied clipboard would report
 * "copied". A confirmation has to be a fact that happened, which means the
 * refusal has to be the one the browser gave.
 */
const copy = async (): Promise<void> => {
  // A browser with no clipboard to write to says so, instead of showing a
  // confirmation for something that did not happen.
  const target: Clipboard | undefined =
    typeof navigator === 'undefined' ? undefined : navigator.clipboard
  if (target === undefined) {
    outcome.value = 'failed'
    return
  }
  try {
    await target.writeText(props.value)
    outcome.value = 'copied'
  } catch {
    outcome.value = 'failed'
  }
}
</script>

<template>
  <span class="rv-copy">
    <RvButton :variant="variant ?? 'secondary'" @click="copy">
      {{ label }}
    </RvButton>
    <span
      class="rv-copy__outcome"
      :class="{ 'rv-copy__outcome--failed': outcome === 'failed' }"
      role="status"
    >
      <template v-if="outcome === 'copied'">{{ copiedLabel }}</template>
      <template v-else-if="outcome === 'failed'">{{ failedLabel }}</template>
    </span>
  </span>
</template>

<style scoped>
.rv-copy {
  display: inline-flex;
  gap: var(--rv-space-2);
  align-items: center;
  min-width: 0;
}

.rv-copy__outcome {
  min-width: 0;
  color: var(--rv-color-status-ready);
  font-size: var(--rv-text-dense);
}

.rv-copy__outcome--failed {
  color: var(--rv-color-status-warning);
}
</style>
