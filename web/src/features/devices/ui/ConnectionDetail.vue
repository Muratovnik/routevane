<script setup lang="ts">
import { computed, nextTick, ref, useId, useTemplateRef, watch } from 'vue'

import {
  addressRefused,
  failureBodyKey,
} from '@/features/devices/lib/deviceFailure'
import { keepFocus } from '@/features/devices/lib/keepFocus'
import { useConnectionFields } from '@/features/devices/model/useConnectionFields'
import type { DevicesModel } from '@/features/devices/model/useDevices'
import ConnectionCredential from '@/features/devices/ui/ConnectionCredential.vue'
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
 * One registered connection: what it is, what it is made of, the password it
 * may hold, and whether Routevane may deliver to it without being asked each
 * time.
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
const fields = useTemplateRef<InstanceType<typeof ConnectionFields>>('fields')
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
const isRouter = computed(
  () =>
    props.devices.targets.value.find(
      (target) => target.id === props.connection.targetID,
    )?.kind === 'router',
)

const {
  needsAccount,
  passwordLabel,
  problems: problemsOf,
} = useConnectionFields(
  requirements,
  computed(() => props.connection.targetID),
)

const draft = ref({ account: '', address: '', interfaceName: '', name: '' })
const reveal = ref(false)
const refusedAddress = ref(false)
const forgetting = ref(false)
const saving = ref(false)
// The words under the failure notice, or null when the refusal is stated at its
// own field instead.
const workBodyKey = computed(() => failureBodyKey(props.devices.failure.value))

const restore = (): void => {
  draft.value = {
    account: props.connection.account,
    address: props.connection.address,
    interfaceName: props.connection.interfaceName,
    name: props.connection.name,
  }
  reveal.value = false
  refusedAddress.value = false
}

watch(() => props.connection.id, restore, { immediate: true })
// A refusal was about the address as it was typed; a new address is a new
// question.
watch(
  () => draft.value.address,
  () => {
    refusedAddress.value = false
  },
)

const trimmed = computed(() => ({
  account: draft.value.account.trim(),
  address: draft.value.address.trim(),
  interfaceName: draft.value.interfaceName.trim(),
  name: draft.value.name.trim(),
}))
const problems = computed(() => problemsOf(trimmed.value))

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

