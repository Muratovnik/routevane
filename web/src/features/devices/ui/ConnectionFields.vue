<script setup lang="ts">
import { computed, useTemplateRef } from 'vue'

import {
  FIELD_ORDER,
  useConnectionFields,
  type FieldKey,
  type FieldProblem,
  type FieldProblems,
} from '@/features/devices/model/useConnectionFields'
import type { ConnectionRequirements } from '@/shared/api/deploy'
import { useLocale } from '@/shared/i18n/useLocale'
import RvField from '@/shared/ui/RvField.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'

/**
 * The fields one connection is made of, in the grid that registering and
 * editing share. Which fields exist and what they are called is the chosen
 * target's own answer, so nothing here is written for one device.
 *
 * What is wrong with the draft is the host's answer too — it needs the same
 * answer to decide whether a submission can go — so the fields only say it, and
 * say it once the host has asked for every problem at once by a submission.
 * From then on the problems follow the draft as it is corrected; leaving a
 * field states nothing.
 */
const props = defineProps<{
  disabled?: boolean
  /** Keeps the creation form's controls distinct from the editor's. */
  idPrefix: string
  problems: FieldProblems
  requirements: ConnectionRequirements | null
  /** A submission found problems: state them, and keep them current. */
  reveal?: boolean
  /** The service refused this draft's address and named the field. */
  refusedAddress?: boolean
  targetId: string
}>()

const name = defineModel<string>('name', { required: true })
const address = defineModel<string>('address', { required: true })
const account = defineModel<string>('account', { required: true })
const interfaceName = defineModel<string>('interfaceName', { required: true })

const { t } = useLocale()
const {
  accountLabel,
  addressInfo,
  addressLabel,
  addressPlaceholder,
  addressRefusal,
  interfaceInfo,
  interfaceLabel,
  interfacePlaceholder,
  needsAccount,
  needsInterface,
} = useConnectionFields(
  computed(() => props.requirements),
  computed(() => props.targetId),
)

const message = (problem: FieldProblem): string => {
  if (problem === 'required') return t('devices.validation.required')
  return t('devices.validation.address', {
    example: addressPlaceholder.value,
  })
}

const errorOf = (key: FieldKey): string | undefined => {
  const problem = props.problems[key]
  if (problem !== undefined && props.reveal === true) return message(problem)
  // The service named the address, so the host says it here until the address
  // is edited.
  if (key === 'address' && props.refusedAddress === true)
    return addressRefusal.value
  return undefined
}

type Input = InstanceType<typeof RvTextInput>
const inputs = {
  account: useTemplateRef<Input>('accountInput'),
  address: useTemplateRef<Input>('addressInput'),
  interfaceName: useTemplateRef<Input>('interfaceInput'),
  name: useTemplateRef<Input>('nameInput'),
}

const element = (key: FieldKey): HTMLElement | null =>
  inputs[key].value?.element() ?? null
const focus = (key: FieldKey): void => element(key)?.focus()

defineExpose({
  element,
  focus,
  /** Moves focus to the first field that needs attention; false when none does. */
  focusFirstInvalid: (): boolean => {
    const first = FIELD_ORDER.find((key) => props.problems[key] !== undefined)
    if (first === undefined) return false
    focus(first)
    return true
  },
})
</script>

<template>
  <div class="connection-fields">
    <RvField
      aligned
      :error="errorOf('name')"
      :input-id="`${idPrefix}-name`"
      :label="t('devices.field.name')"
    >
      <template #default="{ describedBy, invalid }">
        <RvTextInput
          ref="nameInput"
          v-model="name"
          aria-required="true"
          :described-by="describedBy"
          :disabled="disabled"
          :input-id="`${idPrefix}-name`"
          :invalid="invalid"
          maxlength="120"
        />
      </template>
    </RvField>

    <RvField
      aligned
      :error="errorOf('address')"
      :info="addressInfo === '' ? undefined : addressInfo"
      :info-label="t('devices.field.address.info')"
      :input-id="`${idPrefix}-address`"
      :label="addressLabel"
    >
      <template #default="{ describedBy, invalid }">
        <RvTextInput
          ref="addressInput"
          v-model="address"
          aria-required="true"
          :described-by="describedBy"
          :disabled="disabled"
          :input-id="`${idPrefix}-address`"
          :invalid="invalid"
          maxlength="512"
          mono
          :placeholder="addressPlaceholder"
        />
      </template>
    </RvField>

    <RvField
      v-if="needsAccount"
      aligned
      :error="errorOf('account')"
      :input-id="`${idPrefix}-account`"
      :label="accountLabel"
    >
      <template #default="{ describedBy, invalid }">
        <RvTextInput
          ref="accountInput"
          v-model="account"
          aria-required="true"
          :described-by="describedBy"
          :disabled="disabled"
          :input-id="`${idPrefix}-account`"
          :invalid="invalid"
          mono
          maxlength="120"
        />
      </template>
    </RvField>

    <RvField
      v-if="needsInterface"
      aligned
      :error="errorOf('interfaceName')"
      :info="interfaceInfo === '' ? undefined : interfaceInfo"
      :info-label="t('devices.field.interface.info')"
      :input-id="`${idPrefix}-interface`"
      :label="interfaceLabel"
    >
      <template #default="{ describedBy, invalid }">
        <RvTextInput
          ref="interfaceInput"
          v-model="interfaceName"
          aria-required="true"
          :described-by="describedBy"
          :disabled="disabled"
          :input-id="`${idPrefix}-interface`"
          :invalid="invalid"
          mono
          maxlength="120"
          :placeholder="interfacePlaceholder"
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
