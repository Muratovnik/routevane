<script setup lang="ts">
import { useSortable } from '@vueuse/integrations/useSortable'
import { ref, useId, useTemplateRef, watch } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'

export type PriorityItem = {
  id: string
  title: string
  note?: string
  overlaps?: string[] | null
}

const props = defineProps<{
  countLabel?: string
  numbered?: boolean
  compactOverlaps?: boolean
  disabled?: boolean
  description?: string
  items: PriorityItem[]
  overlapPending?: boolean
  overlapUnavailable?: boolean
  overlapUnavailableLabel?: string
  retryable?: boolean
  title?: string
}>()

const emit = defineEmits<{ reorder: [ids: string[]]; retry: [] }>()
const { t, tc } = useLocale()
const list = useTemplateRef<HTMLElement>('list')
const ordered = ref<string[]>([])
const baseId = useId()

const same = (left: string[], right: string[]): boolean =>
  left.join('\0') === right.join('\0')

watch(
  () => props.items.map((item) => item.id),
  (ids) => {
    if (!same(ids, ordered.value)) ordered.value = [...ids]
  },
  { immediate: true },
)

watch(ordered, (ids) => {
  const current = props.items.map((item) => item.id)
  if (props.disabled) {
    if (!same(ids, current)) ordered.value = [...current]
    return
  }
  if (!same(ids, current)) emit('reorder', [...ids])
})

const sortable = useSortable(list, ordered, {
  animation: 200,
  disabled: props.disabled,
  fallbackOnBody: true,
  fallbackTolerance: 3,
  forceFallback: true,
  handle: '.priority-profile__handle:not(:disabled)',
  filter: '.priority-profile__handle:disabled',
  preventOnFilter: false,
  watchElement: true,
})

watch(
  () => props.disabled,
  (disabled) => sortable.option('disabled', Boolean(disabled)),
  { flush: 'sync' },
)

const move = (from: number, to: number): void => {
  if (props.disabled || to < 0 || to >= ordered.value.length) return
  const next = [...ordered.value]
  const [moved] = next.splice(from, 1)
  if (moved === undefined) return
  next.splice(to, 0, moved)
  ordered.value = next
}

const item = (id: string): PriorityItem | undefined =>
  props.items.find((entry) => entry.id === id)

const overlapState = (): 'hidden' | 'pending' | 'unavailable' | 'unknown' => {
  if (props.items.length < 2) return 'hidden'
  if (props.overlapUnavailable) return 'unavailable'
  if (props.overlapPending) return 'pending'
  return props.items.some((entry) => entry.overlaps === null)
    ? 'unknown'
    : 'hidden'
}

const overlapMessage = (): string => {
  const state = overlapState()
  if (state === 'pending') return t('listPicker.overlap.pending')
  if (state === 'unavailable')
    return props.overlapUnavailableLabel ?? t('listPicker.overlap.unavailable')
  return t('listPicker.overlap.unknown')
}
</script>

