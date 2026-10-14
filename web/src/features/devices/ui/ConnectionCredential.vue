<script setup lang="ts">
import { computed, nextTick, ref, useId, useTemplateRef, watch } from 'vue'

import { passwordRefused } from '@/features/devices/lib/deviceFailure'
import { keepFocus } from '@/features/devices/lib/keepFocus'
import type { DevicesModel } from '@/features/devices/model/useDevices'
import type { DeviceCard } from '@/shared/api/devices'
import { useLocale } from '@/shared/i18n/useLocale'
import RvButton from '@/shared/ui/RvButton.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'
import RvTooltip from '@/shared/ui/RvTooltip.vue'

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

const { t } = useLocale()
const headingID = useId()
const fieldID = useId()
const password = ref('')
// The service refused the password it was given and said so: the words go at
// the field until the operator types another.
const refused = ref(false)
watch(password, () => {
  refused.value = false
})
// A saved password is replaced by saying so, not by editing a stand-in for it.
const replacing = ref(false)
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
    return
  }
  password.value = ''
  if (passwordRefused(props.devices.failure.value)) {
    await nextTick()
    // Clearing the field above is a change too; the refusal is stated after it.
    refused.value = true
    field.value?.element()?.focus()
  }
}

const onRemove = async (): Promise<void> => {
  if (disabled.value) return
  await keepFocus(
    () => props.devices.removeCredential(props.connection.id),
    () => field.value?.element(),
  )
}

const startReplacing = async (): Promise<void> => {
  replacing.value = true
  await nextTick()
  field.value?.element()?.focus()
}

const stopReplacing = async (): Promise<void> => {
  password.value = ''
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
      <div class="credential__row">
        <RvField
          class="credential__field"
          :error="refused ? t('devices.credential.invalid') : undefined"
          :input-id="fieldID"
          :label="label"
        >
          <template #default="{ describedBy, invalid }">
            <RvTextInput
              ref="field"
              v-model="password"
              autocomplete="off"
              :described-by="describedBy"
              :disabled="disabled"
              :input-id="fieldID"
              :invalid="invalid"
              type="password"
            />
          </template>
        </RvField>
        <RvButton
          class="rv-field-action"
          :disabled="disabled || password === ''"
          type="submit"
          variant="primary"
          >{{ t('devices.credential.save') }}</RvButton
        >
        <RvButton
          v-if="saved"
          class="rv-field-action"
          :disabled="disabled"
          @click="stopReplacing"
          >{{ t('action.cancel') }}</RvButton
        >
      </div>
      <!-- What saving does, beside the act that does it. -->
      <p class="credential__note">{{ t('devices.credential.where') }}</p>
    </form>

    <template v-else>
      <div class="credential__row credential__row--saved">
        <RvStatus :label="t('devices.credential.saved')" tone="ready" />
        <RvButton ref="replace" :disabled="disabled" @click="startReplacing">{{
          t('devices.credential.replace')
        }}</RvButton>
        <RvTooltip :text="t('devices.credential.remove')">
          <RvButton
            :aria-label="t('devices.credential.remove')"
            :disabled="disabled"
            @click="onRemove"
          >
            <RvIcon name="trash" />
          </RvButton>
        </RvTooltip>
      </div>
      <p v-if="connection.autoDeliver" class="credential__note">
        {{ t('devices.credential.removeEffect') }}
      </p>
    </template>
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

.credential__form {
  display: grid;
  gap: var(--rv-space-3);
  min-width: 0;
}

/* A button beside a field stands on the field's control: the row aligns to its
   bottom edge and `rv-field-action` lifts the button by the line the field
   keeps for its error. */
.credential__row {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  align-items: flex-end;
  min-width: 0;
}

.credential__row--saved {
  align-items: center;
}

.credential__field {
  flex: 1 1 var(--rv-form-column-min);
  max-width: var(--rv-measure-field);
  min-width: 0;
}

.credential__note {
  min-width: 0;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}
</style>
