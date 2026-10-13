<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'

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
 * say it when the operator has had the field or the host asked for every
 * problem at once.
 */
const props = defineProps<{
  disabled?: boolean
  /** Keeps the creation form's controls distinct from the editor's. */
  idPrefix: string
  problems: FieldProblems
  requirements: ConnectionRequirements | null
  /** State every problem, not only those of fields already left behind. */
  reveal?: boolean
  /** The service refused the address this draft changed. */
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

// Nothing is reported as wrong before the operator has had it: a field states
// its own problem once they have left it, and the whole draft states its
// problems when a submission asked for them.
const emptyVisits = (): Record<FieldKey, boolean> => ({
  account: false,
  address: false,
  interfaceName: false,
  name: false,
})
const visited = ref(emptyVisits())
watch(
  () => props.targetId,
  () => {
    visited.value = emptyVisits()
  },
)

const message = (problem: FieldProblem): string => {
  if (problem === 'required') return t('devices.validation.required')
  if (problem === 'local') return t('devices.validation.address.local')
  return t('devices.validation.address', {
    example: addressPlaceholder.value,
  })
}

const errorOf = (key: FieldKey): string | undefined => {
  const problem = props.problems[key]
  if (problem !== undefined && (visited.value[key] || props.reveal === true))
    return message(problem)
  // The service names no field, so the host only says the address when the
  // address is what the draft changed.
  if (key === 'address' && props.refusedAddress === true)
    return t('devices.address.refused')
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
          @blur="visited.name = true"
        />
      </template>
    </RvField>

    <RvField
      aligned
      :error="errorOf('address')"
      :hint="addressHint"
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
          @blur="visited.address = true"
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
          @blur="visited.account = true"
        />
      </template>
    </RvField>

    <RvField
      v-if="needsInterface"
      aligned
      :error="errorOf('interfaceName')"
      :hint="interfaceHint"
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
