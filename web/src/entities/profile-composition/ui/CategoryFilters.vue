<script setup lang="ts">
import { useElementSize } from '@vueuse/core'
import { computed, useTemplateRef } from 'vue'
import type { CategoryDetail, ListDetail } from '../model/types'
import { useLocale } from '@/shared/i18n/useLocale'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvTooltip from '@/shared/ui/RvTooltip.vue'
import RvSearchSelect from '@/shared/ui/RvSearchSelect.vue'
import CategoryLabel from './CategoryLabel.vue'

const props = defineProps<{
  modelValue: string[]
  query: string
  categories: CategoryDetail[]
  lists: ListDetail[]
  disabled?: boolean
  actionLabel?: string
  /**
   * Whether the action beside the filters is unavailable while the filters
   * themselves still work. Reading a retained catalog is not writing to it, so
   * a library that cannot be written to keeps its filters and withdraws the
   * one control that would write.
   */
  actionDisabled?: boolean
}>()
const emit = defineEmits<{
  'update:modelValue': [value: string[]]
  'update:query': [value: string]
  action: []
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
    count: category.lists.length,
  })),
  {
    value: 'rv:uncategorized',
    label: t('listPicker.other'),
    count: props.lists.filter(
      (list) =>
        !props.categories.some((category) => category.lists.includes(list.id)),
    ).length,
  },
])
const quick = computed(() =>
  choices.value.slice(
    0,
    Math.max(0, Math.min(5, Math.floor((width.value - 260) / 155))),
  ),
)
const triggerLabel = computed(
  () =>
    choices.value
      .filter(
        (option) =>
          props.modelValue.includes(option.value) &&
          !quick.value.some(
            (quickOption) => quickOption.value === option.value,
          ),
      )
      .map((option) => option.label)
      .join(', ') || t('listPicker.filter.more'),
)
const toggle = (id: string): void => {
  emit(
    'update:modelValue',
    props.modelValue.includes(id)
      ? props.modelValue.filter((value) => value !== id)
      : [...props.modelValue, id],
  )
}
</script>

<template>
  <div ref="element" class="catalog-filters">
    <div
      class="catalog-filters__categories"
      :aria-label="t('listPicker.filter.label')"
      role="group"
    >
      <button
        type="button"
        class="catalog-filters__chip"
        :aria-pressed="modelValue.length === 0"
        :disabled="disabled"
        @click="emit('update:modelValue', [])"
      >
        {{ t('listPicker.filter.all') }}
      </button>
      <button
        v-for="option in quick"
        :key="option.value"
        type="button"
        class="catalog-filters__chip"
        :aria-pressed="modelValue.includes(option.value)"
        :disabled="disabled"
        @click="toggle(option.value)"
      >
        {{ option.label
        }}<span class="catalog-filters__count">{{
          formatNumber(option.count)
        }}</span>
      </button>
      <RvSearchSelect
        :model-value="modelValue"
        :trigger-label="triggerLabel"
        :options="choices"
        :disabled="disabled"
        :label="t('listPicker.collections')"
        :placeholder="t('listPicker.filter.more')"
        :search-label="t('category.search')"
        :empty-label="t('category.noMatches')"
        @update:model-value="emit('update:modelValue', $event)"
      >
        <template #option="{ option }">
          <CategoryLabel :id="option.value" :label="option.label" /><span
            class="catalog-filters__count"
            >{{
              formatNumber(
                choices.find((choice) => choice.value === option.value)
                  ?.count ?? 0,
              )
            }}</span
          >
        </template>
      </RvSearchSelect>
      <RvTooltip v-if="actionLabel" :text="actionLabel">
        <button
          class="catalog-filters__chip catalog-filters__action"
          type="button"
          :aria-label="actionLabel"
          :disabled="disabled || actionDisabled"
          @click="emit('action')"
        >
          <RvIcon name="plus" />
        </button>
      </RvTooltip>
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

/* A chip that cannot be pressed says so, rather than accepting a press that
   provably changes nothing. */
.catalog-filters__chip:disabled {
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.catalog-filters__chip[aria-pressed='true'] {
  color: var(--rv-color-accent-ink);
  background: var(--rv-color-accent-quiet);
}

/* How many lists a category holds belongs to that category's name, so it reads
   as one label at the size the name is set in — not as a footnote pushed to the
   far edge of the panel, where the eye has to travel to pair the two. The chip
   and the option state the same fact, so they state it the same way. */
.catalog-filters__count {
  flex: none;
  padding-inline: var(--rv-space-1);
  color: var(--rv-color-ink-muted);
  font-variant-numeric: tabular-nums;
  background: var(--rv-color-surface-muted);
  border-radius: var(--rv-radius-sm);
}

/* In the option list a category's name is followed straight by its count, so
   the separation a table row needs between adjacent category labels is not
   wanted here: it is the last thing still holding the number away. */
.rv-search-select__option .category-label {
  margin-inline-end: 0;
}

.catalog-filters__action {
  min-width: var(--rv-control-compact);
  justify-content: center;
}
</style>
