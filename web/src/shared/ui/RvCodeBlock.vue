<script setup lang="ts">
/**
 * The exact bytes, shown as text.
 *
 * Trust in a generated file comes from reading it, so this block never
 * summarises, truncates or prettifies: it scrolls inside its own box and the
 * page around it does not move.
 */
defineProps<{
  caption: string
  text: string
}>()
</script>

<template>
  <figure class="rv-code">
    <figcaption class="rv-code__caption">
      <span>{{ caption }}</span>
      <slot name="action" />
    </figcaption>
    <pre class="rv-code__body" tabindex="0"><code>{{ text }}</code></pre>
  </figure>
</template>

<style scoped>
.rv-code {
  display: grid;
  gap: var(--rv-space-3);
  min-width: 0;
  margin: 0;
}

.rv-code__caption {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  align-items: center;
  justify-content: space-between;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.rv-code__body {
  max-height: 20rem;
  margin: 0;
  padding: var(--rv-space-4);
  overflow: auto;
  font-size: var(--rv-text-dense);
  font-family: var(--rv-font-mono);
  line-height: 1.6;
  background: var(--rv-color-canvas);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-sm);
  tab-size: 4;
}
</style>
