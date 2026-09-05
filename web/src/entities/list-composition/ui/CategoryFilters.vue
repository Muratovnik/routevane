<script setup lang="ts">
import { useElementSize } from '@vueuse/core'
import { computed, useTemplateRef } from 'vue'
import type { CategoryDetail, ServiceDetail } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvSearchSelect from '@/shared/ui/RvSearchSelect.vue'
import CategoryLabel from './CategoryLabel.vue'

const props = defineProps<{
  modelValue: string
  query: string
  categories: CategoryDetail[]
  services: ServiceDetail[]
  disabled?: boolean
}>()
const emit = defineEmits<{
  'update:modelValue': [value: string]
  'update:query': [value: string]
}>()
const { t, tor, formatNumber } = useLocale()
const search = computed({
  get: () => props.query,
  set: (value: string) => emit('update:query', value),
})
const element = useTemplateRef<HTMLElement>('element')
const { width } = useElementSize(element)
const choices = computed(() => [
  ...props.categories.map((category) => ({
    value: category.id,
    label: tor(`category.${category.id}`, category.title),
    count: category.services.length,
  })),
  {
    value: 'rv:uncategorized',
    label: t('servicePicker.other'),
    count: props.services.filter(
      (service) =>
        !props.categories.some((category) =>
          category.services.includes(service.id),
        ),
    ).length,
  },
])
const quick = computed(() =>
  choices.value.slice(
    0,
    Math.max(0, Math.min(5, Math.floor((width.value - 260) / 155))),
  ),
)
const triggerLabel = computed(() =>
  props.modelValue === 'all' ||
  quick.value.some((option) => option.value === props.modelValue)
    ? t('servicePicker.filter.more')
    : undefined,
)
</script>

<template>
  <div ref="element" class="catalog-filters">
    <div
      class="catalog-filters__categories"
      :aria-label="t('servicePicker.filter.label')"
      role="group"
    >
      <button
        type="button"
        class="catalog-filters__chip"
        :aria-pressed="modelValue === 'all'"
        :disabled="disabled"
        @click="emit('update:modelValue', 'all')"
      >
        {{ t('servicePicker.filter.all') }}
      </button>
      <button
        v-for="option in quick"
        :key="option.value"
        type="button"
        class="catalog-filters__chip"
        :aria-pressed="modelValue === option.value"
        :disabled="disabled"
        @click="emit('update:modelValue', option.value)"
      >
        {{ option.label }}<small>{{ formatNumber(option.count) }}</small>
      </button>
      <RvSearchSelect
        :model-value="modelValue"
        :trigger-label="triggerLabel"
        :options="choices"
        :disabled="disabled"
        :label="t('servicePicker.collections')"
        :placeholder="t('servicePicker.filter.more')"
        :search-label="t('category.search')"
        :empty-label="t('category.noMatches')"
        @update:model-value="emit('update:modelValue', $event)"
      >
        <template #option="{ option }">
          <CategoryLabel :id="option.value" :label="option.label" /><small
            class="catalog-filters__count"
            >{{
              formatNumber(
                choices.find((choice) => choice.value === option.value)
                  ?.count ?? 0,
              )
            }}</small
          >
        </template>
      </RvSearchSelect>
    </div>
    <label class="catalog-filters__search">
      <RvIcon name="search" />
      <input
        v-model="search"
        :aria-label="t('create.search')"
        :placeholder="t('create.search')"
        :disabled="disabled"
        type="search"
      />
    </label>
  </div>
</template>

<style scoped>
.catalog-filters {
  display: grid;
  gap: var(--rv-space-3);
  min-width: 0;
}

.catalog-filters__search {
  display: flex;
  align-items: center;
  gap: var(--rv-space-2);
  width: min(100%, var(--rv-measure-field));
  min-height: var(--rv-control-default);
  padding: 0 var(--rv-space-3);
  color: var(--rv-color-ink-muted);
  background: var(--rv-color-field);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-md);
}

.catalog-filters__search:focus-within {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-border-hair);
}

.catalog-filters__search input {
  width: 100%;
  min-width: 0;
  padding: 0;
  border: 0;
  outline: 0;
  background: transparent;
}

.catalog-filters__categories {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  align-items: start;
  min-width: 0;
}

.catalog-filters__chip {
  display: inline-flex;
  align-items: center;
  gap: var(--rv-space-2);
  min-height: var(--rv-control-compact);
  padding: var(--rv-space-1) var(--rv-space-3);
  color: var(--rv-color-ink-muted);
  font: inherit;
  font-size: var(--rv-text-dense);
  max-width: 100%;
  white-space: normal;
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-md);
  cursor: pointer;
}

.catalog-filters__chip[aria-pressed='true'] {
  color: var(--rv-color-accent-ink);
  background: var(--rv-color-accent-quiet);
}

.catalog-filters__chip small {
  padding-inline: var(--rv-space-1);
  background: var(--rv-color-surface-muted);
  border-radius: var(--rv-radius-sm);
}

.catalog-filters__count {
  margin-inline-start: auto;
  color: var(--rv-color-ink-muted);
}
</style>
