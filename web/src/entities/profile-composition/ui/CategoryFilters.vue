<script setup lang="ts">
import { useElementSize } from '@vueuse/core'
import { computed, useTemplateRef } from 'vue'
import type { CategoryDetail, ListDetail } from '../model/types'
import { useLocale } from '@/shared/i18n/useLocale'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvTooltip from '@/shared/ui/RvTooltip.vue'
import RvSearchSelect from '@/shared/ui/RvSearchSelect.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'
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
  /**
   * How many lists the draft holds, when the table is a draft's. The filter
   * that shows only those exists only then: a library holds no selection.
   */
  selectedCount?: number
  selectedOnly?: boolean
}>()
const emit = defineEmits<{
  'update:modelValue': [value: string[]]
  'update:query': [value: string]
  action: []
  'update:selectedOnly': [value: boolean]
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
// The row holds the quick chips beside the chips that always stand in it: all
// categories, the choice panel's trigger and the labelled action. Those are
// reserved first, so the row never wraps onto a second line the fill height
// of the table below does not allow for; each quick chip then takes its share.
const quick = computed(() =>
  choices.value.slice(
    0,
    Math.max(0, Math.min(5, Math.floor((width.value - 360) / 155))),
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
// A chosen category that is not among the quick chips is named by the trigger,
// which then reads as a pressed chip does.
const moreActive = computed(() =>
  choices.value.some(
    (option) =>
      props.modelValue.includes(option.value) &&
      !quick.value.some((quickOption) => quickOption.value === option.value),
  ),
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
      <!-- The trigger is the choice panel's own button. It takes the chips'
           ink, ground and edge at rest, and their pressed look while it names a
           chosen category. -->
      <RvSearchSelect
        chip
        :marked="moreActive"
        :model-value="modelValue"
        :trigger-label="triggerLabel"
        :options="choices"
        :disabled="disabled"
        :label="t('listPicker.collections')"
        :placeholder="t('listPicker.filter.more')"
        :search-label="t('category.search')"
        :empty-label="t('category.noMatches')"
        size="compact"
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
      <button
        v-if="selectedCount !== undefined"
        type="button"
        class="catalog-filters__chip"
        :aria-pressed="selectedOnly === true"
        :disabled="disabled"
        @click="emit('update:selectedOnly', selectedOnly !== true)"
      >
        {{
          t('listPicker.filter.selected', {
            count: formatNumber(selectedCount),
          })
        }}
      </button>
      <RvTooltip v-if="actionLabel" :text="actionLabel">
        <button
          class="catalog-filters__chip catalog-filters__action"
          type="button"
          :aria-label="actionLabel"
          :disabled="disabled || actionDisabled"
          @click="emit('action')"
        >
          <RvIcon name="plus" />{{ t('listPicker.filter.add') }}
        </button>
      </RvTooltip>
    </div>
    <RvTextInput
      v-model="search"
      :aria-label="t('create.search')"
      class="catalog-filters__search"
      :disabled="disabled"
      :placeholder="t('create.search')"
      type="search"
    />
  </div>
</template>

<style scoped>
.catalog-filters {
  display: grid;
  gap: var(--rv-space-3);
  min-width: 0;
}

.catalog-filters__search {
  width: min(100%, var(--rv-measure-field));
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

/* A chip that cannot be pressed says so, rather than accepting a press that
   provably changes nothing. It is drawn as every unavailable control is: flat,
   in the disabled ink, edged with the faint rule and at full opacity. A chip
   that is pressed keeps the accent at low emphasis, as an unavailable switch
   that is on does, so the filter in force still reads as the one in force. */
.catalog-filters__chip:disabled {
  color: var(--rv-color-ink-disabled);
  background: transparent;
  border-color: var(--rv-color-rule);
  cursor: not-allowed;
}

.catalog-filters__chip[aria-pressed='true']:disabled {
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

.catalog-filters__chip:disabled .catalog-filters__count {
  color: var(--rv-color-ink-disabled);
}

/* In the option list a category's name is followed straight by its count, so
   the separation a table row needs between adjacent category labels is not
   wanted here: it is the last thing still holding the number away. */
.rv-search-select__option .category-label {
  margin-inline-end: 0;
}
</style>
