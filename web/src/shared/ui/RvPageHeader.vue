<script setup lang="ts">
defineProps<{ title: string; titleId: string }>()
</script>

<template>
  <header class="rv-page-header">
    <h1 :id="titleId" class="rv-page-header__title">{{ title }}</h1>
    <div v-if="$slots.default" class="rv-page-header__actions"><slot /></div>
  </header>
</template>

<style scoped>
.rv-page-header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--rv-space-4);
  min-height: var(--rv-control-touch);
}

.rv-page-header__title {
  font-size: var(--rv-text-page);
  line-height: var(--rv-leading-tight);
  letter-spacing: var(--rv-tracking-title);
}

.rv-page-header__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--rv-space-3);
}
</style>