// What a changed destination ends, from what this connection actually holds.
// The password and the delivery are the same facts the confirmation of
// forgetting states, so they share its words. What happens to the groups on
// the router differs: forgetting only stops managing them, while a changed
// destination also means the next send creates them again, so the old ones are
// the operator's to remove (ADR 0040).
const heldConsequences = computed(() => [
  ...(props.connection.credentialStored
    ? ['devices.consequence.credential']
    : []),
  ...(props.connection.autoDeliver ? ['devices.consequence.autoDelivery'] : []),
])
const warning = computed(() =>
  connectionChanged.value &&
  (heldConsequences.value.length > 0 || isRouter.value)
    ? [
        ...heldConsequences.value,
        ...(isRouter.value ? ['devices.consequence.routes.edit'] : []),
      ].map((key) => t(key))
    : [],
)
const forgetConsequences = computed(() => [
  ...heldConsequences.value,
  ...(isRouter.value ? ['devices.consequence.routes'] : []),
  'devices.consequence.files',
])

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
  if (disabled.value || !dirty.value) return
  // Submitting is always possible. What would be refused is stated beside the
  // fields it is about, and focus goes to the first of them.
  if (Object.keys(problems.value).length > 0) {
    reveal.value = true
    await nextTick()
    fields.value?.focusFirstInvalid()
    return
  }
  // Read before the write: the re-read that follows replaces this card with
  // the server's own, and the message depends on what was true beforehand.
  const endedPassword =
    connectionChanged.value && props.connection.credentialStored
  const endedDelivery = connectionChanged.value && props.connection.autoDeliver
  saving.value = true
  refusedAddress.value = false
  // The button stops saying it is working when the write ends, before focus
  // is returned: a Save that is still waiting on its own label would be passed
  // over for the fallback, though it is available again as soon as it renders.
  const saved = await keepFocus(
    async () => {
      try {
        return await props.devices.update(
          props.connection.id,
          trimmed.value.name,
          trimmed.value.address,
          trimmed.value.account,
          trimmed.value.interfaceName,
        )
      } finally {
        saving.value = false
      }
    },
    () => fields.value?.element('name'),
  )
  if (!saved) {
    // The service names the field it refused, so the words go beside it; a
    // refusal that names none is stated once, below.
    if (addressRefused(props.devices.failure.value)) {
      refusedAddress.value = true
      await nextTick()
      fields.value?.focus('address')
    }
    return
  }
  restore()
  emit(
    'update:confirmation',
    [
      t('devices.updated'),
      ...(endedPassword ? [t('devices.updated.credential')] : []),
      ...(endedDelivery ? [t('devices.updated.autoDelivery')] : []),
    ].join(' '),
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
      <div class="connection__identity">
        <!-- The mark stands in the heading's own line, so it keeps that line's
             centre whatever stands beneath it. -->
        <h2
          :id="headingID"
          ref="heading"
          class="connection__name"
          tabindex="-1"
        >
          <RvIcon :name="targetIcon(connection.targetID)" />
          <span class="connection__name-text">{{ connection.name }}</span>
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
        novalidate
        @submit.prevent="onSave"
      >
        <ConnectionFields
          ref="fields"
          v-model:account="draft.account"
          v-model:address="draft.address"
          v-model:interface-name="draft.interfaceName"
          v-model:name="draft.name"
          :disabled="disabled"
          id-prefix="device-edit"
          :problems="problems"
          :refused-address="refusedAddress"
          :requirements="requirements"
          :reveal="reveal"
          :target-id="connection.targetID"
        />
        <RvStateNotice
          v-if="warning.length > 0"
          :body="warning.join(' ')"
          :title="t('devices.revoke.title')"
          tone="warning"
        />
        <div class="connection__actions">
          <RvButton :disabled="!dirty || disabled" @click="restore()">{{
            t('action.cancel')
          }}</RvButton>
          <RvButton
            :disabled="!dirty || disabled"
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

    <ConnectionCredential
      v-if="deployerKnown && needsAccount"
      :connection="connection"
      :devices="devices"
      :label="passwordLabel"
    />

    <ConnectionDelivery
      v-if="connection.autoDeliver || requirementsReady"
      :connection="connection"
      :deployer-known="deployerKnown"
      :devices="devices"
      :needs-credential="needsAccount"
      @notice="emit('update:confirmation', $event)"
    />

    <RvStateNotice
      v-if="devices.work.value === 'failed' && workBodyKey !== null"
      :body="t(workBodyKey)"
      live
      :title="t('devices.work.failed')"
      tone="failed"
    />
    <RvDialog
      :close-label="t('action.close')"
      :dismissible="!devices.busy.value"
      :open="forgetting"
      :title="t('devices.forget.confirm', { name: connection.name })"
      variant="panel"
      @update:open="$event === false && (forgetting = false)"
    >
      <!-- What forgetting ends, read from what this connection holds. -->
      <ul class="connection__consequences">
        <li v-for="key in forgetConsequences" :key="key">{{ t(key) }}</li>
      </ul>
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
          variant="danger"
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
  display: flex;
  gap: var(--rv-space-3);
  align-items: center;
  min-width: 0;
  font-size: var(--rv-text-module);
}

.connection__name-text {
  min-width: 0;
  overflow-wrap: anywhere;
}

.connection__meta {
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

.connection__consequences {
  display: grid;
  gap: var(--rv-space-2);
  padding-inline-start: var(--rv-space-5);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  list-style: disc;
}
</style>
