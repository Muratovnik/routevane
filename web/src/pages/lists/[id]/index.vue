<script setup lang="ts">
import { computed } from 'vue'

import ListView from '@/features/view-list/ui/ListView.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'

const route = useRoute()
const { t } = useLocale()

const listId = computed(() => {
  const value = route.params.id
  return typeof value === 'string' ? value : ''
})

useHead({
  title: computed(
    () => `${t('shell.product')} · ${t('shell.nav.library').toLowerCase()}`,
  ),
})
</script>

<template>
  <AppShell>
    <ListView :list-id="listId" />
  </AppShell>
</template>
