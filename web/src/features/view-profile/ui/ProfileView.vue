<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'

import DiagnosticsPanel from '@/features/view-profile/ui/DiagnosticsPanel.vue'
import OutputConfirmDialog from '@/features/view-profile/ui/OutputConfirmDialog.vue'
import ProfileEditor from '@/features/view-profile/ui/ProfileEditor.vue'
import UncoveredLists from '@/features/view-profile/ui/UncoveredLists.vue'
import OutputsPanel from '@/features/view-profile/ui/OutputsPanel.vue'
import { useProfileView } from '@/features/view-profile/model/useProfileView'
import { useLocale } from '@/shared/i18n/useLocale'
import type { ExportFormat } from '@/shared/api/exports'
import { legacyImportedOrdinal } from '@/shared/lib/legacyProfile'
import { targetIcon } from '@/shared/lib/targetIcon'
import { profilePageHash, parseProfilePageHash } from '@/shared/lib/profileHash'
import { useSurfacePreferences } from '@/shared/model/useSurfacePreferences'
import type { ProfileComposition } from '@/shared/api/profiles'
import type {
  ChoiceOption,
  Fact,
  MenuItem,
  StatusTone,
  TabItem,
} from '@/shared/ui/types'
import RvButton from '@/shared/ui/RvButton.vue'
import RvCodeBlock from '@/shared/ui/RvCodeBlock.vue'
import RvCopyButton from '@/shared/ui/RvCopyButton.vue'
import RvDisclosure from '@/shared/ui/RvDisclosure.vue'
import RvFacts from '@/shared/ui/RvFacts.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import RvMenu from '@/shared/ui/RvMenu.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'
import RvTabs from '@/shared/ui/RvTabs.vue'

const props = defineProps<{
  profileId: string
}>()

const emit = defineEmits<{
  changed: []
}>()

const { dateTime, formatBytes, t, tc, tor } = useLocale()
const { expert } = useSurfacePreferences()
const route = useRoute()
const router = useRouter()
const view = useProfileView(() => props.profileId)
// Whether the operator arrived here from somewhere else in the interface.
// Moving the keyboard to the title belongs to that arrival: a row was pressed
// and the content under their hands was replaced, so the keyboard follows it.
// A document that opened on this address moved nobody — its focus already
// starts at the top — and taking it there only draws a ring around the title
// for a press nobody made. Read once, in setup, because the flag is false by
// the time the profile has been read.
const arrived = useNuxtApp().isHydrating !== true
const pageLocation = computed(() => parseProfilePageHash(route.hash))
const titleID = computed(() => `profile-title-${props.profileId}`)
const heading = ref<HTMLElement | null>(null)
const tabIDs = new Set(['overview', 'outputs', 'file', 'diagnostics'])
const tab = ref(pageLocation.value.tab)

if (!tabIDs.has(tab.value)) tab.value = 'overview'

const tabs = computed<TabItem[]>(() => [
  { id: 'overview', label: t('profile.tab.composition') },
  { id: 'outputs', label: t('profile.tab.outputs') },
  { id: 'file', label: t('profile.tab.file') },
  { id: 'diagnostics', label: t('profile.tab.diagnostics') },
])

const published = computed(() =>
  view.outputs.value.some((output) => output.latest !== null),
)
const hasBuildFailure = computed(() => view.failedOutputs.value.length > 0)
const buildFailure = computed(
  () =>
    (view.selectedAttempt.value?.status === 'failed'
      ? view.selectedAttempt.value
      : view.failedOutputs.value[0]?.lastAttempt) ?? null,
)

// A format that never published has nothing to fall back on: its failure is a
// build that did not happen, not a refresh of something already there.
const firstBuildFailed = computed(
  () =>
    buildFailure.value !== null &&
    !view.outputs.value.some(
      (output) =>
        output.lastAttempt === buildFailure.value && output.latest !== null,
    ),
)

// The header states the files' condition only while something is wrong or
// missing. Every other state — archived, behind the catalog, partly covered,
// degraded sources, a refresh that failed after a publication — has a notice
// of its own right below, and a built, current profile says nothing: «published»
// does not say where or when.
const status = computed<{ label: string; tone: StatusTone } | null>(() => {
  if (view.archived.value) return null
  if (hasBuildFailure.value) {
    return firstBuildFailed.value
      ? { label: t('profiles.buildFailed'), tone: 'failed' }
      : null
  }
  return published.value
    ? null
    : { label: t('profile.status.empty'), tone: 'waiting' }
})

