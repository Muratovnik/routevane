<script setup lang="ts">
import { computed } from 'vue'

import DevicesView from '@/features/devices/ui/DevicesView.vue'
import TargetsView from '@/features/targets-overview/ui/TargetsView.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'
import RvDisclosure from '@/shared/ui/RvDisclosure.vue'

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
         talk to at all is reference material behind one click, not a second
         subject competing with them. -->
    <DevicesView />
    <RvDisclosure borderless :summary="t('targets.title')">
      <TargetsView />
    </RvDisclosure>
  </AppShell>
</template>
