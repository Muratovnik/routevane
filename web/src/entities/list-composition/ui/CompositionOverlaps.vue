<script setup lang="ts">
import { computed, ref } from 'vue'

import type { TargetForecast } from '@/shared/api/lists'
import { useLocale } from '@/shared/i18n/useLocale'
import RvButton from '@/shared/ui/RvButton.vue'
import RvDisclosure from '@/shared/ui/RvDisclosure.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

const props = defineProps<{
  forecast: TargetForecast | null
  pending: boolean
  targetTitle: string
}>()
const emit = defineEmits<{ retry: [] }>()
const { t, formatNumber } = useLocale()
const open = ref(false)
const analysis = computed(() => props.forecast?.overlaps)
</script>

<template>
  <RvDisclosure
    :open="open"
    :summary="t('overlaps.title')"
    @toggle="open = $event"
  >
    <RvStateNotice
      v-if="pending"
      live
      :title="t(analysis === undefined ? 'overlaps.loading' : 'overlaps.stale')"
      tone="busy"
    />
    <RvStateNotice
      v-else-if="analysis === undefined"
      :body="t('overlaps.unavailable.body')"
      live
      :title="t('overlaps.unavailable')"
      tone="waiting"
    >
      <template #action
        ><RvButton size="compact" @click="emit('retry')">{{
          t('action.retry')
        }}</RvButton></template
      >
    </RvStateNotice>
    <template v-if="analysis !== undefined && forecast !== null">
      <p class="overlaps__note">
        {{
          t('overlaps.projection', {
            target: targetTitle,
            count: formatNumber(forecast.projectedRules),
          })
        }}
      </p>
      <p v-if="analysis.items.length === 0" class="overlaps__note">
        {{ t('overlaps.empty') }}
      </p>
      <p v-else class="overlaps__note">{{ t('overlaps.found') }}</p>
    </template>
    <p class="overlaps__note">{{ t('overlaps.meaning') }}</p>
    <div class="overlaps__guide">
      <strong>{{ t('overlaps.resolve.title') }}</strong>
      <p>{{ t('overlaps.resolve.body') }}</p>
      <small>{{ t('overlaps.resolve.scope') }}</small>
    </div>
  </RvDisclosure>
</template>

<style scoped>
.overlaps__guide {
  display: grid;
  gap: var(--rv-space-2);
  padding: var(--rv-space-4);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-interface);
  background: var(--rv-color-surface-muted);
  border-left: var(--rv-border-mark) solid var(--rv-color-accent);
  border-radius: var(--rv-radius-sm);
}

.overlaps__guide strong {
  color: var(--rv-color-ink);
}

.overlaps__guide p {
  max-width: var(--rv-measure-prose);
}

.overlaps__guide small {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}

.overlaps__note {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-interface);
}
</style>
