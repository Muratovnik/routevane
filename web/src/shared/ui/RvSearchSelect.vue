<script setup lang="ts">
import {
  ListboxContent,
  ListboxFilter,
  ListboxItem,
  ListboxRoot,
  PopoverContent,
  PopoverPortal,
  PopoverRoot,
  PopoverTrigger,
} from 'reka-ui'
import { computed, ref } from 'vue'
import type { ChoiceOption } from '@/shared/ui/kinds'
import RvIcon from '@/shared/ui/RvIcon.vue'

const props = defineProps<{
  modelValue: string
  options: ChoiceOption[]
  label: string
  placeholder: string
  triggerLabel?: string
  searchLabel: string
  emptyLabel: string
  disabled?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const open = ref(false)
const query = ref('')
const selected = computed(() =>
  props.options.find((option) => option.value === props.modelValue),
)
const matches = computed(() =>
  props.options.filter((option) =>
    `${option.label} ${option.value}`
      .toLocaleLowerCase()
      .includes(query.value.trim().toLocaleLowerCase()),
  ),
)
function choose(value: unknown): void {
  if (typeof value !== 'string') return
  emit('update:modelValue', value)
  open.value = false
}
</script>

<template>
  <PopoverRoot v-model:open="open" @update:open="query = ''">
    <PopoverTrigger
      class="rv-search-select__trigger"
      :disabled="disabled"
      :aria-label="`${label}: ${triggerLabel ?? selected?.label ?? placeholder}`"
    >
      <span>{{ triggerLabel ?? selected?.label ?? placeholder }}</span
      ><RvIcon name="chevron" />
    </PopoverTrigger>
    <PopoverPortal>
      <PopoverContent
        class="rv-search-select__panel"
        align="end"
        :side-offset="4"
        :collision-padding="8"
        :aria-label="label"
      >
        <p class="rv-search-select__title">{{ label }}</p>
        <ListboxRoot :model-value="modelValue" @update:model-value="choose">
          <div class="rv-search-select__search">
            <RvIcon name="search" />
            <ListboxFilter
              v-model="query"
              :aria-label="searchLabel"
              :placeholder="searchLabel"
              auto-focus
            />
          </div>
          <ListboxContent class="rv-search-select__options" :aria-label="label">
            <ListboxItem
              v-for="option in matches"
              :key="option.value"
              :value="option.value"
              :disabled="option.disabled"
              class="rv-search-select__option"
            >
              <RvIcon
                name="check"
                class="rv-search-select__check"
                :class="{
                  'rv-search-select__check--selected':
                    option.value === modelValue,
                }"
              />
              <slot name="option" :option="option">{{ option.label }}</slot>
            </ListboxItem>
          </ListboxContent>
          <p
            v-if="matches.length === 0"
            class="rv-search-select__empty"
            role="status"
          >
            {{ emptyLabel }}
          </p>
        </ListboxRoot>
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot>
</template>

<!-- The portalled Reka content does not retain the caller scope attribute. -->
<style>
.rv-search-select__trigger {
  display: inline-flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--rv-space-2);
  min-height: var(--rv-control-compact);
  min-width: 0;
  max-width: 100%;
  padding: var(--rv-space-1) var(--rv-space-3);
  font: inherit;
  font-size: var(--rv-text-dense);
  color: var(--rv-color-ink-muted);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-md);
  cursor: pointer;
}

.rv-search-select__trigger > span {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.rv-search-select__trigger:disabled {
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.rv-search-select__panel {
  z-index: 30;
  width: var(--rv-panel-width);
  max-width: calc(100vw - var(--rv-space-4));
  /* stylelint-disable-next-line custom-property-pattern -- Reka owns the measured room for this overlay. */
  max-height: var(--reka-popover-content-available-height);
  overflow: auto;
  color: var(--rv-color-ink);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-md);
  box-shadow: var(--rv-shadow-raised);
}

.rv-search-select__title {
  padding: var(--rv-space-3);
  font-weight: 600;
  font-size: var(--rv-text-dense);
}

.rv-search-select__search {
  display: flex;
  align-items: center;
  gap: var(--rv-space-2);
  margin: 0 var(--rv-space-2) var(--rv-space-2);
  padding: var(--rv-space-2);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-sm);
}

.rv-search-select__search:focus-within {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-border-hair);
}

.rv-search-select__search input {
  width: 100%;
  min-width: 0;
  padding: 0;
  background: transparent;
  border: 0;
  outline: 0;
}

.rv-search-select__options {
  position: relative;
  max-height: var(--rv-overlay-height);
  padding: var(--rv-space-1);
  overflow-y: auto;
}

.rv-search-select__option {
  display: flex;
  align-items: center;
  gap: var(--rv-space-2);
  min-height: var(--rv-control-compact);
  padding: var(--rv-space-2);
  font-size: var(--rv-text-dense);
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.rv-search-select__option[data-highlighted] {
  background: var(--rv-color-surface-hover);
  outline: none;
}

.rv-search-select__option[data-state='checked'] {
  color: var(--rv-color-accent-ink);
}

.rv-search-select__check {
  visibility: hidden;
  flex: none;
}

.rv-search-select__check--selected {
  visibility: visible;
}

.rv-search-select__empty {
  padding: var(--rv-space-3);
  color: var(--rv-color-ink-muted);
}
</style>
