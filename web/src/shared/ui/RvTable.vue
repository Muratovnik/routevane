<script setup lang="ts">
/** Native table semantics with one surface and scroll owner. Slots contain
 * thead/tbody/tfoot; column widths and responsive rows belong to the caller.
 * CSS properties control layout, minimum width, display and cell density. */
defineProps<{ dense?: boolean; stickyHeader?: boolean }>()
</script>

<template>
  <div
    class="rv-table-frame"
    :class="{
      'rv-table-frame--dense': dense,
      'rv-table-frame--sticky': stickyHeader,
    }"
  >
    <table class="rv-table">
      <slot />
    </table>
    <slot name="after" />
  </div>
</template>

<style scoped>
.rv-table-frame {
  position: relative;
  min-width: 0;
  overflow: auto;
  background: var(--rv-color-canvas);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-lg);
  overscroll-behavior: contain;
}

.rv-table {
  display: var(--rv-table-display, table);
  width: 100%;
  min-width: var(--rv-table-min-width, 0);
  table-layout: var(--rv-table-layout, auto);
  border-collapse: collapse;
  font-size: var(--rv-table-font-size, var(--rv-text-interface));
}

:slotted(thead) {
  background: var(--rv-color-surface);
}

.rv-table-frame--sticky :slotted(thead) {
  position: sticky;
  top: 0;
  z-index: 1;
}

:slotted(thead) :where(th) {
  height: var(--rv-table-cell-height, auto);
  padding: var(--rv-table-cell-padding, var(--rv-space-3) var(--rv-space-4));
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  font-weight: 600;
  text-align: start;
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

:slotted(tbody) :where(td, th) {
  font-weight: 400;
  height: var(--rv-table-cell-height, auto);
  padding: var(--rv-table-cell-padding, var(--rv-space-3) var(--rv-space-4));
  text-align: start;
  vertical-align: middle;
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

/* Density is inherited so caller-owned utility cells can still set padding. */
.rv-table-frame--dense {
  --rv-table-cell-height: var(--rv-control-default);
  --rv-table-cell-padding: 0 var(--rv-space-3);
  --rv-table-font-size: var(--rv-text-dense);
}

:slotted(tbody) :where(tr:last-child > td, tr:last-child > th) {
  border-bottom: 0;
}
</style>