// The block is collapsed unless the link in it has not been shown yet: a link
// just issued or reissued in this tab is readable now and never again, so it
// must not wait behind a click. Collapsing masks the link again.
const linkOpen = ref(false)
watch(
  () => view.subscriptionURL.value,
  (url) => {
    if (url === '' || announcedLinks.has(url)) return
    linkOpen.value = true
  },
  { immediate: true },
)
// A link counts as shown once its block is on the page and open. A page that
// goes away before the profile has loaded has shown nothing, and the next visit
// must still open the block.
const secretBlock = ref<HTMLElement | null>(null)
watch(
  () =>
    [view.subscriptionURL.value, linkOpen.value, secretBlock.value] as const,
  ([url, open, block]) => {
    if (url !== '' && open && block !== null) announcedLinks.add(url)
  },
  { flush: 'post', immediate: true },
)
const onLinkToggle = (open: boolean): void => {
  linkOpen.value = open
  if (!open) view.revealed.value = false
}

// The lists a coverage refusal names. An attempt recorded before the service
// named them has none, and reads as the sentence alone.
const uncoveredLists = computed(() =>
  buildFailure.value?.code === 'partial_coverage'
    ? buildFailure.value.uncoveredLists
    : [],
)

const buildFailureBody = computed(() => {
  const failure = buildFailure.value
  if (failure === null || uncoveredLists.value.length > 0) return ''
  if (
    failure.code === 'rule_limit' &&
    failure.projectedRules > 0 &&
    failure.maximumRules > 0
  ) {
    return t('profile.notice.build.ruleLimit.body', {
      projected: failure.projectedRules,
      maximum: failure.maximumRules,
    })
  }
  return tor(
    `profile.notice.build.${failure.code}.body`,
    t('profile.notice.build.unknown.body'),
  )
})

const facts = computed<Fact[]>(() => {
  const profile = view.profile.value
  if (profile === null) return []
  const items: Fact[] = [
    {
      key: 'lists',
      label: t('profile.facts.lists'),
      value: view.listTitles.value.join(', '),
    },
    {
      key: 'outputs',
      label: t('profile.facts.outputs'),
      value:
        view.outputs.value.length === 0
          ? t('profiles.noOutputs')
          : view.outputs.value.map(view.outputTitle).join(', '),
    },
  ]
  if (profile.archivedAt !== '') {
    items.push({
      key: 'archived',
      label: t('profile.facts.archived'),
      value: formatTime(profile.archivedAt),
    })
  }
  const latest = view.latest.value
  if (latest !== null) {
    items.push({
      key: 'updated',
      label: t('profile.facts.updated'),
      value: formatTime(latest.contentCreatedAt),
    })
  }
  if (view.ruleCount.value > 0) {
    items.push({
      key: 'rules',
      label: t('profile.facts.rules'),
      value: tc('profile.rules', view.ruleCount.value),
    })
  }
  // The rule is stated with where it came from, so "as in settings" is visibly
  // a deferral rather than a value this profile chose.
  const schedule = view.schedule.value
  if (schedule !== null) {
    items.push({
      key: 'schedule',
      label: t('profile.facts.schedule'),
      value: schedule.followsDefault
        ? t('profile.schedule.followsDefault', {
            rule: t(`settings.refresh.${schedule.effective || 'off'}`),
          })
        : t(`settings.refresh.${schedule.effective || 'off'}`),
    })
    if (schedule.lastRefreshedAt !== '') {
      items.push({
        key: 'refreshed',
        label: t('profile.facts.refreshed'),
        value: schedule.lastRefreshFailed
          ? t('profile.schedule.lastFailed', {
              time: formatTime(schedule.lastRefreshedAt),
            })
          : formatTime(schedule.lastRefreshedAt),
      })
    }
  }
  return items
})

const technicalFacts = computed<Fact[]>(() => {
  const items: Fact[] = []
  const latest = view.latest.value
  if (latest !== null) {
    items.push({
      key: 'artifact',
      label: t('profile.technical.artifact'),
      mono: true,
      value: latest.id,
    })
    items.push({
      key: 'size',
      label: t('profile.technical.size'),
      value: formatBytes(latest.sizeBytes),
    })
    if (latest.contentType !== '') {
      items.push({
        key: 'contentType',
        label: t('profile.technical.contentType'),
        mono: true,
        value: latest.contentType,
      })
    }
  }
  if (view.profile.value !== null) {
    items.push({
      key: 'profile',
      label: t('profile.technical.list'),
      mono: true,
      value: view.profile.value.id,
    })
  }
  if (view.snapshotID.value !== '') {
    items.push({
      key: 'snapshot',
      label: t('profile.technical.snapshot'),
      mono: true,
      value: view.snapshotID.value,
    })
  }
  return items
})

