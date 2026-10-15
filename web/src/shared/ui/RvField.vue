<script lang="ts">
import { useFormField } from '@nuxt/ui/composables/useFormField'
import {
  computed,
  defineComponent,
  provide,
  useSlots,
  type SlotsType,
} from 'vue'

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
 * One form field: label, the control, and — only while there is one — the error
 * the control caused, under it. Nuxt UI draws the field. An example of the format
 * belongs in the control's placeholder and an explanation in `info`, which
 * opens from an informer in the label's row; neither is a line under the label.
 * The explanation is also the control's description: its text is kept in the
 * field, visually hidden, and named in the `describedBy` the slot receives
 * beside the error, so a reader hears it without opening the informer. The
 * `label-aside` slot is the label row's own place for any other small control.
 * The slot receives the ids to point `aria-describedby` at, so the wiring
 * cannot drift from what is rendered.
 *
 * The field keeps no line for the error. The error is drawn when it exists, at
 * the width of the field and wrapping as any text does, and what is under the
 * field moves down by what it takes. The one standing exception is the live
 * region of an announced error, which is empty and takes no room until it holds
 * a text.
 */
const props = defineProps<{
  /** Share caption and control rows with adjacent fields in a parent grid. */
  aligned?: boolean
  /** Announce an asynchronous error while focus remains on another control. */
  announceError?: boolean
  error?: string
  /** What the field means or how it is read, offered from an informer. */
  info?: string
  /**
   * The informer's name, when a form holds more than one and a reader must
   * tell them apart. It names the explanation, never repeats the label.
   */
  infoLabel?: string
  inputId: string
  label: string
}>()

const { t } = useLocale()
const slots = useSlots()

// The informer's text is kept as the control's description, whether or not its
// panel is open, under an id the caller's own id fixes.
const infoId = computed(() => `${props.inputId}-info`)
const describedByWith = (ids: string | undefined): string | undefined => {
  const named = [props.info === undefined ? undefined : infoId.value, ids]
  return (
    named.filter((id) => id !== undefined && id !== '').join(' ') || undefined
  )
}

const failure = computed(() =>
  props.error === undefined || props.error === '' ? undefined : props.error,
)
</script>

<template>
  <UFormField
    :class="{ 'rv-field--aligned': aligned }"
    :error="failure"
    :label="label"
    :ui="{ error: 'rv-field__error-slot', labelWrapper: 'rv-field__label-row' }"
  >
    <!-- The label's text carries the id a choice button names itself by. -->
    <template #label>
      <span :id="fieldLabelId(inputId)">{{ label }}</span>
    </template>
    <!-- In the label's row and outside the label, so the label's own text stays
         what names the control. The informer is named by what it is, never by
         the label, so a label query finds the field alone. -->
    <template v-if="info !== undefined || slots['label-aside']" #hint>
      <template v-if="info !== undefined">
        <span :id="infoId" class="rv-visually-hidden">{{ info }}</span>
        <RvInfoTip inline :label="infoLabel ?? t('field.info')" :text="info" />
      </template>
      <slot name="label-aside" />
    </template>
    <!-- A new id is a new registration, so the binding is keyed by it. -->
    <FieldBinding
      :key="inputId"
      v-slot="binding"
      :input-id="inputId"
      :label="label"
    >
      <slot
        :described-by="describedByWith(binding.describedBy)"
        :invalid="binding.invalid"
      />
    </FieldBinding>
    <!-- The library draws the error's element whenever this slot exists, so the
         slot is given only while there is an error to show. An announced error
         is the exception: it is a live region that stands from the moment the
         field is drawn and only receives its text, because a region that
         arrives together with its text is not reliably announced. Empty, it
         takes no height and no gap. -->
    <template v-if="announceError === true || failure !== undefined" #error>
      <span
        class="rv-field__error"
        :class="{ 'rv-field__error--shown': failure !== undefined }"
        :role="announceError === true ? 'alert' : undefined"
        >{{ failure }}</span
      >
    </template>
  </UFormField>
</template>

<style scoped>
/* The error takes the field's width and wraps inside it. An unbreakable token
   is cut at the edge rather than widening the field, and the form's column
   with it. */
.rv-field__error {
  display: block;
  overflow-wrap: anywhere;
}

.rv-field__error--shown {
  margin-block-start: var(--rv-space-1);
}

.rv-field--aligned {
  display: grid;
  grid-row: span 2;
  grid-template-rows: subgrid;
  row-gap: 0;
  align-self: stretch;
  min-width: 0;
}
</style>

<!-- The label row and the error's holder are drawn by the library, outside this
     component's scope, so they carry names of their own. The holder has no
     margin of its own: the error brings its gap with it, and a live region
     with nothing in it leaves none. -->
<style>
.rv-field__label-row {
  justify-content: flex-start;
}

.rv-field__error-slot {
  margin-block-start: 0;
}
</style>
