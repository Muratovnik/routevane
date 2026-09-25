<script setup lang="ts">
import { h, useTemplateRef, type FunctionalComponent } from 'vue'

import RvIcon from '@/shared/ui/RvIcon.vue'

/**
 * A single-line text control. Nuxt UI draws the field — its ground, edge,
 * placeholder, disabled look and focus ring — at the standard field size; this
 * facade chooses that size and the states Routevane asks of a field.
 *
 * `mono` is for a value an operator compares character by character — an
 * address, an interface name, an identifier. A name or a search is read as
 * prose and keeps the interface face.
 *
 * `inputId` is what a field label points at. A search box has no visible
 * label and is named by an `aria-label` passed through instead.
 */
const props = defineProps<{
  autocomplete?: string
  describedBy?: string
  disabled?: boolean
  inputId?: string
  invalid?: boolean
  mono?: boolean
  placeholder?: string
  type?: 'text' | 'password' | 'search'
}>()

const model = defineModel<string>({ required: true })

// The library draws a leading icon in its own place and colour when it is
// handed one; a component rather than an icon-set name keeps it this
// product's glyph and keeps the library from fetching or injecting a set.
const searchGlyph: FunctionalComponent = () => h(RvIcon, { name: 'search' })

const field = useTemplateRef<{ inputRef: HTMLInputElement | null }>('field')

defineExpose({
  /** The input itself, for a host that moves focus into the field. */
  element: (): HTMLInputElement | null => field.value?.inputRef ?? null,
})
</script>

<template>
  <!-- An invalid value asks the library for its error ring, the same state a
       library form field would set: the colour at rest and on focus. -->
  <UInput
    :id="props.inputId"
    ref="field"
    v-model="model"
    class="rv-text-input"
    :class="{ 'rv-mono': mono === true }"
    :aria-describedby="describedBy"
    :aria-invalid="invalid === true ? 'true' : undefined"
    :autocomplete="autocomplete ?? 'off'"
    :color="invalid === true ? 'error' : 'primary'"
    :disabled="disabled === true"
    :highlight="invalid === true"
    :placeholder="placeholder"
    size="xl"
    spellcheck="false"
    :type="type ?? 'text'"
    variant="outline"
    :leading-icon="type === 'search' ? searchGlyph : undefined"
  />
</template>

<!-- Layout of the root only. The library's root is an inline box, which would
     shrink a field to its text; a field fills the column it is given, and a
     host that wants it narrower sets a width on its own class. -->
<style scoped>
.rv-text-input {
  display: flex;
  min-width: 0;
}
</style>
