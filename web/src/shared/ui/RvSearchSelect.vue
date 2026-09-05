<script setup lang="ts">
import {
  ListboxContent,
  ListboxFilter,
  ListboxGroup,
  ListboxGroupLabel,
  ListboxItem,
  ListboxRoot,
  PopoverContent,
  PopoverPortal,
  PopoverRoot,
  PopoverTrigger,
} from 'reka-ui'
import { computed, ref } from 'vue'
import type { ChoiceGroup, ChoiceOption } from '@/shared/ui/kinds'
import { useLocale } from '@/shared/i18n/useLocale'
import RvIcon from '@/shared/ui/RvIcon.vue'
const props = defineProps<{
  modelValue: string
  options?: ChoiceOption[]
  groups?: ChoiceGroup[]
  label?: string
  toggleLabel?: string
  inputId?: string
  describedBy?: string
  labelledBy?: string
  invalid?: boolean
  loading?: boolean
  loadingLabel?: string
  size?: 'default' | 'compact'
  placeholder: string
  triggerLabel?: string
  searchLabel?: string
  emptyLabel?: string
  disabled?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useLocale()
const open = ref(false)
const query = ref('')
const runs = computed(
  () => props.groups ?? [{ key: '', label: '', options: props.options ?? [] }],
)
const choices = computed(() => runs.value.flatMap((group) => group.options))
const selected = computed(() =>
  choices.value.find((option) => option.value === props.modelValue),
)
const heading = computed(
  () => props.label ?? props.toggleLabel ?? props.placeholder,
)
const matches = computed(() =>
  runs.value
    .map((group) => ({
      ...group,
      options: group.options.filter((option) =>
        [option.label, option.value, option.mono ?? '', group.label]
          .join(' ')
          .toLocaleLowerCase()
          .includes(query.value.trim().toLocaleLowerCase()),
      ),
    }))
    .filter((group) => group.options.length > 0),
)
function choose(value: unknown): void {
  if (
    typeof value !== 'string' ||
    props.disabled ||
    props.loading ||
    !choices.value.some((choice) => choice.value === value && !choice.disabled)
  )
    return
  emit('update:modelValue', value)
  open.value = false
}
</script>
<template>
  <PopoverRoot v-model:open="open" @update:open="query = ''">
    <PopoverTrigger as-child>
      <button
        :id="inputId"
        type="button"
        class="rv-search-select__trigger"
        :class="{ 'rv-search-select__trigger--field': size === 'default' }"
        :disabled="disabled || loading || choices.length === 0"
        :aria-label="
          inputId
            ? undefined
            : `${heading}: ${triggerLabel ?? selected?.label ?? placeholder}`
        "
        :aria-describedby="describedBy"
        :aria-labelledby="labelledBy"
        :aria-invalid="invalid || undefined"
        :aria-busy="loading || undefined"
      >
        <span>{{
          loading
            ? (loadingLabel ?? placeholder)
            : (triggerLabel ?? selected?.label ?? placeholder)
        }}</span
        ><RvIcon name="chevron" />
      </button>
    </PopoverTrigger>
    <PopoverPortal>
      <PopoverContent
        class="rv-search-select__panel"
        :class="{ 'rv-search-select__panel--field': size === 'default' }"
        align="start"
        :side-offset="4"
        :collision-padding="8"
        :aria-label="heading"
      >
        <p class="rv-search-select__title">{{ heading }}</p>
        <ListboxRoot :model-value="modelValue" @update:model-value="choose">
          <div class="rv-search-select__search">
            <RvIcon name="search" />
            <ListboxFilter
              v-model="query"
              :aria-label="searchLabel ?? t('choice.search')"
              :placeholder="searchLabel ?? t('choice.search')"
              auto-focus
            />
          </div>
          <ListboxContent
            class="rv-search-select__options"
            :aria-label="heading"
          >
            <ListboxGroup
              v-for="group in matches"
              :key="group.key"
              class="rv-search-select__group"
            >
              <ListboxGroupLabel
                v-if="group.label"
                class="rv-search-select__group-label"
                >{{ group.label }}</ListboxGroupLabel
              >
              <ListboxItem
                v-for="option in group.options"
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
                <slot name="option" :option="option">
                  <span class="rv-search-select__copy"
                    ><span
                      >{{ option.label }}
                      <span v-if="option.mono" class="rv-search-select__mono">{{
                        option.mono
                      }}</span></span
                    >
                    <small v-if="option.note" class="rv-search-select__note">{{
                      option.note
                    }}</small>
                    <small
                      v-if="option.warning"
                      class="rv-search-select__warning"
                      >{{ option.warning }}</small
                    >
                  </span>
                </slot>
              </ListboxItem>
            </ListboxGroup>
          </ListboxContent>
          <p
            v-if="matches.length === 0"
            class="rv-search-select__empty"
            role="status"
          >
            {{ emptyLabel ?? t('choice.empty') }}
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
  /* stylelint-disable-next-line custom-property-pattern -- Reka measures the trigger. */
  min-width: var(--reka-popover-trigger-width);
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

.rv-search-select__trigger--field {
  background: var(--rv-color-field);
  width: 100%;
  min-height: var(--rv-control-touch);
}

.rv-search-select__trigger[aria-invalid='true'] {
  border-color: var(--rv-color-status-failed);
}

.rv-search-select__group-label {
  padding: var(--rv-space-2);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-meta);
}

.rv-search-select__copy {
  display: grid;
  gap: var(--rv-space-1);
  min-width: 0;
  overflow-wrap: anywhere;
}

.rv-search-select__mono {
  font-family: var(--rv-font-mono);
}

.rv-search-select__note {
  color: var(--rv-color-ink-muted);
}

.rv-search-select__warning {
  color: var(--rv-color-status-warning);
}

.rv-search-select__option[data-disabled] {
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}
</style>

<style>
.rv-search-select__panel--field {
  /* stylelint-disable-next-line custom-property-pattern -- Reka measures the field. */
  width: var(--reka-popover-trigger-width, var(--rv-panel-width));
}
</style>
