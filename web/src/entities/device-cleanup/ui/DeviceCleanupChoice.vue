<script setup lang="ts">
import { computed } from 'vue'

import type {
  DeviceObjects,
  DeviceObjectsSurface,
} from '@/entities/device-cleanup/model/types'
import { useLocale } from '@/shared/i18n/useLocale'
import RvStatus from '@/shared/ui/RvStatus.vue'
import RvSwitch from '@/shared/ui/RvSwitch.vue'

/**
 * Whether retiring a format or a profile also removes from the routers what it
 * delivered there (ADR 0060). It says what would go, router by router, and why
 * it cannot when one of them cannot be cleaned; a format that left nothing on
 * any router offers nothing.
 */
const props = defineProps<{
  disabled?: boolean
  objects: DeviceObjects | null
  state: 'loading' | 'ready' | 'failed'
}>()

const remove = defineModel<boolean>({ required: true })

const { t, tc } = useLocale()

const surfaces = computed(() => props.objects?.surfaces ?? [])

const where = (surface: DeviceObjectsSurface): string =>
  surface.connectionName === ''
    ? surface.endpoint
    : t('cleanup.surface', {
        name: surface.connectionName,
        endpoint: surface.endpoint,
      })

const what = (surface: DeviceObjectsSurface): string => {
  if (surface.blockedBy !== '') return t(`cleanup.blocked.${surface.blockedBy}`)
  const parts: string[] = []
  if (surface.fqdnGroups > 0)
    parts.push(tc('cleanup.groups', surface.fqdnGroups))
  if (surface.routes > 0) parts.push(tc('cleanup.routes', surface.routes))
  return parts.length === 0 ? t('cleanup.nothing') : parts.join(', ')
}
</script>

<template>
  <RvStatus
    v-if="state === 'loading'"
    :label="t('cleanup.loading')"
    tone="busy"
  />
  <RvStatus
    v-else-if="state === 'failed'"
    :label="t('cleanup.failed')"
    tone="warning"
  />
  <div v-else-if="surfaces.length > 0" class="device-cleanup">
    <RvSwitch
      v-model="remove"
      :description="
        objects?.cleanable === true
          ? t('cleanup.note')
          : t('cleanup.unavailable')
      "
      :disabled="disabled || objects?.cleanable !== true"
      :label="t('cleanup.switch')"
    />
    <ul class="device-cleanup__surfaces">
      <li
        v-for="surface in surfaces"
        :key="`${surface.outputID}:${surface.endpoint}:${surface.interfaceName}`"
        :class="{ 'device-cleanup__surface--blocked': !surface.cleanable }"
      >
        <span class="device-cleanup__where">{{ where(surface) }}</span>
        <span class="device-cleanup__what">{{ what(surface) }}</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.device-cleanup {
  display: grid;
  gap: var(--rv-space-3);
}

.device-cleanup__surfaces {
  display: grid;
  gap: var(--rv-space-1);
  margin: 0;
  padding: 0;
  font-size: var(--rv-text-dense);
  list-style: none;
}

.device-cleanup__surfaces li {
  display: flex;
  flex-wrap: wrap;
  gap: 0 var(--rv-space-2);
}

.device-cleanup__where {
  font-weight: 600;
  overflow-wrap: anywhere;
}

.device-cleanup__what {
  color: var(--rv-color-ink-muted);
}

.device-cleanup__surface--blocked .device-cleanup__what {
  color: var(--rv-color-status-warning);
}
</style>
