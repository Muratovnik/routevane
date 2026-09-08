<script setup lang="ts">
import { useId } from 'vue'

defineProps<{ title: string; description?: string }>()
const headingID = useId()
</script>

<template>
  <section class="rv-settings-section" :aria-labelledby="headingID">
    <header class="rv-settings-section__heading">
      <h2 :id="headingID">{{ title }}</h2>
      <p v-if="description">{{ description }}</p>
    </header>
    <div class="rv-settings-section__content"><slot /></div>
  </section>
</template>

<style scoped>
.rv-settings-section {
  display: grid;
  grid-template-columns: var(--rv-settings-heading-width) minmax(0, 1fr);
  align-items: start;
  gap: var(--rv-space-8);
  padding: var(--rv-space-6);
  background: var(--rv-color-surface);
  border-radius: var(--rv-radius-lg);
}

.rv-settings-section__heading,
.rv-settings-section__content {
  display: grid;
  gap: var(--rv-space-3);
  min-width: 0;
}

.rv-settings-section__heading h2 {
  font-size: var(--rv-text-section);
}

.rv-settings-section__heading p {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

@container settings-content (width < 48rem) {
  .rv-settings-section {
    grid-template-columns: minmax(0, 1fr);
    gap: var(--rv-space-6);
  }
}
</style>
