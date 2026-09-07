<script setup lang="ts">
import { computed } from 'vue'

import ProfileView from '@/features/view-profile/ui/ProfileView.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'

const route = useRoute()
const { t } = useLocale()

const profileId = computed(() => {
  const value = route.params.id
  return typeof value === 'string' ? value : ''
})

useHead({
  title: computed(
    () => `${t('shell.product')} · ${t('shell.nav.profiles').toLowerCase()}`,
  ),
})
</script>

<template>
  <AppShell>
    <ProfileView :profile-id="profileId" />
  </AppShell>
</template>
