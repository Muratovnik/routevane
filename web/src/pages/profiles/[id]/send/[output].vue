<script setup lang="ts">
import { computed, onMounted } from 'vue'

import SendPanel from '@/features/send-artifact/ui/SendPanel.vue'
import { useProfileView } from '@/features/view-profile/model/useProfileView'
import { useLocale } from '@/shared/i18n/useLocale'
import { legacyImportedOrdinal } from '@/shared/lib/legacyProfile'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'
import RvButton from '@/shared/ui/RvButton.vue'
import RvPageHeader from '@/shared/ui/RvPageHeader.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

const route = useRoute()
const { t } = useLocale()

const profileId = computed(() => {
  const value = route.params.id
  return typeof value === 'string' ? value : ''
})
const outputId = computed(() => {
  const value = route.params.output
  return typeof value === 'string' ? value : ''
})

const view = useProfileView(() => profileId.value)

const output = computed(
  () => view.outputs.value.find((entry) => entry.id === outputId.value) ?? null,
)
const target = computed(
  () =>
    view.catalog.value?.targets.find(
      (entry) => entry.id === output.value?.targetID,
    ) ?? null,
)

// The way back names the profile as its own page does, an imported one
// included; a profile the page could not read is left out of the trail.
const profileName = computed(() => {
  const name = view.profile.value?.name ?? ''
  const ordinal = legacyImportedOrdinal(name)
  return ordinal === null
    ? name
    : t('profiles.imported.name', { number: ordinal })
})
const breadcrumbs = computed(() => [
  { label: t('profiles.title'), to: '/' },
  ...(profileName.value === ''
    ? []
    : [{ label: profileName.value, to: `/profiles/${profileId.value}` }]),
])
const formatTitle = computed(() =>
  output.value === null ? '' : view.outputTitle(output.value),
)
const heading = computed(() =>
  formatTitle.value === ''
    ? t('send.title')
    : t('send.heading', { format: formatTitle.value }),
)
// The name the format's FQDN groups carry on the router (ADR 0049); empty for a
// format that has none.
const groupPrefix = computed(() =>
  output.value?.targetID === 'keenetic-dns'
    ? output.value.fqdnGroupPrefix || 'routevane'
    : '',
)

useHead({
  title: computed(
    () => `${t('shell.product')} · ${t('send.title').toLowerCase()}`,
  ),
})

const reload = async (): Promise<void> => {
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
      aria-labelledby="send-title"
      class="send-entry"
    >
      <RvPageHeader
        :breadcrumb-label="t('profile.breadcrumb')"
        :breadcrumbs="breadcrumbs"
        :title="heading"
        title-id="send-title"
      />
      <RvStateNotice
        v-if="view.state.value === 'loading'"
        live
        :title="t('profile.loading')"
        tone="busy"
      />
      <RvStateNotice
        v-else-if="view.state.value === 'failed'"
        :body="t('send.profile.failed.body')"
        live
        :title="t('send.profile.failed')"
        tone="failed"
      >
        <template #action>
          <RvButton @click="reload">{{ t('action.retry') }}</RvButton>
        </template>
      </RvStateNotice>
      <RvStateNotice
        v-else-if="view.state.value === 'missing'"
        :body="t('profile.missing.body')"
        live
        :title="t('profile.missing')"
        tone="failed"
      />
      <RvStateNotice
        v-else-if="output === null"
        :body="t('send.connection.missing.body')"
        live
        :title="t('send.connection.missing')"
        tone="failed"
      />
      <RvStateNotice v-else :title="t('profiles.noArtifact')" tone="waiting" />
    </section>
    <SendPanel
      v-else-if="output !== null && output.latest !== null"
      :key="`${profileId}:${output.id}:${output.targetID}:${output.latest.id}`"
      :artifact-id="output.latest.id"
      :breadcrumbs="breadcrumbs"
      :group-prefix="groupPrefix"
      :heading="heading"
      :preferred-device-id="output.deviceID"
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
</style>
