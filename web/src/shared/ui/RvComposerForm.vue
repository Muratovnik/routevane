<template>
  <form class="rv-composer-form">
    <div class="rv-composer-form__body">
      <div class="rv-composer-form__fields"><slot /></div>
      <div v-if="$slots.details" class="rv-composer-form__details">
        <slot name="details" />
      </div>
    </div>
    <div class="rv-composer-form__actions"><slot name="actions" /></div>
  </form>
</template>

<style scoped>
.rv-composer-form {
  display: flex;
  flex-direction: column;
  gap: var(--rv-space-5);
  min-width: 0;
  min-height: 0;
}

/* The fields and what is said about them are the part of the form that gives
   way: a host that caps the form's height, as the settings rail does to keep
   itself inside the window, scrolls this part and leaves the actions where
   they are. Without a cap it has its natural height and never scrolls. The
   inset keeps a focus ring or an error from being cut at the scroll edge. */
.rv-composer-form__body {
  display: grid;
  gap: var(--rv-space-5);
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  margin: calc(var(--rv-space-1) * -1);
  padding: var(--rv-space-1);
  overflow: hidden auto;
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
   commit it. A note that explains them takes the whole line above.

   They are also pinned to the bottom of the window while the form is partly
   below it, so the act that stores the draft is in reach without scrolling the
   page. The bar's own ground covers what passes under it, and the negative
   margin gives back the space its padding takes where the form ends in the
   window, so the bar sits where the unpinned row would. */
.rv-composer-form__actions {
  position: sticky;
  bottom: var(--rv-space-4);
  display: flex;
  flex: none;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: var(--rv-space-3);
  min-width: 0;
  margin-bottom: calc(var(--rv-space-4) * -1);
  padding-block: var(--rv-space-3) var(--rv-space-4);
  background: var(--rv-color-surface);
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}
</style>