const selectedTitle = computed(() => {
  const output = view.selectedOutput.value
  return output === null ? '' : view.outputTitle(output)
})

// A profile publishes the same composition in several formats, and the file and
// the diagnostics belong to exactly one of them. That choice is offered on the
// tab that reads the file rather than left as a marked row on the tab that
// lists the connections — a row painted as chosen on a screen where choosing
// changes nothing visible states a consequence that is not there.
const outputChoices = computed<ChoiceOption[]>(() =>
  view.outputs.value.map((output) => ({
    icon: targetIcon(output.targetID),
    label: view.outputTitle(output),
    value: output.id,
  })),
)
const outputScope = computed<string>({
  get: () => view.selectedOutputID.value,
  set: (value) => view.selectOutput(value),
})
const diagnosticsShown = computed(
  () => view.snapshotID.value !== '' && view.diagnosticsState.value === 'ready',
)
const setupTargetTitle = computed(() =>
  view.targetTitle(pageLocation.value.setup),
)
// The editor asks the server what the draft weighs in the formats this profile
// already publishes, so it takes them already named for this reader.
const editorOutputs = computed(() =>
  view.outputs.value.map((output) => ({
    id: output.id,
    targetID: output.targetID,
    title: view.outputTitle(output),
  })),
)
const importedOrdinal = computed(() =>
  legacyImportedOrdinal(view.profile.value?.name ?? ''),
)
const displayName = computed(() => {
  const ordinal = importedOrdinal.value
  return ordinal === null
    ? (view.profile.value?.name ?? '')
    : t('profiles.imported.name', { number: ordinal })
})

watch(tab, (value) => {
  if (value === 'file') void view.openContent()
  if (value === 'diagnostics') void view.openDiagnostics()
  // A tab the operator chose is a place they can come back from, so it takes a
  // history entry of its own. A tab that the address already names — the
  // browser's Back or Forward moved there — has its entry and adds no second.
  if (pageLocation.value.tab !== value)
    void router.push({ hash: profilePageHash({ tab: value }) })
})

// A restored tab can be active before its output arrives. Detail loading follows
// resource readiness as well as clicks; an invalidated active detail reloads too.
watch(
  [
    view.state,
    view.busy,
    () => view.latest.value?.id,
    view.snapshotID,
    view.contentState,
    view.diagnosticsState,
  ],
  () => {
    if (view.state.value !== 'ready' || view.busy.value) return
    if (tab.value === 'file' && view.contentState.value === 'idle')
      void view.openContent()
    if (tab.value === 'diagnostics' && view.diagnosticsState.value === 'idle')
      void view.openDiagnostics()
  },
)
watch(
  () => pageLocation.value.tab,
  (value) => {
    if (tabIDs.has(value)) tab.value = value
  },
)

onMounted(async () => {
  await view.initialize()
  const setupTarget = pageLocation.value.setup
  if (view.state.value === 'ready' && setupTarget !== '') {
    tab.value = 'outputs'
    const existing = view.outputs.value.find(
      (output) => output.targetID === setupTarget,
    )
    if (existing !== undefined) {
      view.selectOutput(existing.id)
    } else if (
      view.availableTargets.value.some((target) => target.id === setupTarget)
    ) {
      if (await view.bind(setupTarget)) emit('changed')
    }
    await router.replace({ hash: profilePageHash({ tab: tab.value }) })
  }
  if (arrived) heading.value?.focus()
})

const formatTime = (value: string): string => {
  const parsed = new Date(value)
  return Number.isNaN(parsed.valueOf()) ? '—' : dateTime.value.format(parsed)
}

const reasonLabel = (code: string): string =>
  tor(
    `profile.diagnostics.reason.${code}`,
    t('profile.diagnostics.reason.unknown'),
  )

const onSave = async (
  name: string,
  composition: ProfileComposition,
): Promise<void> => {
  if (await view.save(name, composition)) emit('changed')
}

const onBind = async (targetID: string): Promise<void> => {
  if (await view.bind(targetID)) emit('changed')
}

const onArchive = async (next: boolean): Promise<void> => {
  if (await view.setArchived(next)) emit('changed')
}

const exportLabel = (format: ExportFormat): string =>
  tor(`export.format.${format.rendererID}`, format.fileExtension.toUpperCase())

