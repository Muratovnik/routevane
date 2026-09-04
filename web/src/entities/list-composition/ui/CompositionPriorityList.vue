<script setup lang="ts">
import { useSortable } from '@vueuse/integrations/useSortable'
import { ref, useId, useTemplateRef, watch } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import RvIcon from '@/shared/ui/RvIcon.vue'

export type PriorityItem = {
  id: string
  title: string
  note?: string
  overlaps?: string[] | null
}

const props = defineProps<{
  disabled?: boolean
  description?: string
  items: PriorityItem[]
  overlapPending?: boolean
  title?: string
}>()

const emit = defineEmits<{ reorder: [ids: string[]] }>()
const { t, tc } = useLocale()
const list = useTemplateRef<HTMLElement>('list')
const ordered = ref<string[]>([])
const baseId = useId()

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
  const [moved] = next.splice(from, 1)
  if (moved === undefined) return
  next.splice(to, 0, moved)
  ordered.value = next
}

function item(id: string): PriorityItem | undefined {
  return props.items.find((entry) => entry.id === id)
}

function overlapState(): 'hidden' | 'pending' | 'unknown' {
  if (props.items.length < 2) return 'hidden'
  if (props.overlapPending) return 'pending'
  return props.items.some((entry) => entry.overlaps === null)
    ? 'unknown'
    : 'hidden'
}
</script>

<template>
  <section :aria-labelledby="baseId + '-title'" class="priority-list">
    <header class="priority-list__heading">
      <span>
        <h3 :id="baseId + '-title'">
          {{ title ?? t('list.priority.title') }}
        </h3>
        <small>{{ tc('list.priority.count', items.length) }}</small>
      </span>
      <p :id="baseId + '-note'">
        {{ description ?? t('list.priority.body') }}
      </p>
      <p
        v-if="overlapState() !== 'hidden'"
        class="priority-list__overlap-state"
        role="status"
      >
        {{
          t(
            overlapState() === 'pending'
              ? 'servicePicker.overlap.pending'
              : 'servicePicker.overlap.unknown',
          )
        }}
      </p>
    </header>
    <p v-if="ordered.length === 0" class="priority-list__empty">
      {{ t('list.priority.empty') }}
    </p>
    <ol
      v-else
      ref="list"
      :aria-describedby="baseId + '-note'"
      :aria-labelledby="baseId + '-title'"
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
        <span class="priority-list__copy">
          <strong>{{ item(id)?.title ?? id }}</strong>
          <small v-if="item(id)?.note">{{ item(id)?.note }}</small>
          <span
            v-if="(item(id)?.overlaps?.length ?? 0) > 0"
            class="priority-list__tags"
          >
            <span
              v-for="other in item(id)?.overlaps ?? []"
              :key="other"
              class="priority-list__tag"
            >
              {{ t('servicePicker.overlap.tag', { list: other }) }}
            </span>
          </span>
        </span>
        <slot name="actions" :item="item(id)" />
      </li>
    </ol>
  </section>
</template>

<style scoped>
.priority-list {
  display: grid;
  align-content: start;
  width: 100%;
  min-width: 0;
  overflow: hidden;
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-lg);
}

.priority-list__heading {
  display: grid;
  gap: var(--rv-space-2);
  padding: var(--rv-space-4);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.priority-list__heading > span {
  display: flex;
  gap: var(--rv-space-3);
  align-items: baseline;
  justify-content: space-between;
}

.priority-list__heading h3 {
  font-size: var(--rv-text-interface);
}

.priority-list__heading small,
.priority-list__heading p,
.priority-list__empty {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-meta);
}

.priority-list__heading small {
  font-variant-numeric: tabular-nums;
}

.priority-list__overlap-state {
  color: var(--rv-color-ink-tertiary);
}

.priority-list__empty {
  padding: var(--rv-space-5) var(--rv-space-4);
}

.priority-list__items {
  display: grid;
  align-content: start;
  max-height: var(--rv-picker-height);
  margin: 0;
  padding: var(--rv-space-2);
  overflow-y: auto;
  overscroll-behavior: contain;
  list-style: none;
}

.priority-list__item {
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
  min-height: var(--rv-row-comfortable);
  padding: var(--rv-space-2);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.priority-list__item:last-child {
  border-bottom: 0;
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

.priority-list__copy {
  display: grid;
  flex: 1;
  gap: var(--rv-space-1);
  min-width: 0;
}

.priority-list__copy strong {
  overflow-wrap: anywhere;
}

.priority-list__copy small {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
  font-variant-numeric: tabular-nums;
}

.priority-list__tags {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-1);
}

.priority-list__tag {
  padding: var(--rv-space-1) var(--rv-space-2);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-meta);
  background: var(--rv-color-surface-selected);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-lg);
}
</style>
