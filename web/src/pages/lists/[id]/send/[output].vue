<script setup lang="ts">
import { computed, onMounted } from 'vue'

import SendPanel from '@/features/send-artifact/ui/SendPanel.vue'
import { useListView } from '@/features/view-list/model/useListView'
import { useLocale } from '@/shared/i18n/useLocale'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'
import RvButton from '@/shared/ui/RvButton.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

const route = useRoute()
const { t } = useLocale()

const listId = computed(() => {
  const value = route.params.id
  return typeof value === 'string' ? value : ''
})
const outputId = computed(() => {
  const value = route.params.output
  return typeof value === 'string' ? value : ''
})

const view = useListView(() => listId.value)

const output = computed(
  () => view.outputs.value.find((entry) => entry.id === outputId.value) ?? null,
)
const target = computed(
  () =>
    view.catalog.value?.targets.find(
      (entry) => entry.id === output.value?.targetID,
    ) ?? null,
)

useHead({
  title: computed(
    () => `${t('shell.product')} · ${t('send.title').toLowerCase()}`,
  ),
})

async function reload(): Promise<void> {
  await view.initialize()
  if (view.state.value === 'ready' && outputId.value !== '')
    view.selectOutput(outputId.value)
}

onMounted(reload)
</script>

<template>
  <AppShell>
    <section
      v-if="
        view.state.value !== 'ready' ||
        output === null ||
        output.latest === null
      "
      aria-labelledby="send-entry-title"
      class="send-entry"
    >
      <h1 id="send-entry-title" class="send-entry__title">
        {{ t('send.title') }}
      </h1>
      <RvStateNotice
        v-if="view.state.value === 'loading'"
        live
        :title="t('list.loading')"
        tone="busy"
      />
      <RvStateNotice
        v-else-if="view.state.value === 'failed'"
        :body="t('send.route.failed.body')"
        live
        :title="t('send.route.failed')"
        tone="failed"
      >
        <template #action>
          <RvButton @click="reload">{{ t('action.retry') }}</RvButton>
        </template>
      </RvStateNotice>
      <RvStateNotice
        v-else-if="view.state.value === 'missing'"
        :body="t('list.missing.body')"
        live
        :title="t('list.missing')"
        tone="failed"
      >
        <template #action>
          <RvButton to="/" variant="secondary">{{ t('action.back') }}</RvButton>
        </template>
      </RvStateNotice>
      <RvStateNotice
        v-else-if="output === null"
        :body="t('send.connection.missing.body')"
        live
        :title="t('send.connection.missing')"
        tone="failed"
      >
        <template #action>
          <RvButton :to="`/lists/${listId}`" variant="secondary">{{
            t('send.back')
          }}</RvButton>
        </template>
      </RvStateNotice>
      <RvStateNotice v-else :title="t('library.noArtifact')" tone="waiting">
        <template #action>
          <RvButton :to="`/lists/${listId}`" variant="secondary">
            {{ t('send.back') }}
          </RvButton>
        </template>
      </RvStateNotice>
    </section>
    <SendPanel
      v-else-if="output !== null && output.latest !== null"
      :artifact-id="output.latest.id"
      :list-id="listId"
      :list-name="view.list.value?.name ?? ''"
      :target="target"
      :target-id="output.targetID"
    />
  </AppShell>
</template>

<style scoped>
.send-entry {
  display: grid;
  gap: var(--rv-space-6);
}

.send-entry__title {
  font-weight: 700;
  font-size: var(--rv-text-page);
  line-height: var(--rv-leading-tight);
}
</style>
