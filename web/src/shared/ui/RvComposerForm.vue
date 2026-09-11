<script setup lang="ts">
defineProps<{ compact?: boolean }>()
</script>

<template>
  <form
    class="rv-composer-form"
    :class="{ 'rv-composer-form--compact': compact }"
  >
    <div class="rv-composer-form__fields"><slot /></div>
    <div v-if="$slots.details" class="rv-composer-form__details">
      <slot name="details" />
    </div>
    <div class="rv-composer-form__actions"><slot name="actions" /></div>
  </form>
</template>

<style scoped>
.rv-composer-form {
  display: grid;
  gap: var(--rv-space-6);
  min-width: 0;
}

.rv-composer-form__fields {
  display: grid;
  gap: var(--rv-space-6);
  align-items: start;
  min-width: 0;
}

.rv-composer-form__actions {
  display: grid;
  gap: var(--rv-space-3);
  min-width: 0;
}

/* Docked, the form is a band across the top of the workspace rather than a
   rail beside it. Its fields stand side by side and take the whole band, each
   one no narrower than a field may be drawn, so the band reads as a row of
   named things rather than as a column squeezed sideways. */
.rv-composer-form--compact {
  gap: var(--rv-space-5);
}

.rv-composer-form--compact .rv-composer-form__fields {
  grid-template-columns: repeat(
    auto-fit,
    minmax(min(100%, var(--rv-form-column-min)), 1fr)
  );
  gap: var(--rv-space-4) var(--rv-space-6);
}

/* The acts that end the edit stand together at the band's end, on one line and
   in the order they are taken: leave the draft, then commit it. */
.rv-composer-form--compact .rv-composer-form__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: var(--rv-space-3);
}
</style>
