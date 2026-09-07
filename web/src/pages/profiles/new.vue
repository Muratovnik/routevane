<script setup lang="ts">
import { computed } from 'vue'

import CreateProfile from '@/features/create-profile/ui/CreateProfile.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import { profilePageHash } from '@/shared/lib/profileHash'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'

const router = useRouter()
const { t } = useLocale()

useHead({
  title: computed(
    () => `${t('shell.product')} · ${t('create.title').toLowerCase()}`,
  ),
})

function onCreated(profileID: string, targetID: string): void {
  void router.push(
    `/profiles/${profileID}${profilePageHash({ setup: targetID, tab: 'outputs' })}`,
  )
}
</script>

<template>
  <AppShell>
    <CreateProfile @created="onCreated" />
  </AppShell>
</template>