// The same acts, in the same order, as a profile's row on the shelf: one-off
// export, sending each format, adding one, rebuilding, and the archive. An
// archived profile is read and exported, or brought back, and nothing else.
const profileMenuItems = computed<MenuItem[]>(() => {
  const items: MenuItem[] = [
    {
      children: view.exportFormats.value.map((format) => ({
        disabled: view.exporting.value !== '',
        icon: 'download' as const,
        key: `export:${format.id}`,
        label: exportLabel(format),
      })),
      icon: 'download',
      key: 'export',
      label: t('profiles.menu.export'),
    },
  ]
  if (view.archived.value) {
    items.push({
      disabled: view.busy.value,
      icon: 'archive',
      key: 'restore',
      label: t('profiles.restore'),
      separatorBefore: true,
    })
    return items
  }
  const sendItems = view.outputs.value
    .filter(view.deployable)
    .map((output): MenuItem => {
      const device = view.devices.value.find(
        (candidate) => candidate.id === output.deviceID,
      )
      const format = view.outputTitle(output)
      return {
        icon: targetIcon(output.targetID),
        key: `send:${output.id}`,
        label:
          device === undefined
            ? format
            : t('profiles.sendTo', { format, connection: device.name }),
        to: `/profiles/${props.profileId}/send/${output.id}`,
      }
    })
  if (sendItems.length > 0) {
    items.push({
      children: sendItems,
      icon: 'send',
      key: 'send',
      label: t('profiles.menu.send'),
    })
  }
  items.push(
    {
      disabled: view.busy.value || view.targetGroups.value.length === 0,
      icon: 'plus',
      key: 'add-format',
      label: t('profiles.addFormat'),
    },
    {
      disabled: view.busy.value || view.outputs.value.length === 0,
      icon: 'refresh',
      key: 'rebuild',
      label: t('profiles.rebuild'),
    },
    {
      disabled: view.busy.value,
      icon: 'archive',
      key: 'archive',
      label: t('profiles.archive'),
      separatorBefore: true,
    },
  )
  return items
})

const outputsPanel = ref<InstanceType<typeof OutputsPanel> | null>(null)

const onProfileMenu = async (key: string): Promise<void> => {
  if (key.startsWith('export:')) {
    await view.download(key.slice('export:'.length))
    return
  }
  if (key === 'add-format') {
    tab.value = 'outputs'
    await nextTick()
    await outputsPanel.value?.focusAdd()
  }
  if (key === 'rebuild') await onRebuild()
  if (key === 'archive') await onArchive(true)
  if (key === 'restore') await onArchive(false)
}

const onRebuild = async (): Promise<void> => {
  await view.rebuild()
  emit('changed')
}

const onRebuildOutput = async (outputID: string): Promise<void> => {
  if (await view.rebuildOutput(outputID)) emit('changed')
}

// Reissuing a link and removing a format cannot be undone, so each is asked
// once more, with what will happen, and a refusal stays in that question.
const confirming = ref<{
  kind: 'reissue' | 'remove'
  outputID: string
} | null>(null)
const confirmOpen = ref(false)
const confirmError = ref<string | null>(null)
const confirmTarget = computed(() => {
  const output = view.outputs.value.find(
    (candidate) => candidate.id === confirming.value?.outputID,
  )
  return output === undefined ? '' : view.outputTitle(output)
})

const askConfirmation = (
  kind: 'reissue' | 'remove',
  outputID: string,
): void => {
  confirming.value = { kind, outputID }
  confirmError.value = null
  confirmOpen.value = true
}

const onConfirm = async (): Promise<void> => {
  const request = confirming.value
  if (request === null) return
  confirmError.value = null
  const error =
    request.kind === 'reissue'
      ? await view.reissue(request.outputID)
      : await view.remove(request.outputID)
  if (error !== null) {
    confirmError.value = error
    return
  }
  confirmOpen.value = false
  if (request.kind === 'remove') emit('changed')
}
</script>

<script lang="ts">
// The subscription links this tab has already opened the link block for. The
// link lives in memory for the life of the tab, so a later visit to the profile
// finds it again, but only a link nobody has been shown yet opens the block.
const announcedLinks = new Set<string>()
</script>

