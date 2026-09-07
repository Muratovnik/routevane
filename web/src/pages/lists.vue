<script setup lang="ts">
import { computed } from 'vue'

import ListsView from '@/features/lists/ui/ListsView.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'

const { t } = useLocale()

useHead({
  title: computed(
    () => `${t('shell.product')} · ${t('profiles.title').toLowerCase()}`,
  ),
})
</script>

<template>
  <AppShell>
    <ListsView />
  </AppShell>
</template>
