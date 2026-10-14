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
  placeholder?: string
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
    :placeholder="placeholder"
    :rows="rows ?? 9"
    size="xl"
    spellcheck="false"
    variant="outline"
  />
</template>

<!-- Layout of the root, and the unavailable look of the editor it draws: the
     library's root is an inline box, and the editor fills the column it is
     given. An unavailable editor is flat and dimmer, drawn with the faint rule,
     as an unavailable button is; the library's own lowered opacity is replaced
     by an unlayered rule that outranks its utilities. -->
<style scoped>
.rv-textarea {
  display: flex;
  min-width: 0;
}

.rv-textarea :deep(textarea:disabled) {
  color: var(--rv-color-ink-disabled);
  background: transparent;
  box-shadow: var(--rv-edge-disabled);
  opacity: 1;
  cursor: not-allowed;
}

.rv-textarea :deep(textarea:disabled::placeholder) {
  color: var(--rv-color-ink-disabled);
}
</style>
