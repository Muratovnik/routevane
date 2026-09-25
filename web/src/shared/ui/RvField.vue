<script lang="ts">
import { useFormField } from '@nuxt/ui/composables/useFormField'
import { computed, defineComponent, type SlotsType } from 'vue'

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
  props: { inputId: { type: String, required: true } },
  slots: Object as SlotsType<{
    default: { describedBy: string | undefined; invalid: boolean }
  }>,
  setup(props, { slots }) {
    const { ariaAttrs } = useFormField({ id: props.inputId })
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
 * One form field: label, the hint that states its format between the label and
 * the control, the control, and — while there is one — the error the control
 * caused, under it. Nuxt UI draws the field; the hint is the library's
 * description and stays on screen beside the error. The slot receives the ids
 * to point `aria-describedby` at, so the wiring cannot drift from what is
 * rendered.
 */
const props = defineProps<{
  error?: string
  hint?: string
  inputId: string
  label: string
}>()

const failure = computed(() =>
  props.error === undefined || props.error === '' ? undefined : props.error,
)
const description = computed(() => (props.hint === '' ? undefined : props.hint))
</script>

<template>
  <UFormField :description="description" :error="failure" :label="label">
    <!-- A new id is a new registration, so the binding is keyed by it. -->
    <FieldBinding :key="inputId" v-slot="binding" :input-id="inputId">
      <slot :described-by="binding.describedBy" :invalid="binding.invalid" />
    </FieldBinding>
  </UFormField>
</template>
