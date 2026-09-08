<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import ProfileEditor from '@/features/view-profile/ui/ProfileEditor.vue'
import OutputsPanel from '@/features/view-profile/ui/OutputsPanel.vue'
import { useProfileView } from '@/features/view-profile/model/useProfileView'
import { useLocale } from '@/shared/i18n/useLocale'
import type { ExportFormat } from '@/shared/api/exports'
import { legacyImportedOrdinal } from '@/shared/lib/legacyProfile'
import { profilePageHash, parseProfilePageHash } from '@/shared/lib/profileHash'
import { useSurfacePreferences } from '@/shared/model/useSurfacePreferences'
import type { ProfileComposition } from '@/shared/api/profiles'
import type { Fact, MenuItem, StatusTone, TabItem } from '@/shared/ui/types'
import RvButton from '@/shared/ui/RvButton.vue'
import RvCodeBlock from '@/shared/ui/RvCodeBlock.vue'
import RvCopyButton from '@/shared/ui/RvCopyButton.vue'
import RvFacts from '@/shared/ui/RvFacts.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import RvMenu from '@/shared/ui/RvMenu.vue'
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

const warned = computed(
  () =>
    view.stale.value ||
    view.partialCoverageCount.value > 0 ||
    view.degradedSources.value.length > 0,
)

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

const statusLabel = computed(() => {
  if (view.archived.value) return t('profile.status.archived')
  if (hasBuildFailure.value) return t('profile.status.failed')
  if (!published.value) return t('profile.status.empty')
  return warned.value ? t('profile.status.warning') : t('profile.status.ready')
})

const statusTone = computed<StatusTone>(() => {
  if (view.archived.value) return 'waiting'
  if (hasBuildFailure.value) return 'failed'
  if (!published.value) return 'waiting'
  return warned.value ? 'warning' : 'ready'
})