<template>
  <section
    :aria-labelledby="view.state.value === 'ready' ? titleID : undefined"
    class="profile"
  >
    <div
      v-if="view.state.value === 'loading'"
      :aria-label="t('profile.loading')"
      aria-busy="true"
      class="profile__loading rv-loading-feedback"
      role="status"
    >
      <span class="rv-visually-hidden">{{ t('profile.loading') }}</span>
      <span
        aria-hidden="true"
        class="profile__loading-line profile__loading-line--breadcrumb"
        data-testid="rv-skeleton-part"
      />
      <span
        aria-hidden="true"
        class="profile__loading-line profile__loading-line--title"
        data-testid="rv-skeleton-part"
      />
      <span aria-hidden="true" class="profile__loading-tabs">
        <span v-for="index in 4" :key="index" data-testid="rv-skeleton-part" />
      </span>
      <span
        aria-hidden="true"
        class="profile__loading-panel"
        data-testid="rv-skeleton-part"
      />
    </div>
    <RvStateNotice
      v-else-if="view.state.value === 'missing'"
      :body="t('profile.missing.body')"
      live
      :title="t('profile.missing')"
      tone="failed"
    >
      <template #action>
        <RvButton to="/" variant="secondary">{{ t('action.back') }}</RvButton>
      </template>
    </RvStateNotice>
    <RvStateNotice
      v-else-if="view.state.value === 'failed'"
      :body="t(`failure.${view.failure.value}.body`)"
      live
      :title="t('profiles.failed')"
      tone="failed"
    >
      <template #action>
        <RvButton @click="view.initialize">{{ t('action.retry') }}</RvButton>
      </template>
    </RvStateNotice>

    <template v-else>
      <RvStateNotice
        v-if="view.catalogRefresh.state.value === 'stale'"
        live
        :title="t('catalog.stale')"
        :body="t('catalog.stale.body')"
        tone="warning"
      >
        <template #action>
          <RvButton @click="view.catalogRefresh.retry">{{
            t('action.retry')
          }}</RvButton>
        </template>
      </RvStateNotice>

      <header class="profile__header">
        <nav :aria-label="t('profile.breadcrumb')" class="profile__breadcrumb">
          <NuxtLink class="profile__breadcrumb-link" to="/">
            {{ t('profiles.title') }}
          </NuxtLink>
        </nav>
        <h1 :id="titleID" ref="heading" class="profile__title" tabindex="-1">
          {{ displayName }}
        </h1>
        <div class="profile__header-tools">
          <RvStatus
            v-if="status !== null"
            :label="status.label"
            :tone="status.tone"
          />
          <RvMenu
            :items="profileMenuItems"
            :label="t('profiles.menu', { name: displayName })"
            @select="onProfileMenu"
          />
        </div>
      </header>

      <RvStateNotice
        v-if="importedOrdinal !== null"
        :body="t('profile.notice.imported.body')"
        :title="t('profile.notice.imported')"
        tone="waiting"
      />
      <RvStateNotice
        v-if="view.exportFailed.value"
        :body="t('profiles.export.failed.body')"
        live
        :title="t('profiles.export.failed')"
        tone="failed"
      />
      <RvStateNotice
        v-if="view.archived.value"
        :body="t('profile.notice.archived.body')"
        :title="t('profile.notice.archived')"
        tone="waiting"
      />
      <RvStateNotice
        v-if="view.work.value === 'adding'"
        :body="
          t('profile.notice.preparing.body', {
            target: setupTargetTitle,
          })
        "
        live
        :title="t('profile.notice.preparing')"
        tone="busy"
      />
      <RvStateNotice
        v-if="view.work.value === 'failed' && buildFailure === null"
        :body="t('profile.notice.action.failed.body')"
        live
        :title="t('profile.notice.action.failed')"
        tone="failed"
      />
      <!-- A read that follows a write and does not answer leaves the confirmed
           profile on screen, with the drafts beside it, and says it may be
           behind rather than swapping the page for a failure. -->
      <RvStateNotice
        v-if="view.rereadFailed.value"
        :body="t('profile.notice.reread.failed.body')"
        live
        :title="t('profile.notice.reread.failed')"
        tone="warning"
      >
        <template #action>
          <RvButton :disabled="view.busy.value" @click="view.initialize">{{
            t('action.retry')
          }}</RvButton>
        </template>
      </RvStateNotice>
      <RvStateNotice
        v-if="view.stale.value"
        :body="t('profile.notice.stale.body')"
        :title="t('profile.notice.stale')"
        tone="warning"
      />
      <RvStateNotice
        v-if="buildFailure !== null"
        :body="buildFailureBody"
        live
        :title="
          firstBuildFailed
            ? t('profiles.buildFailed')
            : t('profile.notice.build.failed')
        "
        tone="failed"
      >
        <template v-if="uncoveredLists.length > 0" #default>
          <UncoveredLists :lists="uncoveredLists" />
        </template>
        <template v-if="!view.archived.value" #action>
          <RvButton
            :disabled="view.busy.value"
            :loading="view.work.value === 'rebuilding'"
            :loading-label="t('profile.rebuild.busy')"
            @click="onRebuild"
            >{{ t('profiles.rebuild') }}</RvButton
          >
        </template>
      </RvStateNotice>
      <RvStateNotice
        v-if="view.partialCoverageCount.value > 0"
        :body="
          t('profile.notice.partial.body', {
            count: view.partialCoverageCount.value,
          })
        "
        :title="t('profile.notice.partial')"
        tone="warning"
      />
      <RvStateNotice
        v-if="view.degradedSources.value.length > 0"
        :body="
          t('profile.notice.degraded.body', {
            count: view.degradedSources.value.length,
          })
        "
        :title="t('profile.notice.degraded')"
        tone="warning"
      />

      <div
        v-if="view.subscriptionURL.value !== ''"
        ref="secretBlock"
        class="profile__secret"
      >
        <RvDisclosure
          borderless
          :open="linkOpen"
          :summary="t('profile.subscription', { target: selectedTitle })"
          @toggle="onLinkToggle"
        >
          <!-- The informer stands at the end of the summary row, where it stays
               reachable while the block is collapsed. -->
          <template #summary-end>
            <RvInfoTip
              :label="t('profile.subscription.info')"
              :text="t('profile.subscription.info.text')"
            />
          </template>
          <p class="profile__secret-value">
            {{
              view.revealed.value
                ? view.subscriptionURL.value
                : view.maskedSubscription.value
            }}
          </p>
          <div class="profile__secret-actions">
            <RvButton
              v-if="!view.revealed.value"
              variant="secondary"
              @click="view.reveal"
            >
              {{ t('profile.subscription.reveal') }}
            </RvButton>
            <RvCopyButton
              :copied-label="t('profile.subscription.copied')"
              :failed-label="t('profile.subscription.copyFailed')"
              :label="t('profile.subscription.copy')"
              :value="view.subscriptionURL.value"
            />
          </div>
        </RvDisclosure>
      </div>
      <RvTabs v-model="tab" :label="t('profile.tabs')" :tabs="tabs">
        <template #overview>
          <div
            class="profile__panel"
            :class="{
              'profile__panel--composition':
                !view.archived.value && view.profile.value !== null,
            }"
          >
            <ProfileEditor
              v-if="!view.archived.value && view.profile.value !== null"
              :busy="view.busy.value"
              :categories="view.catalog.value?.categories ?? []"
              :exclusions="view.profile.value.exclusions"
              :list-domains="view.profile.value.listDomains"
              :name="displayName"
              :outputs="editorOutputs"
              :priority="view.profile.value.priority"
              :selected="view.profile.value.lists"
              :selected-categories="view.profile.value.categories"
              :lists="view.catalog.value?.listDetails ?? []"
              @save="onSave"
            />
            <RvFacts v-else-if="facts.length > 0" :items="facts" />
            <section
              v-if="expert && technicalFacts.length > 0"
              aria-labelledby="profile-technical"
              class="profile__technical"
            >
              <h2 id="profile-technical" class="profile__section-title">
                {{ t('profile.technical') }}
              </h2>
              <RvFacts :items="technicalFacts" />
            </section>
          </div>
        </template>

        <template #outputs>
          <div class="profile__panel">
            <OutputsPanel
              ref="outputsPanel"
              :archived="view.archived.value"
              :automatic-delivery="view.automaticDelivery"
              :busy="view.busy.value"
              :deployable="view.deployable"
              :devices="view.devices.value"
              :hidden-target-count="view.hiddenTargetCount.value"
              :profile-id="profileId"
              :outputs="view.outputs.value"
              :schedule="view.schedule.value"
              :schedule-saving="view.scheduleState.value === 'saving'"
              :target-groups="view.targetGroups.value"
              :target-title="view.targetTitle"
              @bind="onBind"
              @bind-device="view.bindDevice"
              @rebuild="onRebuildOutput"
              @reissue="askConfirmation('reissue', $event)"
              @remove="askConfirmation('remove', $event)"
              @set-schedule="view.setSchedule"
              @settings-saved="view.initialize"
            />
            <RvStateNotice
              v-if="view.scheduleState.value === 'failed'"
              live
              :title="t('profile.schedule.failed')"
              :body="t('profile.schedule.failed.body')"
              tone="failed"
            >
              <template #action>
                <RvButton @click="view.retrySchedule">{{
                  t('action.retry')
                }}</RvButton>
              </template>
            </RvStateNotice>
          </div>
        </template>

        <template #file>
          <div class="profile__panel">
            <div
              v-if="view.outputs.value.length > 1"
              class="profile__panel-scope"
            >
              <label
                id="profile-file-scope-label"
                class="profile__panel-scope-label"
                for="profile-file-scope"
              >
                {{ t('profile.panel.scope') }}
              </label>
              <RvSelect
                v-model="outputScope"
                input-id="profile-file-scope"
                labelled-by="profile-file-scope-label"
                :options="outputChoices"
                :placeholder="t('outputs.column.target')"
              />
            </div>
            <RvStateNotice
              v-if="view.latest.value === null"
              :title="t('profile.diagnostics.none')"
              tone="waiting"
            />
            <RvStateNotice
              v-else-if="view.contentState.value === 'loading'"
              live
              :title="t('profile.file.loading')"
              tone="busy"
            />
            <RvStateNotice
              v-else-if="view.contentState.value === 'failed'"
              :body="t('profile.file.failed.body')"
              live
              :title="t('profile.file.failed')"
              tone="warning"
            >
              <template #action
                ><RvButton size="compact" @click="view.openContent()">{{
                  t('action.retry')
                }}</RvButton></template
              >
            </RvStateNotice>
            <RvCodeBlock
              v-else-if="view.content.value !== null"
              fill
              :caption="tc('profile.lines', view.lineCount.value)"
              :text="view.content.value.text"
            >
              <template #action>
                <RvCopyButton
                  :copied-label="t('profile.file.copied')"
                  :failed-label="t('profile.subscription.copyFailed')"
                  :label="t('profile.file.copy')"
                  :value="view.content.value.text"
                  variant="quiet"
                />
              </template>
            </RvCodeBlock>
          </div>
        </template>

        <template #diagnostics>
          <div class="profile__panel">
            <div
              v-if="view.outputs.value.length > 1 && !diagnosticsShown"
              class="profile__panel-scope"
            >
              <label
                id="profile-diagnostics-scope-label"
                class="profile__panel-scope-label"
                for="profile-diagnostics-scope"
              >
                {{ t('profile.panel.scope') }}
              </label>
              <RvSelect
                v-model="outputScope"
                input-id="profile-diagnostics-scope"
                labelled-by="profile-diagnostics-scope-label"
                :options="outputChoices"
                :placeholder="t('outputs.column.target')"
              />
            </div>
            <RvStateNotice
              v-if="view.snapshotID.value === ''"
              :title="t('profile.diagnostics.none')"
              tone="waiting"
            />
            <RvStateNotice
              v-else-if="view.diagnosticsState.value === 'loading'"
              live
              :title="t('profile.diagnostics.loading')"
              tone="busy"
            />
            <RvStateNotice
              v-else-if="view.diagnosticsState.value === 'failed'"
              :body="t('profile.diagnostics.failed.body')"
              live
              :title="t('profile.diagnostics.failed')"
              tone="warning"
            >
              <template #action
                ><RvButton size="compact" @click="view.openDiagnostics()">{{
                  t('action.retry')
                }}</RvButton></template
              >
            </RvStateNotice>
            <DiagnosticsPanel
              v-else-if="view.diagnosticsState.value === 'ready'"
              :key="view.snapshotID.value"
              :list-title="view.listTitle"
              :reason-label="reasonLabel"
              :rules="view.diagnostics.value"
            >
              <template v-if="view.outputs.value.length > 1" #scope>
                <div class="profile__panel-scope">
                  <label
                    id="profile-diagnostics-scope-label"
                    class="profile__panel-scope-label"
                    for="profile-diagnostics-scope"
                  >
                    {{ t('profile.panel.scope') }}
                  </label>
                  <RvSelect
                    v-model="outputScope"
                    input-id="profile-diagnostics-scope"
                    labelled-by="profile-diagnostics-scope-label"
                    :options="outputChoices"
                    :placeholder="t('outputs.column.target')"
                  />
                </div>
              </template>
            </DiagnosticsPanel>
          </div>
        </template>
      </RvTabs>

      <OutputConfirmDialog
        v-if="confirming !== null"
        v-model:open="confirmOpen"
        :busy="
          view.work.value === 'reissuing' || view.work.value === 'removing'
        "
        :error="confirmError"
        :kind="confirming.kind"
        :target="confirmTarget"
        @confirm="onConfirm"
      />
    </template>
  </section>
