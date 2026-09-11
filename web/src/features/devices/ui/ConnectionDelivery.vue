<script setup lang="ts">
import { computed, ref } from 'vue'

import type { DevicesModel } from '@/features/devices/model/useDevices'
import type { DeviceCard } from '@/shared/api/devices'
import { useLocale } from '@/shared/i18n/useLocale'
import RvButton from '@/shared/ui/RvButton.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'

/**
 * Whether Routevane may deliver to this connection without being asked each
 * time. It is its own act and its own section: saving a connection's
 * parameters never grants it, and it is one control rather than a switch and a
 * button beside it.
 *
 * The credential lives here and nowhere else. It is held for as long as it
 * takes to send it, and this component is unmounted with the connection it
 * belongs to, so departure takes the field with it.
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
const credential = ref('')

const disabled = computed(
  () => props.devices.busy.value || props.devices.state.value === 'stale',
)
// Offering to turn delivery on means this build can apply a file to this kind
// of device, knows what its deployer needs, and has somewhere to keep a
// password when one is needed.
const canOfferConsent = computed(
  () =>
    props.connection.deployable &&
    props.deployerKnown &&
    (props.devices.secretStoreAvailable.value || !props.needsCredential),
)
const router = computed(
  () =>
    props.devices.targets.value.find(
      (target) => target.id === props.connection.targetID,
    )?.kind === 'router',
)

const onEnable = async (): Promise<void> => {
  const submitted = credential.value
  if (props.needsCredential && submitted === '') return
  credential.value = ''
  const done = await props.devices.enable(props.connection.id, submitted)
  if (!done) return
  emit(
    'notice',
    t(
      router.value
        ? 'devices.auto.enabled.next.router'
        : 'devices.auto.enabled.next.other',
    ),
  )
}

const onDisable = async (): Promise<void> => {
  emit('notice', '')
  await props.devices.disable(props.connection.id)
}
</script>

<template>
  <section class="delivery">
    <h3 class="delivery__title">{{ t('devices.auto.title') }}</h3>

    <!-- On: the state is stated and the only offer is to withdraw it. The
         password is not shown, not stood in for, and not read back. -->
    <template v-if="connection.autoDeliver">
      <RvStatus
        :label="
          t(
            deployerKnown && needsCredential
              ? 'devices.auto.on'
              : 'devices.auto.on.noCredential',
          )
        "
        tone="ready"
      />
      <div class="delivery__actions">
        <RvButton :disabled="disabled" @click="onDisable">{{
          t('devices.auto.disable')
        }}</RvButton>
      </div>
    </template>

    <form
      v-else-if="canOfferConsent"
      class="delivery__consent"
      @submit.prevent="onEnable"
    >
      <p class="delivery__note">
        {{
          t(
            needsCredential
              ? 'devices.auto.consent'
              : 'devices.auto.consent.noCredential',
          )
        }}
      </p>
      <div v-if="needsCredential" class="delivery__credential">
        <RvField input-id="device-credential" :label="t('devices.auto.label')">
          <RvTextInput
            v-model="credential"
            autocomplete="off"
            :disabled="disabled"
            input-id="device-credential"
            type="password"
          />
        </RvField>
      </div>
      <div class="delivery__actions">
        <RvButton
          :disabled="disabled || (needsCredential && credential === '')"
          type="submit"
          >{{ t('devices.auto.enable') }}</RvButton
        >
      </div>
    </form>

    <!-- A device this build cannot apply a file to, and a system with nowhere
         to keep a password, are two different facts. Neither is a broken
         switch. -->
    <p v-else class="delivery__note">
      {{
        t(
          connection.deployable
            ? 'devices.auto.unavailable'
            : 'devices.manualOnly',
        )
      }}
    </p>
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

.delivery__consent {
  display: grid;
  gap: var(--rv-space-4);
  justify-items: start;
  min-width: 0;
}

.delivery__credential {
  width: min(100%, var(--rv-measure-field));
}

.delivery__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
}
</style>
