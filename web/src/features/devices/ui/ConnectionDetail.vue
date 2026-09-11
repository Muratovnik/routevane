<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'

import { useConnectionFields } from '@/features/devices/model/useConnectionFields'
import type { DevicesModel } from '@/features/devices/model/useDevices'
import ConnectionDelivery from '@/features/devices/ui/ConnectionDelivery.vue'
import ConnectionFields from '@/features/devices/ui/ConnectionFields.vue'
import type { DeviceCard } from '@/shared/api/devices'
import { useLocale } from '@/shared/i18n/useLocale'
import { targetIcon } from '@/shared/lib/targetIcon'
import RvButton from '@/shared/ui/RvButton.vue'
import RvDialog from '@/shared/ui/RvDialog.vue'
import RvFacts from '@/shared/ui/RvFacts.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvMenu from '@/shared/ui/RvMenu.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import type { Fact, MenuItem } from '@/shared/ui/types'

/**
 * One registered connection: what it is, what it is made of, and whether
 * Routevane may deliver to it without being asked each time.
 *
 * Its target is not among the editable parts. A device that became a different
 * kind of device is a different device, and a stored credential would then
 * point somewhere the operator never approved.
 */
const props = defineProps<{
  confirmation: string
  connection: DeviceCard
  devices: DevicesModel
  /** The target's name in the reader's language, which the catalog owns. */
  targetTitle: string
}>()

const emit = defineEmits<{
  'update:confirmation': [message: string]
}>()

const { t } = useLocale()
const headingID = useId()
const heading = ref<HTMLElement | null>(null)
defineExpose({ focusHeading: (): void => heading.value?.focus() })

const requirements = computed(
  () =>
    props.devices.deployableTargets.value.find(
      (target) => target.targetID === props.connection.targetID,
    )?.requirements ?? null,
)
// `ready` means this tab holds a current authoritative answer about what the
// target's deployer needs. Without one the parameters are stated rather than
// edited: the fields to offer are exactly what is unknown.
const requirementsReady = computed(
  () => props.devices.requirementsState.value === 'ready',
)
const deployerKnown = computed(
  () => requirementsReady.value && requirements.value !== null,
)
const disabled = computed(
  () => props.devices.busy.value || props.devices.state.value === 'stale',
)

const { needsAccount, needsInterface } = useConnectionFields(
  requirements,
  computed(() => props.connection.targetID),
)

const draft = ref({ account: '', address: '', interfaceName: '', name: '' })
const reveal = ref(false)
const forgetting = ref(false)
const saving = ref(false)

const restore = (): void => {
  draft.value = {
    account: props.connection.account,
    address: props.connection.address,
    interfaceName: props.connection.interfaceName,
    name: props.connection.name,
  }
  reveal.value = false
}

watch(() => props.connection.id, restore, { immediate: true })

const trimmed = computed(() => ({
  account: draft.value.account.trim(),
  address: draft.value.address.trim(),
  interfaceName: draft.value.interfaceName.trim(),
  name: draft.value.name.trim(),
}))

// The destination and account consent was given for. Changing any of them is
// what the server answers by revoking that consent.
const connectionChanged = computed(
  () =>
    trimmed.value.address !== props.connection.address ||
    trimmed.value.account !== props.connection.account ||
    trimmed.value.interfaceName !== props.connection.interfaceName,
)
const dirty = computed(
  () => connectionChanged.value || trimmed.value.name !== props.connection.name,
)
const complete = computed(
  () =>
    trimmed.value.name !== '' &&
    trimmed.value.address !== '' &&
    (!needsAccount.value || trimmed.value.account !== '') &&
    (!needsInterface.value || trimmed.value.interfaceName !== ''),
)
const canSave = computed(
  () =>
    dirty.value && complete.value && requirementsReady.value && !disabled.value,
)
const revoking = computed(
  () => props.connection.autoDeliver && connectionChanged.value,
)

const facts = computed<Fact[]>(() => [
  {
    key: 'address',
    label: t('devices.field.address'),
    mono: true,
    value: props.connection.address,
  },
  ...(props.connection.account === ''
    ? []
    : [
        {
          key: 'account',
          label: t('devices.field.account'),
          value: props.connection.account,
        },
      ]),
  ...(props.connection.interfaceName === ''
    ? []
    : [
        {
          key: 'interface',
          label: t('devices.field.interface'),
          mono: true,
          value: props.connection.interfaceName,
        },
      ]),
])

const menuItems = computed<MenuItem[]>(() => [
  { icon: 'trash', key: 'forget', label: t('devices.forget') },
])

const onSave = async (): Promise<void> => {
  if (!dirty.value || disabled.value) return
  if (!complete.value) {
    reveal.value = true
    return
  }
  // Read before the write: the re-read that follows replaces this card with
  // the server's own, and the message depends on what was true beforehand.
  const revoked = revoking.value
  saving.value = true
  const saved = await props.devices.update(
    props.connection.id,
    trimmed.value.name,
    trimmed.value.address,
    trimmed.value.account,
    trimmed.value.interfaceName,
  )
  saving.value = false
  if (!saved) return
  await nextTick()
  restore()
  emit(
    'update:confirmation',
    t(revoked ? 'devices.updated.revoked' : 'devices.updated'),
  )
}

const onForget = async (): Promise<void> => {
  emit('update:confirmation', '')
  await props.devices.forget(props.connection.id)
  forgetting.value = false
}
</script>

