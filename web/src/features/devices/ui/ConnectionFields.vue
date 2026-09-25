<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { useConnectionFields } from '@/features/devices/model/useConnectionFields'
import type { ConnectionRequirements } from '@/shared/api/deploy'
import { useLocale } from '@/shared/i18n/useLocale'
import { validAddress } from '@/shared/lib/deviceAddress'
import RvField from '@/shared/ui/RvField.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'

/**
 * The fields one connection is made of, in the grid that registering and
 * editing share. Which fields exist and what they are called is the chosen
 * target's own answer, so nothing here is written for one device.
 */
const props = defineProps<{
  disabled?: boolean
  /** Keeps the creation form's controls distinct from the editor's. */
  idPrefix: string
  requirements: ConnectionRequirements | null
  /** State every missing field, not only the ones already left behind. */
  reveal?: boolean
  targetId: string
}>()

const name = defineModel<string>('name', { required: true })
const address = defineModel<string>('address', { required: true })
const account = defineModel<string>('account', { required: true })
const interfaceName = defineModel<string>('interfaceName', { required: true })

const { t } = useLocale()
const {
  accountLabel,
  addressHint,
  addressLabel,
  addressPlaceholder,
  interfaceHint,
  interfaceLabel,
  needsAccount,
  needsInterface,
} = useConnectionFields(
  computed(() => props.requirements),
  computed(() => props.targetId),
)

// Nothing is reported as missing before the operator has had it: a field
// states its own omission once they have left it, and the whole draft states
// its omissions when a submission asked for them.
const visited = ref({
  account: false,
  address: false,
  interfaceName: false,
  name: false,
})

watch(
  () => props.targetId,
  () => {
    visited.value = {
      account: false,
      address: false,
      interfaceName: false,
      name: false,
    }
  },
)

const error = (value: string, seen: boolean): string | undefined =>
  (seen || props.reveal === true) && value.trim() === ''
    ? t('devices.validation.required')
    : undefined

// An address that is missing is missing; one that is written in a shape no
// deployer accepts is answered here rather than by a refusal that names no
// field. What the shape is stays in `shared/lib`, and which networks a device
// may live on stays on the server.
const addressError = computed(() => {
  const required = error(address.value, visited.value.address)
  if (required !== undefined) return required
  if (!(visited.value.address || props.reveal === true)) return undefined
  const value = address.value.trim()
  if (value === '' || validAddress(value)) return undefined
  return t('devices.validation.address', {
    example: addressPlaceholder.value,
  })
})
</script>

<template>
  <div class="connection-fields">
    <RvField
      :error="error(name, visited.name)"
      :input-id="`${idPrefix}-name`"
      :label="t('devices.field.name')"
    >
      <template #default="{ describedBy, invalid }">
        <RvTextInput
          v-model="name"
          :described-by="describedBy"
          :disabled="disabled"
          :input-id="`${idPrefix}-name`"
          :invalid="invalid"
          maxlength="120"
          @blur="visited.name = true"
        />
      </template>
    </RvField>

    <RvField
      :error="addressError"
      :hint="addressHint"
      :input-id="`${idPrefix}-address`"
      :label="addressLabel"
    >
      <template #default="{ describedBy, invalid }">
        <RvTextInput
          v-model="address"
          :described-by="describedBy"
          :disabled="disabled"
          :input-id="`${idPrefix}-address`"
          :invalid="invalid"
          maxlength="512"
          mono
          :placeholder="addressPlaceholder"
          @blur="visited.address = true"
        />
      </template>
    </RvField>

    <RvField
      v-if="needsAccount"
      :error="error(account, visited.account)"
      :input-id="`${idPrefix}-account`"
      :label="accountLabel"
    >
      <template #default="{ describedBy, invalid }">
        <RvTextInput
          v-model="account"
          :described-by="describedBy"
          :disabled="disabled"
          :input-id="`${idPrefix}-account`"
          :invalid="invalid"
          mono
          maxlength="120"
          @blur="visited.account = true"
        />
      </template>
    </RvField>

    <RvField
      v-if="needsInterface"
      :error="error(interfaceName, visited.interfaceName)"
      :hint="interfaceHint"
      :input-id="`${idPrefix}-interface`"
      :label="interfaceLabel"
    >
      <template #default="{ describedBy, invalid }">
        <RvTextInput
          v-model="interfaceName"
          :described-by="describedBy"
          :disabled="disabled"
          :input-id="`${idPrefix}-interface`"
          :invalid="invalid"
          mono
          maxlength="120"
          @blur="visited.interfaceName = true"
        />
      </template>
    </RvField>
  </div>
</template>

<style scoped>
/* Two columns where the pane has room for two reading widths, one where it
   does not. A lone remaining field keeps its own column rather than stretching
   across the pane. */
.connection-fields {
  display: grid;
  grid-template-columns: repeat(
    auto-fit,
    minmax(min(100%, var(--rv-form-column-min)), 1fr)
  );
  gap: var(--rv-space-5);
  align-items: start;
  min-width: 0;
}
</style>
