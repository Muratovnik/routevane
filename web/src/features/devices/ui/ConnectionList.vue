<script setup lang="ts">
import type { DeviceCard } from '@/shared/api/devices'
import { useLocale } from '@/shared/i18n/useLocale'
import { targetIcon } from '@/shared/lib/targetIcon'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'
import type { StatusTone } from '@/shared/ui/types'

/**
 * The connections this operator registered, as one surface rather than a stack
 * of cards. A row says what the connection is and what Routevane does with it,
 * and nothing about whether the device can be reached: this product delivers to
 * devices, it does not watch them.
 */
defineProps<{
  addDisabled: boolean
  busy: boolean
  connections: DeviceCard[]
  selectedId: string
  /** The target's name in the reader's language, which the catalog owns. */
  titleOf: (connection: DeviceCard) => string
}>()

const emit = defineEmits<{
  add: []
  select: [id: string]
}>()

const { t } = useLocale()

const deliveryLabel = (connection: DeviceCard): string => {
  if (!connection.deployable) return t('devices.delivery.manual')
  return t(
    connection.autoDeliver
      ? 'devices.auto.on.noCredential'
      : 'devices.auto.off',
  )
}

// Green is the enabled state, not decoration: a connection waiting for an
// operator's decision is a neutral mark.
const deliveryTone = (connection: DeviceCard): StatusTone =>
  connection.deployable && connection.autoDeliver ? 'ready' : 'waiting'
</script>

<template>
  <!-- The page is already announced as «Подключения»; this heading orders the
       column for a reader moving by headings without adding a second landmark
       of the same name. -->
  <section class="connections">
    <header class="connections__header">
      <h2 class="connections__title">{{ t('connections.title') }}</h2>
      <span class="connections__count">{{ connections.length }}</span>
      <!-- The collection's principal action stands the same height here as on
           the profiles shelf and in the library; the row wraps under the title
           where a language needs more width, rather than shrinking it. -->
      <RvButton
        :disabled="addDisabled"
        size="touch"
        variant="primary"
        @click="emit('add')"
      >
        <RvIcon name="plus" />{{ t('devices.add') }}
      </RvButton>
    </header>
    <ul class="connections__rows">
      <li v-for="connection in connections" :key="connection.id">
        <button
          type="button"
          class="connections__row"
          :class="{
            'connections__row--selected': connection.id === selectedId,
          }"
          :aria-describedby="`delivery-${connection.id}`"
          :aria-label="t('devices.configure.aria', { name: connection.name })"
          :aria-pressed="connection.id === selectedId"
          :disabled="busy"
          @click="emit('select', connection.id)"
        >
          <RvIcon :name="targetIcon(connection.targetID)" />
          <span class="connections__identity">
            <strong class="connections__name">{{ connection.name }}</strong>
            <span class="connections__meta">
              {{ titleOf(connection) }}
              <span aria-hidden="true"> · </span>
              <span class="connections__address">{{ connection.address }}</span>
            </span>
            <RvStatus
              :id="`delivery-${connection.id}`"
              :label="deliveryLabel(connection)"
              :tone="deliveryTone(connection)"
            />
          </span>
          <!-- The chosen row is marked by a shape as well as by its ground, and
               the mark holds its place in every row so choosing one never moves
               the text beside it. -->
          <RvIcon class="connections__mark" name="chevron" />
        </button>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.connections {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  min-width: 0;
  max-height: var(--rv-connections-list-height);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-lg);
}

.connections__header {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  align-items: center;
  padding: var(--rv-space-4) var(--rv-space-4) var(--rv-space-3);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.connections__title {
  font-size: var(--rv-text-section);
}

.connections__count {
  margin-inline-end: auto;
  color: var(--rv-color-ink-muted);
  font-variant-numeric: tabular-nums;
}

.connections__rows {
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  list-style: none;
}

.connections__rows > li + li {
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.connections__row {
  display: flex;
  gap: var(--rv-space-3);
  align-items: flex-start;
  width: 100%;
  padding: var(--rv-space-3) var(--rv-space-4);
  color: var(--rv-color-ink);
  font: inherit;
  text-align: start;
  background: transparent;
  border: var(--rv-border-hair) solid transparent;
  cursor: pointer;
}

.connections__row:hover {
  background: var(--rv-color-surface-hover);
}

.connections__row--selected {
  background: var(--rv-color-surface-selected);
  border-color: var(--rv-color-accent);
}

.connections__row:disabled {
  cursor: not-allowed;
}

.connections__identity {
  display: grid;
  gap: var(--rv-space-1);
  flex: 1;
  min-width: 0;
  justify-items: start;
}

.connections__name {
  font-size: var(--rv-text-interface);
  overflow-wrap: anywhere;
}

.connections__meta {
  min-width: 0;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}

.connections__address {
  font-family: var(--rv-font-mono);
}

.connections__mark {
  flex: none;
  align-self: center;
  color: var(--rv-color-accent-ink);
  opacity: 0;
  transform: rotate(-90deg);
}

.connections__row--selected .connections__mark {
  opacity: 1;
}
</style>
