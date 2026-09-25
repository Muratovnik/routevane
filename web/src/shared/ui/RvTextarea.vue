<script setup lang="ts">
/**
 * A multi-line text control, drawn by Nuxt UI at the standard field size.
 *
 * Its standing height is a number of rows rather than a length: nine rows of
 * the field's own line are the working area an entry editor had as a minimum
 * height, so pasting a list does not start in a slot. `mono` is for contents
 * compared character by character — domains, addresses, a list's entries.
 */
defineProps<{
  describedBy?: string
  disabled?: boolean
  inputId: string
  invalid?: boolean
  mono?: boolean
  rows?: number
}>()

const model = defineModel<string>({ required: true })
</script>

<template>
  <UTextarea
    :id="inputId"
    v-model="model"
    class="rv-textarea"
    :class="{ 'rv-mono': mono === true }"
    :aria-describedby="describedBy"
    :aria-invalid="invalid === true ? 'true' : undefined"
    :color="invalid === true ? 'error' : 'primary'"
    :disabled="disabled === true"
    :highlight="invalid === true"
    :rows="rows ?? 9"
    size="xl"
    spellcheck="false"
    variant="outline"
  />
</template>

<!-- Layout of the root only: the library's root is an inline box, and the
     editor fills the column it is given. -->
<style scoped>
.rv-textarea {
  display: flex;
  min-width: 0;
}
</style>
