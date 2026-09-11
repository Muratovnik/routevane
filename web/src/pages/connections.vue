<script setup lang="ts">
import { computed } from 'vue'

import DevicesView from '@/features/devices/ui/DevicesView.vue'
import TargetsView from '@/features/targets-overview/ui/TargetsView.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'

const { t } = useLocale()

useHead({
  title: computed(
    () => `${t('shell.product')} · ${t('connections.title').toLowerCase()}`,
  ),
})
</script>

<template>
  <AppShell>
    <!-- The connections this operator made come first. What this build can
         talk to at all is reference material behind one click, and it states
         its own summary from the catalog it reads. -->
    <DevicesView />
    <TargetsView />
  </AppShell>
</template>