</template>

<style scoped>
.profile {
  container: route / inline-size;
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  gap: var(--rv-space-3);
  width: 100%;
  min-width: 0;
}

.profile > *,
.profile__panel {
  flex-shrink: 0;
}

/* The composition table has a standing height of its own, taken from the
   window rather than from whatever is left over. A notice above it is an
   addition to the page, not a subtraction from the table: the panel keeps this
   height and the page lengthens. It is a height rather than a flexible basis
   because a flexible item is exactly what "whatever is left over" means. */
.profile__panel--composition {
  height: var(--rv-composition-floor);
}

/* The first read keeps the page's final silhouette in place. No transient
   status sentence is painted above the title and then removed a frame later. */
.profile__loading {
  display: grid;
  gap: var(--rv-space-3);
  width: 100%;
}

/* The same placeholder ground the list card's loading rows stand on, so a
   placeholder is as visible here as there. It does not pulse: a pulse between
   two grounds passes through values the page cannot tell from its own. */
.profile__loading-line,
.profile__loading-tabs > span,
.profile__loading-panel {
  background: var(--rv-color-skeleton);
  border-radius: var(--rv-radius-sm);
}

.profile__loading-line--breadcrumb {
  width: min(var(--rv-measure-field), 45%);
  height: var(--rv-space-3);
}

