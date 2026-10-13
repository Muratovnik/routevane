<script setup lang="ts">
import { computed } from 'vue'

import DevicesView from '@/features/devices/ui/DevicesView.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import {
  connectionPageHash,
  parseConnectionPageHash,
} from '@/shared/lib/connectionHash'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'

const { t } = useLocale()
const route = useRoute()
const router = useRouter()
// The connection a link names, read once on arrival. Choosing another one
// moves the address with it, so a refresh stays on what is on screen.
const named = parseConnectionPageHash(route.hash)
const follow = (id: string): void => {
  void router.replace({ hash: connectionPageHash(id) })
}

useHead({
  title: computed(
    () => `${t('shell.product')} · ${t('connections.title').toLowerCase()}`,
  ),
})
</script>

<template>
  <AppShell>
    <!-- The connections this operator made come first. What this build can
         talk to at all is reference material behind one click inside the same
         section, so the catalog is read once and every part states the same
         answer. -->
    <DevicesView :initial-connection="named" @select="follow" />
  </AppShell>
</template>
