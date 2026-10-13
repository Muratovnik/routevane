<script setup lang="ts">
import { computed, useId } from 'vue'

import { keepFocus } from '@/features/devices/lib/keepFocus'
import type { DevicesModel } from '@/features/devices/model/useDevices'
import type { DeviceCard } from '@/shared/api/devices'
import { useLocale } from '@/shared/i18n/useLocale'
import RvSwitch from '@/shared/ui/RvSwitch.vue'

/**
 * Whether Routevane may deliver to this connection without being asked each
 * time. It is its own act and its own section: saving a connection's
 * parameters never grants it, and saving its password does not either. One
 * switch states what the server holds, and it changes only when the server says
 * it did — a click is a request, never the new state.
 *
 * It carries no password. A format that needs one has it saved in the section
 * above first, and until then the switch is unavailable and says why beside it.
 */
const props = defineProps<{
  connection: DeviceCard
  /** The deployment catalog has a current answer for this connection's target. */
  deployerKnown: boolean
  devices: DevicesModel
  needsCredential: boolean
}>()

const emit = defineEmits<{
  /** What the connection's notice area should say after this act. */
  notice: [message: string]
}>()

const { t } = useLocale()
const headingID = useId()

const busy = computed(
  () => props.devices.busy.value || props.devices.state.value === 'stale',
)
// A password this format needs and nothing holds is the one precondition the
// switch can be missing; where the system has no store to hold one, it cannot
// be met at all.
const missingPassword = computed(
  () => props.needsCredential && !props.connection.credentialStored,
)
// Turning delivery on needs this build to apply a file to this kind of device
// and to know what its deployer needs; turning it off needs neither, so a
// connection already on can always be switched off.
const canTurnOn = computed(
  () =>
    props.connection.deployable &&
    props.deployerKnown &&
    !missingPassword.value &&
    props.devices.requirementsState.value === 'ready',
)
const reason = computed(() => {
  if (props.connection.autoDeliver || !missingPassword.value) return undefined
  return t(
    props.devices.secretStoreAvailable.value
      ? 'devices.auto.needsCredential'
      : 'devices.auto.noStore',
  )
})

const onToggle = async (next: boolean): Promise<void> => {
  if (busy.value) return
  emit('notice', '')
  const done = await keepFocus(
    () =>
      next
        ? props.devices.enable(props.connection.id)
        : props.devices.disable(props.connection.id),
    () => null,
  )
  if (done && next) emit('notice', t('devices.auto.enabled.next'))
}
</script>

<template>
  <section :aria-labelledby="headingID" class="delivery">
    <h3 :id="headingID" class="delivery__title">
      {{ t('devices.auto.title') }}
    </h3>

    <!-- A device this build cannot apply a file to is a download, not a broken
         switch. -->
    <p v-if="!connection.deployable" class="delivery__note">
      {{ t('devices.manualOnly') }}
    </p>
    <RvSwitch
      v-else
      :description="reason"
      :disabled="busy || (!connection.autoDeliver && !canTurnOn)"
      :label="t('devices.auto.switch')"
      :model-value="connection.autoDeliver"
      @update:model-value="onToggle"
    />
  </section>
</template>

<style scoped>
.delivery {
  display: grid;
  gap: var(--rv-space-4);
  justify-items: start;
  min-width: 0;
}

/* The heading carries the rule that separates this part of the connection
   from the one above it, so neither needs a surface of its own. */
.delivery__title {
  width: 100%;
  padding-bottom: var(--rv-space-2);
  font-size: var(--rv-text-section);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.delivery__note {
  min-width: 0;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}
</style>
