<script setup lang="ts">
import { computed } from 'vue'

import CreateList from '@/features/create-list/ui/CreateList.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import { listPageHash } from '@/shared/lib/listHash'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'

const router = useRouter()
const { t } = useLocale()

useHead({
  title: computed(
    () => `${t('shell.product')} · ${t('create.title').toLowerCase()}`,
  ),
})

function onCreated(listID: string, targetID: string): void {
  void router.push(
    `/profiles/${listID}${listPageHash({ setup: targetID, tab: 'outputs' })}`,
  )
}
</script>

<template>
  <AppShell>
    <CreateList @created="onCreated" />
  </AppShell>
</template>
