<template>
  <form class="rv-composer-form">
    <div class="rv-composer-form__rail">
      <!-- What the draft says about itself and what stands in the way of the
           act: the host's own live regions, present from the first draw and
           taking no room until they hold something. -->
      <div class="rv-composer-form__status"><slot name="status" /></div>
      <div class="rv-composer-form__actions"><slot name="actions" /></div>
    </div>
    <div class="rv-composer-form__body">
      <div class="rv-composer-form__fields"><slot /></div>
    </div>
  </form>
</template>

<style scoped>
.rv-composer-form {
  display: flex;
  flex-direction: column;
  gap: var(--rv-space-4);
  min-width: 0;
  min-height: 0;
}

/* The fields and what is said about them are the part of the form that gives
   way: a host that caps the form's height, as the settings rail does to keep
   itself inside the window, scrolls this part and leaves the actions where
   they are, above it. Without a cap it has its natural height and never scrolls. The
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

/* The act on the draft comes first, standing alone and as wide as the rail:
   the primary action is full width, and wherever the rail starts in the window
   it is the first thing it shows, with nothing of the fields able to pass under
   it. What the draft says about itself, leaving it and what stands in its way
   take a line above it, and only while there is something to say: with none,
   the rail is the button and nothing else. A hairline sets the block apart from
   the fields below. */
.rv-composer-form__rail {
  display: flex;
  flex: none;
  flex-direction: column;
  min-width: 0;
  padding-bottom: var(--rv-space-4);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.rv-composer-form__status {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: var(--rv-space-2) var(--rv-space-3);
  min-width: 0;
}

/* A live region that holds nothing has no height, so it adds no band. The line
   gets its distance from the button only once a child of it has content. */
.rv-composer-form__status:has(> :not(:empty)) {
  margin-bottom: var(--rv-space-3);
}

.rv-composer-form__actions {
  display: grid;
  gap: var(--rv-space-3);
  min-width: 0;
}
</style>
