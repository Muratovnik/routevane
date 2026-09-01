<script setup lang="ts">
import { computed } from 'vue'

import SettingsView from '@/features/settings/ui/SettingsView.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'

const { t } = useLocale()

useHead({
  title: computed(
    () => `${t('shell.product')} · ${t('settings.title').toLowerCase()}`,
  ),
})
</script>

<template>
  <AppShell>
    <SettingsView />
  </AppShell>
</template>
