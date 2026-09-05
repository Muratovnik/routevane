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

.rv-composer-form--compact {
  gap: var(--rv-space-3);
}

.rv-composer-form--compact .rv-composer-form__fields {
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--rv-space-4);
}

.rv-composer-form--compact .rv-composer-form__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
}
</style>
