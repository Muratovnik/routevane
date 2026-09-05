<script setup lang="ts">
import type { Fact } from '@/shared/ui/kinds'

/**
 * A fact list: the aligned label/value ledger this product uses everywhere it
 * states what something is. Values that are compared character by character —
 * identifiers, addresses, formats — ask for the mono role.
 */
defineProps<{
  items: Fact[]
}>()
</script>

<template>
  <dl class="rv-facts">
    <div v-for="item in items" :key="item.key" class="rv-facts__row">
      <dt class="rv-facts__label">{{ item.label }}</dt>
      <dd
        class="rv-facts__value"
        :class="{ 'rv-facts__value--mono': item.mono }"
      >
        {{ item.value }}
      </dd>
    </div>
  </dl>
</template>

<style scoped>
.rv-facts {
  container: facts / inline-size;
  display: grid;
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.rv-facts__row {
  display: grid;
  grid-template-columns: minmax(7rem, 0.7fr) minmax(0, 1.3fr);
  gap: var(--rv-space-4);
  align-items: baseline;
  padding: var(--rv-space-4) 0;
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.rv-facts__label {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.rv-facts__value {
  min-width: 0;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.rv-facts__value--mono {
  font-weight: 400;
  font-size: var(--rv-text-dense);
  font-family: var(--rv-font-mono);
}

@container facts (width <= 34rem) {
  .rv-facts__row {
    grid-template-columns: 1fr;
    gap: var(--rv-space-1);
  }
}
</style>
