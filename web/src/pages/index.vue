<script setup lang="ts">
import { computed } from 'vue'

import LibraryView from '@/features/library/ui/LibraryView.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'

const { t } = useLocale()

useHead({
  title: computed(
    () => `${t('shell.product')} · ${t('library.title').toLowerCase()}`,
  ),
})
</script>

<template>
  <AppShell>
    <LibraryView />
  </AppShell>
</template>
