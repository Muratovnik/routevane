<script setup lang="ts">
import type { StatusTone } from '@/shared/ui/kinds'

/**
 * One status mark for the whole surface.
 *
 * A status is a mark plus words, never a filled surface: the colour is the
 * fastest signal for whoever can see it, and the label is the whole signal for
 * whoever cannot.
 */
defineProps<{
  label: string
  tone?: StatusTone
}>()
</script>

<template>
  <span class="rv-status" :class="`rv-status--${tone ?? 'waiting'}`">
    <span class="rv-status__dot" aria-hidden="true" />
    <span class="rv-status__label">{{ label }}</span>
  </span>
</template>

<style scoped>
.rv-status {
  display: inline-flex;
  gap: var(--rv-space-2);
  align-items: baseline;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  white-space: nowrap;
}

.rv-status__dot {
  flex: none;
  align-self: center;
  width: 0.5rem;
  height: 0.5rem;
  background: currentcolor;
  border-radius: 50%;
}

.rv-status__label {
  color: var(--rv-color-ink);
  font-weight: 600;
  white-space: normal;
}

.rv-status--ready {
  color: var(--rv-color-status-ready);
}

.rv-status--warning {
  color: var(--rv-color-status-warning);
}

.rv-status--failed {
  color: var(--rv-color-status-failed);
}

.rv-status--busy {
  color: var(--rv-color-status-busy);
}

.rv-status--waiting {
  color: var(--rv-color-status-waiting);
}

.rv-status--busy .rv-status__dot {
  animation: rv-status-pulse 1.4s ease-in-out infinite;
}

@keyframes rv-status-pulse {
  50% {
    opacity: 0.35;
  }
}
</style>
