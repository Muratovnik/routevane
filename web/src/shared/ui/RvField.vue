<script lang="ts">
import { useFormField } from '@nuxt/ui/composables/useFormField'
import { computed, defineComponent, provide, type SlotsType } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import { fieldLabelId, fieldLabelKey } from '@/shared/lib/fieldLabel'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'

/**
 * The link between the library's form field and whatever control fills it.
 *
 * The field's `<label for>` names the id its control registers. A Nuxt UI
 * input registers itself, but a Routevane choice drawn on Reka does not, and
 * the label would then point at an id nobody carries. This binding registers
 * the caller's id for every kind of control, and hands the slot the ids of
 * the texts the field actually drew. Registering also withholds the field
 * from the control underneath, so the wiring is stated once, here, rather
 * than a second time by a library input.
 */
const FieldBinding = defineComponent({
  name: 'RvFieldBinding',
  props: {
    inputId: { type: String, required: true },
    label: { type: String, required: true },
  },
  slots: Object as SlotsType<{
    default: { describedBy: string | undefined; invalid: boolean }
  }>,
  setup(props, { slots }) {
    const { ariaAttrs } = useFormField({ id: props.inputId })
    provide(
      fieldLabelKey,
      computed(() => ({
        inputId: props.inputId,
        label: props.label,
        labelId: fieldLabelId(props.inputId),
      })),
    )
    const describedBy = computed(() => ariaAttrs.value?.['aria-describedby'])
    const invalid = computed(() => ariaAttrs.value?.['aria-invalid'] === true)
    return () =>
      slots.default?.({
        describedBy: describedBy.value,
        invalid: invalid.value,
      })
  },
})
</script>

<script setup lang="ts">
/**
 * One form field: label, the control, and — while there is one — the error the
 * control caused, under it. Nuxt UI draws the field. An example of the format
 * belongs in the control's placeholder and an explanation in `info`, which
 * opens from an informer beside the label; neither is a line under the label.
 * `hint` is the older form of the same thing — the library's description
 * between the label and the control — and stays only for the fields that have
 * not moved to an informer. The slot receives the ids to point
 * `aria-describedby` at, so the wiring cannot drift from what is rendered.
 */
const props = defineProps<{
  /** Share caption and control rows with adjacent fields in a parent grid. */
  aligned?: boolean
  /** Announce an asynchronous error while focus remains on another control. */
  announceError?: boolean
  error?: string
  hint?: string
  /** What the field means or how it is read, offered from an informer. */
  info?: string
  inputId: string
  label: string
}>()

const { t } = useLocale()

const failure = computed(() =>
  props.error === undefined || props.error === '' ? undefined : props.error,
)
const description = computed(() => (props.hint === '' ? undefined : props.hint))
</script>

<template>
  <UFormField
    :class="{ 'rv-field--aligned': aligned }"
    :description="description"
    :error="failure"
    :label="label"
    :ui="{ labelWrapper: 'justify-start' }"
  >
    <!-- The label's text carries the id a choice button names itself by. -->
    <template #label>
      <span :id="fieldLabelId(inputId)">{{ label }}</span>
    </template>
    <!-- Beside the label and outside it, so the label's own text stays what
         names the control. -->
    <template v-if="info !== undefined" #hint>
      <RvInfoTip
        inline
        :label="t('field.info', { field: label })"
        :text="info"
      />
    </template>
    <!-- A new id is a new registration, so the binding is keyed by it. -->
    <FieldBinding
      :key="inputId"
      v-slot="binding"
      :input-id="inputId"
      :label="label"
    >
      <slot :described-by="binding.describedBy" :invalid="binding.invalid" />
    </FieldBinding>
    <template v-if="announceError === true && failure !== undefined" #error>
      <span role="alert">{{ failure }}</span>
    </template>
  </UFormField>
</template>

<style scoped>
.rv-field--aligned {
  display: grid;
  grid-row: span 2;
  grid-template-rows: subgrid;
  row-gap: 0;
  align-self: stretch;
  min-width: 0;
}
</style>