<template>
  <section :aria-labelledby="headingID" class="connection">
    <header class="connection__header">
      <RvIcon :name="targetIcon(connection.targetID)" />
      <div class="connection__identity">
        <h2
          :id="headingID"
          ref="heading"
          class="connection__name"
          tabindex="-1"
        >
          {{ connection.name }}
        </h2>
        <p class="connection__meta">
          {{ targetTitle }}
          <span aria-hidden="true"> · </span>
          <span class="connection__address">{{ connection.address }}</span>
        </p>
      </div>
      <RvMenu
        :disabled="disabled"
        :items="menuItems"
        :label="t('devices.menu', { name: connection.name })"
        @select="forgetting = true"
      />
    </header>

    <RvStateNotice
      v-if="!requirementsReady"
      :body="
        devices.requirementsState.value === 'loading'
          ? undefined
          : t('devices.requirements.failed.body')
      "
      :title="
        t(
          devices.requirementsState.value === 'loading'
            ? 'devices.requirements.reading'
            : 'devices.requirements.failed',
        )
      "
      :tone="devices.requirementsState.value === 'loading' ? 'busy' : 'warning'"
    >
      <template #action>
        <RvButton
          :disabled="devices.requirementsState.value === 'loading'"
          @click="devices.retryRequirements"
          >{{ t('action.retry') }}</RvButton
        >
      </template>
    </RvStateNotice>

    <RvStateNotice
      v-if="confirmation !== ''"
      live
      :title="confirmation"
      tone="ready"
    />

    <section class="connection__part">
      <h3 class="connection__part-title">{{ t('devices.parameters') }}</h3>
      <form
        v-if="requirementsReady"
        class="connection__form"
        @submit.prevent="onSave"
      >
        <ConnectionFields
          v-model:account="draft.account"
          v-model:address="draft.address"
          v-model:interface-name="draft.interfaceName"
          v-model:name="draft.name"
          :disabled="disabled"
          id-prefix="device-edit"
          :requirements="requirements"
          :reveal="reveal"
          :target-id="connection.targetID"
        />
        <RvStateNotice
          v-if="revoking"
          :body="t('devices.parameters.revoke.body')"
          :title="t('devices.parameters.revoke')"
          tone="warning"
        />
        <div class="connection__actions">
          <RvButton :disabled="!dirty || disabled" @click="restore()">{{
            t('action.cancel')
          }}</RvButton>
          <RvButton
            :disabled="!canSave"
            :loading="saving"
            :loading-label="t('action.save')"
            type="submit"
            variant="primary"
            >{{ t('action.save') }}</RvButton
          >
        </div>
      </form>
      <RvFacts v-else :items="facts" />
    </section>

    <ConnectionDelivery
      v-if="connection.autoDeliver || requirementsReady"
      :connection="connection"
      :deployer-known="deployerKnown"
      :devices="devices"
      :needs-credential="needsAccount"
      @notice="emit('update:confirmation', $event)"
    />

    <RvStateNotice
      v-if="devices.work.value === 'failed'"
      :body="t('devices.work.failed.body')"
      live
      :title="t('devices.work.failed')"
      tone="failed"
    />
    <RvStateNotice
      v-if="devices.state.value === 'stale'"
      :body="t('devices.stale.body')"
      live
      :title="t('devices.stale')"
      tone="warning"
    >
      <template #action>
        <RvButton @click="devices.retryDevices">{{
          t('action.retry')
        }}</RvButton>
      </template>
    </RvStateNotice>

    <RvDialog
      :close-label="t('action.close')"
      :dismissible="!devices.busy.value"
      :open="forgetting"
      :title="t('devices.forget.confirm', { name: connection.name })"
      variant="panel"
      @update:open="$event === false && (forgetting = false)"
    >
      <p class="connection__note">{{ t('devices.forget.confirm.body') }}</p>
      <template #footer>
        <RvButton
          :disabled="devices.busy.value"
          variant="quiet"
          @click="forgetting = false"
          >{{ t('action.cancel') }}</RvButton
        >
        <RvButton
          :disabled="devices.busy.value"
          :loading="devices.busy.value"
          :loading-label="t('devices.forget')"
          variant="primary"
          @click="onForget"
          >{{ t('devices.forget') }}</RvButton
        >
      </template>
    </RvDialog>
  </section>
</template>

<style scoped>
.connection {
  display: grid;
  gap: var(--rv-space-6);
  align-content: start;
  min-width: 0;
  padding: var(--rv-space-6);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-lg);
}

.connection__header {
  display: flex;
  gap: var(--rv-space-3);
  align-items: flex-start;
  min-width: 0;
}

.connection__identity {
  display: grid;
  gap: var(--rv-space-1);
  flex: 1;
  min-width: 0;
}

.connection__name {
  font-size: var(--rv-text-module);
  overflow-wrap: anywhere;
}

.connection__meta,
.connection__note {
  min-width: 0;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}

.connection__address {
  font-family: var(--rv-font-mono);
}

.connection__part {
  display: grid;
  gap: var(--rv-space-4);
  min-width: 0;
}

/* The heading carries the rule that separates one part of the connection from
   the next, so the parts need no surfaces of their own inside this one. */
.connection__part-title {
  padding-bottom: var(--rv-space-2);
  font-size: var(--rv-text-section);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.connection__form {
  display: grid;
  gap: var(--rv-space-4);
  min-width: 0;
}

.connection__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  justify-content: flex-end;
}
</style>