.profile__loading-line--title {
  width: min(var(--rv-measure-field), 65%);
  height: var(--rv-space-8);
}

.profile__loading-tabs {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--rv-space-3);
  padding-bottom: var(--rv-space-3);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.profile__loading-tabs > span {
  height: var(--rv-control-compact);
}

.profile__loading-panel {
  min-height: var(--rv-editor-min-height);
}

.profile__header {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: var(--rv-space-1) var(--rv-space-4);
  align-items: center;
}

.profile__header-tools {
  margin-inline-start: auto;
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
}

.profile__breadcrumb {
  grid-column: 1 / -1;
  margin: 0;
}

.profile__breadcrumb-link {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-meta);
  text-decoration: none;
}

.profile__breadcrumb-link:hover {
  color: var(--rv-color-ink);
  text-decoration: underline;
}

.profile__breadcrumb-link:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-focus-offset);
}

.profile__title {
  font-size: var(--rv-text-page);
  line-height: var(--rv-leading-tight);
  letter-spacing: var(--rv-tracking-title);
  overflow-wrap: anywhere;
}

.profile__section-title {
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
  font-size: var(--rv-text-section);
}

.profile__secret {
  padding: 0 var(--rv-space-5);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-md);
}

/* The link is long and monospaced. It wraps inside its own box rather than
   widening the page, and its box does not change height when revealed. */