const buildFailureBody = computed(() => {
  const failure = buildFailure.value
  if (failure === null) return ''
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
  void router.replace({ hash: profilePageHash({ tab: value }) })
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
  heading.value?.focus()
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

const profileMenuItems = computed<MenuItem[]>(() => {
  const items: MenuItem[] = []
  items.push({
    children: view.exportFormats.value.map((format) => ({
      disabled: view.exporting.value !== '',
      icon: 'download' as const,
      key: `export:${format.id}`,
      label: exportLabel(format),
    })),
    icon: 'download',
    key: 'downloads',
    label: t('profiles.download.menu'),
  })
  const deployItems = view.outputs.value
    .filter(view.deployable)
    .map((output) => ({
      icon: 'send' as const,
      key: `send:${output.id}`,
      label: t('profiles.sendTarget', { target: view.outputTitle(output) }),
      to: `/profiles/${props.profileId}/send/${output.id}`,
    }))
  if (deployItems.length === 1) items.push(deployItems[0] as MenuItem)
  if (deployItems.length > 1) {
    items.push({
      children: deployItems,
      icon: 'send',
      key: 'send',
      label: t('profiles.send'),
    })
  }
  if (!view.archived.value) {
    items.push({
      disabled: view.busy.value || view.outputs.value.length === 0,
      icon: 'refresh',
      key: 'rebuild',
      label: t('profile.rebuild'),
    })
  }
  items.push({
    disabled: view.busy.value,
    icon: 'archive',
    key: view.archived.value ? 'restore' : 'archive',
    label: view.archived.value ? t('profile.restore') : t('profile.archive'),
    separatorBefore: true,
  })
  return items
})

const onProfileMenu = async (key: string): Promise<void> => {
  if (key.startsWith('export:')) {
    await view.download(key.slice('export:'.length))
    return
  }
  if (key === 'rebuild') {
    await view.rebuild()
    emit('changed')
  }
  if (key === 'archive') await onArchive(true)
  if (key === 'restore') await onArchive(false)
}
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
      <span class="profile__visually-hidden">{{ t('profile.loading') }}</span>
      <span
        aria-hidden="true"
        class="profile__loading-line profile__loading-line--breadcrumb"
      />
      <span
        aria-hidden="true"
        class="profile__loading-line profile__loading-line--title"
      />
      <span aria-hidden="true" class="profile__loading-tabs">
        <span v-for="index in 4" :key="index" />
      </span>
      <span aria-hidden="true" class="profile__loading-panel" />
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
      :body="t('profiles.failed.body')"
      live
      :title="t('profiles.failed')"
      tone="failed"
    >
      <template #action>
        <RvButton @click="view.initialize">{{ t('action.retry') }}</RvButton>
      </template>
    </RvStateNotice>

    <template v-else>
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
          <RvStatus :label="statusLabel" :tone="statusTone" />
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
        :title="t('profile.notice.build.failed')"
        tone="failed"
      />
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

      <section
        v-if="view.subscriptionURL.value !== ''"
        aria-labelledby="profile-subscription"
        class="profile__secret"
      >
        <h2 id="profile-subscription" class="profile__section-title">
          {{ t('profile.subscription', { target: selectedTitle }) }}
          <RvInfoTip
            :label="t('profile.subscription.info')"
            :text="t('profile.subscription.info.text')"
          />
        </h2>
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
            size="compact"
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
      </section>
      <p
        v-else-if="expert && view.latest.value !== null"
        class="profile__secret-gone"
      >
        {{ t('profile.subscription.gone') }}
      </p>

      <RvTabs v-model="tab" :label="t('profile.tabs')" :tabs="tabs" />

      <div
        v-show="tab === 'overview'"
        id="rv-panel-overview"
        aria-labelledby="rv-tab-overview"
        class="profile__panel profile__panel--composition"
        role="tabpanel"
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

      <div
        v-show="tab === 'outputs'"
        id="rv-panel-outputs"
        aria-labelledby="rv-tab-outputs"
        class="profile__panel"
        role="tabpanel"
      >
        <OutputsPanel
          :archived="view.archived.value"
          :busy="view.busy.value"
          :deployable="view.deployable"
          :devices="view.devices.value"
          :profile-id="profileId"
          :outputs="view.outputs.value"
          :schedule="view.schedule.value"
          :selected-id="view.selectedOutputID.value"
          :target-groups="view.targetGroups.value"
          :target-title="view.targetTitle"
          @bind="onBind"
          @bind-device="view.bindDevice"
          @select="view.selectOutput"
          @set-schedule="view.setSchedule"
        />
      </div>

      <div
        v-show="tab === 'file'"
        id="rv-panel-file"
        aria-labelledby="rv-tab-file"
        class="profile__panel"
        role="tabpanel"
      >
        <p v-if="view.outputs.value.length > 1" class="profile__panel-scope">
          {{ t('profile.panel.scope', { target: selectedTitle }) }}
        </p>
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

      <div
        v-show="tab === 'diagnostics'"
        id="rv-panel-diagnostics"
        aria-labelledby="rv-tab-diagnostics"
        class="profile__panel"
        role="tabpanel"
      >
        <p v-if="view.outputs.value.length > 1" class="profile__panel-scope">
          {{ t('profile.panel.scope', { target: selectedTitle }) }}
        </p>
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
        <template v-else-if="view.diagnosticsState.value === 'ready'">
          <p class="profile__prose">
            {{ t('profile.diagnostics.perList') }}
          </p>
          <ul class="profile__counts">
            <li
              v-for="entry in view.rulesByList.value"
              :key="entry.id"
              class="profile__count"
            >
              <span>{{ view.listTitle(entry.id) }}</span>
              <strong>{{ tc('profile.rules', entry.count) }}</strong>
            </li>
          </ul>
          <p
            v-if="view.diagnostics.value.every((rule) => !rule.excluded)"
            class="profile__prose"
          >
            {{ t('profile.diagnostics.empty') }}
          </p>
          <ul class="profile__rules">
            <li
              v-for="rule in view.diagnostics.value"
              :key="`${rule.excluded}-${rule.listID}-${rule.value}`"
              class="profile__rule"
            >
              <span class="profile__rule-state">
                {{
                  rule.excluded
                    ? t('profile.diagnostics.excluded')
                    : t('profile.diagnostics.included')
                }}
              </span>
              <span class="profile__rule-list">
                {{ view.listTitle(rule.listID) }}
              </span>
              <span class="profile__rule-value">{{ rule.value }}</span>
              <span class="profile__rule-reason">
                {{ rule.reasons.map(reasonLabel).join(', ') }}
              </span>
            </li>
          </ul>
        </template>
      </div>
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

.profile > * {
  flex-shrink: 0;
}

.profile__panel--composition {
  flex: 1;
  min-height: var(--rv-picker-mobile-height);
}

/* The first read keeps the page's final silhouette in place. No transient
   status sentence is painted above the title and then removed a frame later. */
.profile__loading {
  display: grid;
  gap: var(--rv-space-3);
  width: 100%;
}

.profile__loading-line,
.profile__loading-tabs > span,
.profile__loading-panel {
  background: var(--rv-color-surface);
  border-radius: var(--rv-radius-sm);
  animation: rv-list-loading var(--rv-motion-working) var(--rv-motion-ease-out)
    infinite alternate;
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

.profile__visually-hidden {
  position: absolute;
  width: var(--rv-border-hair);
  height: var(--rv-border-hair);
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}

@keyframes rv-list-loading {
  from {
    background: var(--rv-color-surface);
  }

  to {
    background: var(--rv-color-surface-muted);
  }
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

.profile__section {
  display: grid;
  gap: var(--rv-space-4);
  padding-top: var(--rv-space-4);
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.profile__section--drawer {
  padding-top: var(--rv-space-4);
}

.profile__secret {
  display: grid;
  gap: var(--rv-space-4);
  padding: var(--rv-space-5);
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

.profile__secret-gone {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-dense);
}

.profile__panel {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--rv-space-6);
}

.profile__technical {
  display: grid;
  gap: var(--rv-space-3);
}

.profile__prose {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-interface);
}

.profile__counts {
  display: grid;
  gap: var(--rv-space-1);
}

.profile__count {
  display: flex;
  gap: var(--rv-space-3);
  justify-content: space-between;
  padding: var(--rv-space-2) 0;
  font-size: var(--rv-text-dense);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

/* A profile can carry hundreds of rules. The ledger scrolls inside its own box so
   opening diagnostics does not turn the page into a mile of rows. */
.profile__rules {
  display: grid;
  gap: var(--rv-space-2);
  max-height: 24rem;
  padding-right: var(--rv-space-1);
  overflow-y: auto;
}

.profile__rule {
  display: grid;
  grid-template-columns: minmax(6rem, auto) minmax(6rem, auto) minmax(0, 1fr);
  gap: var(--rv-space-1) var(--rv-space-3);
  padding: var(--rv-space-2) var(--rv-space-3);
  font-size: var(--rv-text-dense);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-sm);
}

.profile__rule-state {
  font-weight: 600;
}

.profile__rule-list {
  color: var(--rv-color-ink-muted);
}

.profile__rule-value {
  font-family: var(--rv-font-mono);
  overflow-wrap: anywhere;
}

.profile__rule-reason {
  grid-column: 1 / -1;
  color: var(--rv-color-ink-tertiary);
  font-family: var(--rv-font-mono);
  overflow-wrap: anywhere;
}

@container route (width <= 44rem) {
  .profile__rule {
    grid-template-columns: 1fr;
  }
}

/* A panel that describes one output says which one, but only when the profile
   feeds more than one. */
.profile__panel-scope {
  margin-bottom: var(--rv-space-3);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

/* The schedule is four labelled choices, named by the section heading above
   them. The control is as wide as its longest option unless it is told
   otherwise,
   and in a grid that intrinsic width becomes the container's minimum. */
.profile__schedule-field {
  width: 100%;
  max-width: var(--rv-measure-field);
  min-width: 0;
}

.profile__schedule-note {
  margin-top: var(--rv-space-2);
  color: var(--rv-color-ink-muted);
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
</style>
