<script setup lang="ts">
import { computed } from 'vue'

import ProfilesView from '@/features/profiles/ui/ProfilesView.vue'
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
    <ProfilesView />
  </AppShell>
</template>
