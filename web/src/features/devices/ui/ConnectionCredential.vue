<script setup lang="ts">
import { computed, nextTick, ref, useId, useTemplateRef, watch } from 'vue'

import {
  passwordRefusalKey,
  passwordRefused,
} from '@/features/devices/lib/deviceFailure'
import { keepFocus } from '@/features/devices/lib/keepFocus'
import type { DevicesModel } from '@/features/devices/model/useDevices'
import type { DeviceCard } from '@/shared/api/devices'
import { useLocale } from '@/shared/i18n/useLocale'
import { useNotify } from '@/shared/model/useNotify'
import RvButton from '@/shared/ui/RvButton.vue'
import RvConfirmDialog from '@/shared/ui/RvConfirmDialog.vue'
import RvFact from '@/shared/ui/RvFact.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'

/**
 * The password of a connection whose format needs one, as its own saved fact
 * (ADR 0050). Saving it is an act on the connection and grants nothing: whether
 * Routevane may send without being asked is the next section's switch.
 *
 * The password lives in this component for the one request that carries it and
 * is cleared with the request. This component is unmounted with the connection
 * it belongs to, so departure takes the field with it. The page never reads a
 * saved password back and shows no stand-in for one.
 */
const props = defineProps<{
  connection: DeviceCard
  devices: DevicesModel
  /** What the target's deployer calls the password, in the reader's language. */
  label: string
}>()

const { t, tor } = useLocale()
const { notify } = useNotify()
const headingID = useId()
const fieldID = useId()
const password = ref('')
// The service refused the password it was given and said why: the words go at
// the field until the operator types another.
const refusal = ref('')
watch(password, () => {
  refusal.value = ''
})
// A saved password is replaced by saying so, not by editing a stand-in for it.
const replacing = ref(false)
// Removing a password also ends automatic delivery, so it asks first.
const removing = ref(false)
const field = useTemplateRef<InstanceType<typeof RvTextInput>>('field')
const replace = useTemplateRef<InstanceType<typeof RvButton>>('replace')

const disabled = computed(
  () => props.devices.busy.value || props.devices.state.value === 'stale',
)
const saved = computed(() => props.connection.credentialStored)
// Where the operating system offers no store, a password cannot be kept, and
// the section says so instead of offering a field that could not keep one.
const unavailable = computed(
  () => !saved.value && !props.devices.secretStoreAvailable.value,
)
const editing = computed(() => !saved.value || replacing.value)

const focusReplace = (): HTMLElement | null =>
  (replace.value?.$el as HTMLElement | undefined) ?? null

const onSave = async (): Promise<void> => {
  if (disabled.value || password.value === '') return
  const submitted = password.value
  password.value = ''
  const done = await keepFocus(
    () => props.devices.saveCredential(props.connection.id, submitted),
    focusReplace,
  )
  if (done) {
    replacing.value = false
    announceCheck()
    return
  }
  password.value = ''
  const failure = props.devices.failure.value
  if (passwordRefused(failure)) {
    await nextTick()
    // Clearing the field above is a change too; the refusal is stated after it.
    refusal.value = passwordRefusalKey(failure)
    field.value?.element()?.focus()
  }
}

// Saving signed in with the password (ADR 0059). What that sign-in showed is
// said once, after the fact that the password is kept.
const announceCheck = (): void => {
  const check = props.devices.credentialCheck.value
  if (check === null || check.result === 'not_applicable') return
  if (check.result === 'verified') {
    notify({
      title: t('devices.credential.saved'),
      body: t('devices.credential.check.verified'),
      tone: 'ready',
    })
    return
  }
  if (check.result === 'interface_missing') {
    notify({
      title: t('devices.credential.saved'),
      body: t('devices.credential.check.interface', {
        name: props.connection.interfaceName,
      }),
      tone: 'waiting',
    })
    return
  }
  notify({
    title: t('devices.credential.check.unverified'),
    body: tor(`error.${check.code}`, t('devices.credential.check.failed')),
    tone: 'waiting',
  })
}

const onRemove = async (): Promise<void> => {
  if (disabled.value) return
  const done = await keepFocus(
    () => props.devices.removeCredential(props.connection.id),
    () => field.value?.element(),
  )
  removing.value = false
  if (done) notify({ title: t('devices.credential.removed') })
}

