<script setup lang="ts">
import { useSortable } from '@vueuse/integrations/useSortable'
import { ref, useTemplateRef, watch } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import RvIcon from '@/shared/ui/RvIcon.vue'

export type PriorityItem = {
  id: string
  title: string
  note?: string
}

const props = defineProps<{
  disabled?: boolean
  items: PriorityItem[]
}>()

const emit = defineEmits<{ reorder: [ids: string[]] }>()
const { t } = useLocale()
const list = useTemplateRef<HTMLElement>('list')
const ordered = ref<string[]>([])

function same(left: string[], right: string[]): boolean {
  return left.join('\0') === right.join('\0')
}

watch(
  () => props.items.map((item) => item.id),
  (ids) => {
    if (!same(ids, ordered.value)) ordered.value = [...ids]
  },
  { immediate: true },
)

watch(ordered, (ids) => {
  const current = props.items.map((item) => item.id)
  if (!same(ids, current)) emit('reorder', [...ids])
})

const sortable = useSortable(list, ordered, {
  animation: 200,
  disabled: props.disabled,
  fallbackOnBody: true,
  fallbackTolerance: 3,
  forceFallback: true,
  handle: '.priority-list__handle',
  watchElement: true,
})

watch(
  () => props.disabled,
  (disabled) => sortable.option('disabled', disabled),
)

function move(from: number, to: number): void {
  if (props.disabled || to < 0 || to >= ordered.value.length) return
  const next = [...ordered.value]
  const [item] = next.splice(from, 1)
  if (item === undefined) return
  next.splice(to, 0, item)
  ordered.value = next
}

function item(id: string): PriorityItem | undefined {
  return props.items.find((entry) => entry.id === id)
}
</script>

<template>
  <section aria-labelledby="list-priority-title" class="priority-list">
    <header class="priority-list__heading">
      <h3 id="list-priority-title">{{ t('list.priority.title') }}</h3>
      <p id="list-priority-note">{{ t('list.priority.body') }}</p>
    </header>
    <ol
      ref="list"
      aria-describedby="list-priority-note"
      aria-labelledby="list-priority-title"
      class="priority-list__items"
    >
      <li
        v-for="(id, index) in ordered"
        :key="id"
        :data-id="id"
        class="priority-list__item"
      >
        <button
          :aria-label="
            t('list.priority.move.aria', {
              list: item(id)?.title ?? id,
              position: index + 1,
            })
          "
          class="priority-list__handle"
          :disabled="disabled"
          type="button"
          @keydown.down.prevent="move(index, index + 1)"
          @keydown.up.prevent="move(index, index - 1)"
        >
          <RvIcon name="drag" />
        </button>
        <span class="priority-list__position" aria-hidden="true">{{
          index + 1
        }}</span>
        <span class="priority-list__copy">
          <strong>{{ item(id)?.title ?? id }}</strong>
          <small v-if="item(id)?.note">{{ item(id)?.note }}</small>
        </span>
        <slot name="actions" :item="item(id)" />
      </li>
    </ol>
  </section>
</template>

<style scoped>
.priority-list,
.priority-list__heading {
  display: grid;
  gap: var(--rv-space-1);
  width: 100%;
}

.priority-list__heading h3 {
  font-size: var(--rv-text-interface);
}

.priority-list__heading p {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.priority-list__items {
  display: grid;
  width: 100%;
  margin: var(--rv-space-3) 0 0;
  padding: 0;
  list-style: none;
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.priority-list__item {
  display: flex;
  gap: var(--rv-space-3);
  align-items: center;
  min-height: var(--rv-row-default);
  padding: var(--rv-space-2) 0;
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.priority-list__item.sortable-ghost {
  background: var(--rv-color-accent-quiet);
}

.priority-list__item.sortable-chosen {
  background: var(--rv-color-surface-muted);
}

.priority-list__handle {
  display: inline-flex;
  flex: none;
  align-items: center;
  justify-content: center;
  width: var(--rv-control-touch);
  height: var(--rv-control-touch);
  padding: 0;
  color: var(--rv-color-ink-tertiary);
  font: inherit;
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: grab;
  touch-action: none;
}

.priority-list__handle:active {
  cursor: grabbing;
}

.priority-list__handle:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-border-hair);
}

.priority-list__handle:disabled {
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.priority-list__handle:hover:not(:disabled) {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-hover);
}

.priority-list__position {
  min-width: var(--rv-space-5);
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
  text-align: end;
}

.priority-list__copy {
  display: flex;
  flex: 1;
  flex-wrap: wrap;
  gap: var(--rv-space-1) var(--rv-space-3);
  align-items: baseline;
  min-width: 0;
}

.priority-list__copy small {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
  font-variant-numeric: tabular-nums;
}
</style>
