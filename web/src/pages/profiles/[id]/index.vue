<script setup lang="ts">
import { computed } from 'vue'

import SendPanel from '@/features/send-artifact/ui/SendPanel.vue'
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
    <!-- The profile is where a format is sent from: the send panel opens over
         it, and a connection still to be made is made on its own page. -->
    <ProfileView :profile-id="profileId">
      <template #send="{ context, applied, busy }">
        <SendPanel
          :key="`${context.outputID}:${context.artifactID}`"
          :artifact-id="context.artifactID"
          embedded
          :group-prefix="context.groupPrefix"
          :heading="context.heading"
          :preferred-device-id="context.preferredDeviceID"
          :target="context.target"
          :target-id="context.targetID"
          @add-connection="navigateTo('/connections')"
          @applied="applied"
          @busy="busy"
        />
      </template>
    </ProfileView>
  </AppShell>
</template>
