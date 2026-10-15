<script setup lang="ts">
import type { DeviceCard } from '@/shared/api/devices'
import { useLocale } from '@/shared/i18n/useLocale'
import { targetIcon } from '@/shared/lib/targetIcon'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvSection from '@/shared/ui/RvSection.vue'
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
  return t(connection.autoDeliver ? 'devices.auto.on' : 'devices.auto.off')
}

// Green is the enabled state, not decoration: a connection waiting for an
// operator's decision is a neutral mark.
const deliveryTone = (connection: DeviceCard): StatusTone =>
  connection.deployable && connection.autoDeliver ? 'ready' : 'waiting'
</script>

<template>
  <!-- The page's own heading says «Connections»; this one names the column, so a
       reader moving by headings hears two different things. -->
  <RvSection class="connections" flush :title="t('devices.list.title')">
    <template #meta>
      <span class="connections__count">{{ connections.length }}</span>
    </template>
    <!-- The collection's principal action stands the same height here as on
         the profiles shelf and in the library; the row wraps under the title
         where a language needs more width, rather than shrinking it. -->
    <template #actions>
      <RvButton
        :disabled="addDisabled"
        size="touch"
        variant="primary"
        @click="emit('add')"
      >
        <RvIcon name="plus" />{{ t('devices.add') }}
      </RvButton>
    </template>
    <ul class="connections__rows">
      <li v-for="connection in connections" :key="connection.id">
        <!-- The row is named by what it shows — name, format, address and
             delivery — so what is read is what is seen; the chosen one is the
             current item of the list. -->
        <button
          type="button"
          class="connections__row"
          :class="{
            'connections__row--selected': connection.id === selectedId,
          }"
          :aria-current="connection.id === selectedId ? 'true' : undefined"
          :disabled="busy"
          @click="emit('select', connection.id)"
        >
          <span class="connections__identity">
            <!-- The mark stands in the name's own line, so it keeps that line's
                 centre however tall the row grows beneath it. -->
            <strong class="connections__name">
              <RvIcon :name="targetIcon(connection.targetID)" />
              <span class="connections__name-text">{{ connection.name }}</span>
            </strong>
            <span class="connections__meta">
              {{ titleOf(connection) }}
              <span aria-hidden="true"> · </span>
              <span class="connections__address">{{ connection.address }}</span>
            </span>
            <RvStatus
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
  </RvSection>
</template>

<style scoped>
/* A ceiling, not a standing height: past it the rows scroll inside the
   section's frame. */
.connections {
  max-height: var(--rv-connections-list-height);
}

.connections__count {
  color: var(--rv-color-ink-muted);
  font-variant-numeric: tabular-nums;
}

.connections__rows {
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
  padding: var(--rv-space-3) var(--rv-space-6);
  color: var(--rv-color-ink);
  font: inherit;
  text-align: start;
  background: transparent;
  border: 0;
  cursor: pointer;
}

.connections__row:hover {
  background: var(--rv-color-surface-hover);
}

/* The chosen row takes the ground every other selection here takes. It carries
   no frame of its own: the rows already divide by a hairline, and the mark at
   its end and the current state say what the ground does not. */
.connections__row--selected {
  background: var(--rv-color-surface-muted);
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
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
  min-width: 0;
  font-size: var(--rv-text-interface);
}

.connections__name-text {
  min-width: 0;
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