const startReplacing = async (): Promise<void> => {
  replacing.value = true
  await nextTick()
  field.value?.element()?.focus()
}

const stopReplacing = async (): Promise<void> => {
  password.value = ''
  // A refusal belongs to the attempt that drew it, and the field was already
  // empty, so clearing it above is no change for the watcher to see.
  refusal.value = ''
  replacing.value = false
  await nextTick()
  focusReplace()?.focus()
}
</script>

<template>
  <section :aria-labelledby="headingID" class="credential">
    <h3 :id="headingID" class="credential__title">
      {{ t('devices.credential.title') }}
    </h3>

    <p v-if="unavailable" class="credential__note">
      {{ t('devices.credential.unavailable') }}
    </p>

    <form v-else-if="editing" class="credential__form" @submit.prevent="onSave">
      <!-- The act stands in the field's own row, so the error — when there is
           one — runs under the whole row, not under the input alone. -->
      <RvField
        :error="refusal === '' ? undefined : t(refusal)"
        :input-id="fieldID"
        :label="label"
      >
        <template #default="{ describedBy, invalid }">
          <div class="credential__row">
            <RvTextInput
              ref="field"
              v-model="password"
              autocomplete="off"
              class="credential__input"
              :described-by="describedBy"
              :disabled="disabled"
              :input-id="fieldID"
              :invalid="invalid"
              type="password"
            />
            <RvButton
              :disabled="disabled || password === ''"
              type="submit"
              variant="primary"
              >{{ t('devices.credential.save') }}</RvButton
            >
            <RvButton
              v-if="saved"
              :disabled="disabled"
              @click="stopReplacing"
              >{{ t('action.cancel') }}</RvButton
            >
          </div>
        </template>
      </RvField>
      <!-- What saving does, beside the act that does it. -->
      <p class="credential__note">{{ t('devices.credential.where') }}</p>
    </form>

    <template v-else>
      <div class="credential__row">
        <RvFact :label="t('devices.credential.saved')" />
        <span class="credential__actions">
          <RvButton ref="replace" :disabled="disabled" @click="startReplacing"
            ><RvIcon name="edit" />{{
              t('devices.credential.replace')
            }}</RvButton
          >
          <RvButton
            :disabled="disabled"
            variant="quiet"
            @click="removing = true"
            ><RvIcon name="trash" />{{
              t('devices.credential.remove')
            }}</RvButton
          >
        </span>
      </div>
      <p class="credential__note">{{ t('devices.credential.where') }}</p>
    </template>

    <RvConfirmDialog
      :busy="devices.busy.value"
      :cancel-label="t('action.cancel')"
      :close-label="t('action.close')"
      :confirm-label="t('devices.credential.remove')"
      :open="removing"
      :title="t('devices.credential.remove.title', { name: connection.name })"
      @confirm="onRemove"
      @update:open="removing = $event"
    >
      <p>{{ t('devices.credential.remove.body') }}</p>
      <p v-if="connection.autoDeliver">
        {{ t('devices.credential.removeEffect') }}
      </p>
    </RvConfirmDialog>
  </section>
</template>

<style scoped>
.credential {
  display: grid;
  gap: var(--rv-space-4);
  min-width: 0;
}

/* The heading carries the rule that separates this part of the connection
   from the one above it, so neither needs a surface of its own. */
.credential__title {
  padding-bottom: var(--rv-space-2);
  font-size: var(--rv-text-section);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

/* The field and the sentence about where the password is kept are one group:
   the sentence stands right under the row, and an error, when there is one,
   comes between them. */
.credential__form {
  display: grid;
  gap: var(--rv-space-2);
  min-width: 0;
}

/* The input takes what the button and Cancel leave of the row, so the row's
   right edge is the edge of the parameter grid above it. */
.credential__row {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  align-items: center;
  min-width: 0;
}

/* The fact reads first; what can be done about it stands at the row's end. */
.credential__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  margin-inline-start: auto;
}

.credential__input {
  flex: 1 1 var(--rv-form-column-min);
  min-width: 0;
}

.credential__note {
  min-width: 0;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}
</style>
