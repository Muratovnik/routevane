<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { loadRuntimeVersion } from '@/shared/api/runtime'
import { useLocale } from '@/shared/i18n/useLocale'

defineProps<{ collapsed?: boolean }>()
const { t } = useLocale()
const version = ref<string | null>(null)

onMounted(async () => {
  try {
    version.value = await loadRuntimeVersion()
  } catch {
    // An unavailable or older service does not establish a version.
  }
})
</script>

<template>
  <p v-if="version !== null" class="application-version">
    <span :class="{ 'application-version__label--hidden': collapsed }">
      {{ t('shell.version', { version }) }}
    </span>
    <span v-if="collapsed" aria-hidden="true">{{ version }}</span>
  </p>
</template>

<style scoped>
.application-version {
  padding-inline: var(--rv-space-3);
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}

.application-version__label--hidden {
  position: absolute;
  width: var(--rv-border-hair);
  height: var(--rv-border-hair);
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}
</style>