.profile__secret-value {
  padding: var(--rv-space-2) var(--rv-space-3);
  font-size: var(--rv-text-dense);
  font-family: var(--rv-font-mono);
  line-height: 1.6;
  overflow-wrap: anywhere;
  background: var(--rv-color-canvas);
  border-radius: var(--rv-radius-sm);
}

.profile__secret-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  align-items: center;
}

/* Outside the composition editor, which fills its standing height, rows keep
   their natural height and start at the top, so a short fact ledger stays a
   ledger rather than stretching to the window. */
.profile__panel {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--rv-space-6);
}

.profile__panel:not(.profile__panel--composition) {
  grid-auto-rows: max-content;
  align-content: start;
}

.profile__technical {
  display: grid;
  gap: var(--rv-space-3);
}

/* A panel that reads one published file asks which one, but only when the
   profile publishes more than one. */
.profile__panel-scope {
  display: grid;
  gap: var(--rv-space-2);
  width: min(100%, var(--rv-measure-field));
  margin-bottom: var(--rv-space-4);
}

.profile__panel-scope-label {
  font-weight: 600;
  font-size: var(--rv-text-dense);
}

@container (width <= 40rem) {
  .profile__header {
    grid-template-columns: minmax(0, 1fr);
  }

  .profile__header-tools {
    margin-inline-start: 0;
  }
}

/* Outside the fill layout the page is a column of natural heights and the
   table is capped on its own, so the panel takes the height its contents
   need — the same boundary the picker uses for its own fill. */
@media (width <= 64rem), (height <= 36rem) {
  .profile__panel--composition {
    height: auto;
    min-height: var(--rv-picker-mobile-height);
  }
}
</style>