<template>
  <section :aria-labelledby="baseId + '-title'" class="priority-list">
    <header class="priority-profile__heading">
      <span>
        <h3 :id="baseId + '-title'">
          {{ title ?? t('profile.priority.title') }}
        </h3>
        <small role="status">{{
          countLabel ?? tc('profile.priority.count', items.length)
        }}</small>
      </span>
      <p :id="baseId + '-note'">
        {{ description ?? t('profile.priority.body') }}
      </p>
      <div
        v-if="overlapState() !== 'hidden'"
        class="priority-profile__overlap-state"
      >
        <p role="status">
          {{ overlapMessage() }}
        </p>
        <RvButton
          v-if="overlapState() === 'unknown' && retryable"
          :disabled="disabled"
          size="compact"
          variant="quiet"
          @click="emit('retry')"
        >
          {{ t('action.retry') }}
        </RvButton>
      </div>
    </header>
    <p v-if="ordered.length === 0" class="priority-profile__empty">
      {{ t('profile.priority.empty') }}
    </p>
    <ol
      v-else
      ref="list"
      :aria-describedby="baseId + '-note'"
      :aria-labelledby="baseId + '-title'"
      class="priority-profile__items"
    >
      <li
        v-for="(id, index) in ordered"
        :key="id"
        :data-id="id"
        class="priority-profile__item"
      >
        <span
          v-if="numbered"
          class="priority-profile__position"
          aria-hidden="true"
          >{{ index + 1 }}</span
        >
        <button
          :aria-label="
            t('profile.priority.move.aria', {
              list: item(id)?.title ?? id,
              position: index + 1,
            })
          "
          class="priority-profile__handle"
          :disabled="disabled"
          type="button"
          @keydown.down.prevent="move(index, index + 1)"
          @keydown.up.prevent="move(index, index - 1)"
        >
          <RvIcon name="drag" />
        </button>
        <span class="priority-profile__copy">
          <strong>{{ item(id)?.title ?? id }}</strong>
          <small v-if="item(id)?.note">{{ item(id)?.note }}</small>
          <span
            v-if="!compactOverlaps && (item(id)?.overlaps?.length ?? 0) > 0"
            class="priority-profile__tags"
          >
            <span
              v-for="other in item(id)?.overlaps ?? []"
              :key="other"
              class="priority-profile__tag"
            >
              {{ t('listPicker.overlap.tag', { list: other }) }}
            </span>
          </span>
        </span>
        <slot name="actions" :item="item(id)" />
      </li>
    </ol>
    <div v-if="$slots.footer" class="priority-profile__footer">
      <slot name="footer" />
    </div>
  </section>
</template>

<style scoped>
.priority-list {
  display: grid;
  align-content: start;
  width: 100%;
  min-width: 0;
  overflow: visible;
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-lg);
}

.priority-profile__heading {
  display: grid;
  gap: var(--rv-space-2);
  padding: var(--rv-space-5);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.priority-profile__heading > span {
  display: flex;
  gap: var(--rv-space-3);
  align-items: baseline;
  justify-content: space-between;
}

.priority-profile__heading h3 {
  font-size: var(--rv-text-interface);
}

.priority-profile__heading small,
.priority-profile__heading p,
.priority-profile__empty {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-meta);
}

.priority-profile__heading small {
  font-variant-numeric: tabular-nums;
}

.priority-profile__overlap-state {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  align-items: center;
  justify-content: space-between;
  color: var(--rv-color-ink-tertiary);
}

.priority-profile__empty {
  padding: var(--rv-space-5) var(--rv-space-4);
}

.priority-profile__items {
  display: grid;
  align-content: start;
  max-height: var(--rv-priority-height, var(--rv-picker-height));
  margin: 0;
  padding: var(--rv-space-3);
  gap: var(--rv-space-2);
  overflow-y: auto;
  overscroll-behavior: contain;
  list-style: none;
}

.priority-profile__item {
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
  min-height: var(--rv-row-comfortable);
  padding: var(--rv-space-2);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-md);
}

.priority-profile__item.sortable-ghost {
  background: var(--rv-color-accent-quiet);
}

.priority-profile__item.sortable-chosen {
  background: var(--rv-color-surface-muted);
}

.priority-profile__handle {
  order: 1;
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

.priority-profile__handle:active {
  cursor: grabbing;
}

.priority-profile__handle:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-border-hair);
}

.priority-profile__handle:disabled {
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.priority-profile__handle:hover:not(:disabled) {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-hover);
}

.priority-profile__copy {
  display: grid;
  flex: 1;
  gap: var(--rv-space-1);
  min-width: 0;
}

.priority-profile__copy strong {
  font-weight: 400;
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}

.priority-profile__copy small {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
  font-variant-numeric: tabular-nums;
}

.priority-profile__tags {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-1);
}

.priority-profile__tag {
  padding: var(--rv-space-1) var(--rv-space-2);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-meta);
  background: var(--rv-color-surface-selected);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-lg);
}

.priority-profile__position {
  display: grid;
  flex: none;
  place-items: center;
  width: var(--rv-space-8);
  height: var(--rv-space-8);
  color: var(--rv-color-accent-ink);
  background: var(--rv-color-accent-quiet);
  border-radius: var(--rv-radius-sm);
  font-size: var(--rv-text-dense);
  font-variant-numeric: tabular-nums;
}

.priority-profile__footer {
  display: grid;
  gap: var(--rv-space-5);
  padding: var(--rv-space-5);
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}
</style>
