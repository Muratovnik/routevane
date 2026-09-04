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
  services: { id: string; title: string }[]
  targetTitle: string
}>()
const emit = defineEmits<{ retry: [] }>()
const { t, formatNumber } = useLocale()
const open = ref(false)
const analysis = computed(() => props.forecast?.overlaps)
const title = (id: string) =>
  props.services.find((service) => service.id === id)?.title ?? id
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
      <template v-else>
        <p class="overlaps__note">{{ t('overlaps.meaning') }}</p>
        <div class="overlaps__guide">
          <strong>{{ t('overlaps.resolve.title') }}</strong>
          <p>{{ t('overlaps.resolve.body') }}</p>
          <small>{{ t('overlaps.resolve.scope') }}</small>
        </div>
        <ul :aria-label="t('overlaps.title')" class="overlaps__items">
          <li
            v-for="(item, index) in analysis.items"
            :key="index"
            class="overlaps__item"
          >
            <strong>{{ t(`overlaps.${item.kind}`) }}</strong>
            <div
              v-for="(entry, side) in item.covering === undefined
                ? [item.entry]
                : [item.entry, item.covering]"
              :key="side"
              class="overlaps__value"
            >
              <span v-if="side === 1" class="overlaps__note">{{
                t('overlaps.covering')
              }}</span>
              <span class="overlaps__note">{{
                t(`overlaps.rule.${entry.ruleKind}`)
              }}</span>
              <code class="overlaps__address">{{ entry.value }}</code>
              <span class="overlaps__owners">
                <a
                  v-for="id in entry.services"
                  :key="id"
                  :href="`/library#list=${encodeURIComponent(id)}`"
                  rel="noopener noreferrer"
                  target="_blank"
                  >{{ t('overlaps.openList', { list: title(id) }) }}</a
                >
              </span>
            </div>
          </li>
        </ul>
      </template>
      <p v-if="analysis.truncated" class="overlaps__note" role="status">
        {{
          t('overlaps.truncated', {
            count: formatNumber(analysis.items.length),
          })
        }}
      </p>
    </template>
  </RvDisclosure>
</template>

<style scoped>
.overlaps__items {
  display: grid;
  gap: var(--rv-space-4);
  max-block-size: var(--rv-analysis-height);
  margin: 0;
  padding: var(--rv-space-1);
  overflow: auto;
  list-style: none;
}

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

.overlaps__item {
  display: grid;
  gap: var(--rv-space-3);
  padding-bottom: var(--rv-space-4);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.overlaps__value {
  display: grid;
  gap: var(--rv-space-1);
  overflow-wrap: anywhere;
}

.overlaps__note {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-interface);
}

.overlaps__address {
  font-family: var(--rv-font-mono);
  font-size: var(--rv-text-interface);
}

.overlaps__owners {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2) var(--rv-space-4);
  font-size: var(--rv-text-interface);
}

.overlaps__owners a {
  color: var(--rv-color-accent-ink);
}
</style>
