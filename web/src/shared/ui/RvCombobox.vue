<script setup lang="ts">
import type { ChoiceGroup, ChoiceOption } from '@/shared/ui/types'
import RvSearchSelect from '@/shared/ui/RvSearchSelect.vue'
defineProps<{
  describedBy?: string
  disabled?: boolean
  /** What to say when the query matches nothing. One line, not a lesson. */
  emptyLabel: string
  groups?: ChoiceGroup[]
  inputId: string
  invalid?: boolean
  loading?: boolean
  loadingLabel?: string
  options?: ChoiceOption[]
  placeholder: string
  /** Accessible name of the control that opens the list from the field. */
  toggleLabel: string
}>()
const model = defineModel<string>({ required: true })
</script>
<template>
  <RvSearchSelect v-bind="$props" v-model="model" size="default" />
</template>
