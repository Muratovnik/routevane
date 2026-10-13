<template>
  <form class="rv-composer-form">
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
  gap: var(--rv-space-5);
  min-width: 0;
}

/* The fields take as many columns as the form has room for, each no narrower
   than a field may be drawn. The rail beside the table holds one; the band
   across the top of a stacked or docked layout holds two or more, and nothing
   here has to be told which of the two it is standing in. */
.rv-composer-form__fields {
  display: grid;
  grid-template-columns: repeat(
    auto-fit,
    minmax(min(100%, var(--rv-form-column-min)), 1fr)
  );
  gap: var(--rv-space-4) var(--rv-space-6);
  align-items: start;
  min-width: 0;
}

/* The acts that end the edit stand together at the form's end, on one line
   where there is room and in the order they are taken: leave the draft, then
   commit it. A note that explains them takes the whole line above. */
.rv-composer-form__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: var(--rv-space-3);
  min-width: 0;
}
</style>
