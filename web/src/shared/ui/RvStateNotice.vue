<script setup lang="ts">
import { computed } from 'vue'

import type { IconName, StatusTone } from '@/shared/ui/types'
import RvIcon from '@/shared/ui/RvIcon.vue'

/**
 * Every screen state that is not plain content says three things: what is here
 * or missing, why, and what to do next. One component so a loading state, a
 * refusal and a warning are recognisably the same kind of message, and so no
 * screen invents a fourth shape for the same job.
 */
const props = defineProps<{
  body?: string
  live?: boolean
  title: string
  tone?: StatusTone
}>()

const icons: Record<StatusTone, IconName> = {
  busy: 'refresh',
  failed: 'warning',
  ready: 'check',
  waiting: 'info',
  warning: 'warning',
}

const icon = computed(() => icons[props.tone ?? 'waiting'])
</script>

<template>
  <div
    class="rv-notice"
    :class="[
      `rv-notice--${tone ?? 'waiting'}`,
      { 'rv-loading-feedback': tone === 'busy' },
    ]"
    :role="live === true ? 'status' : undefined"
  >
    <span class="rv-notice__icon">
      <RvIcon :name="icon" />
    </span>
    <div class="rv-notice__content">
      <p class="rv-notice__title">{{ title }}</p>
      <p v-if="body !== undefined && body !== ''" class="rv-notice__body">
        {{ body }}
      </p>
      <div v-if="$slots.action" class="rv-notice__action">
        <slot name="action" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.rv-notice {
  display: flex;
  gap: var(--rv-space-4);
  align-items: flex-start;
  padding: var(--rv-space-4) var(--rv-space-5);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-md);
}

.rv-notice__icon {
  display: inline-flex;
  margin-top: 0.125rem;
  color: var(--rv-color-status-waiting);
}

.rv-notice__content {
  display: grid;
  gap: var(--rv-space-2);
  min-width: 0;
}

.rv-notice__title {
  font-weight: 600;
  font-size: var(--rv-text-interface);
}

.rv-notice__body {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-interface);
}

.rv-notice__action {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  margin-top: var(--rv-space-2);
}

.rv-notice--ready .rv-notice__icon {
  color: var(--rv-color-status-ready);
}

.rv-notice--warning .rv-notice__icon {
  color: var(--rv-color-status-warning);
}

.rv-notice--failed .rv-notice__icon {
  color: var(--rv-color-status-failed);
}

.rv-notice--failed .rv-notice__title {
  color: var(--rv-color-status-failed);
}

.rv-notice--busy .rv-notice__icon {
  color: var(--rv-color-status-busy);
}
</style>
