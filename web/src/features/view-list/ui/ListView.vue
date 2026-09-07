<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import ListEditor from '@/features/view-list/ui/ListEditor.vue'
import OutputsPanel from '@/features/view-list/ui/OutputsPanel.vue'
import { useListView } from '@/features/view-list/model/useListView'
import { useLocale } from '@/shared/i18n/useLocale'
import type { ExportFormat } from '@/shared/api/exports'
import { legacyImportedOrdinal } from '@/shared/lib/legacyList'
import { listPageHash, parseListPageHash } from '@/shared/lib/listHash'
import { useSurfacePreferences } from '@/shared/model/useSurfacePreferences'
import type { ListComposition } from '@/shared/api/lists'
import type { Fact, MenuItem, StatusTone, TabItem } from '@/shared/ui/kinds'
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
  listId: string
}>()

const emit = defineEmits<{
  changed: []
}>()

const { dateTime, formatBytes, t, tc, tor } = useLocale()
const { expert } = useSurfacePreferences()
const route = useRoute()
const router = useRouter()
const view = useListView(() => props.listId)
const pageLocation = computed(() => parseListPageHash(route.hash))
const titleID = computed(() => `list-title-${props.listId}`)
const heading = ref<HTMLElement | null>(null)
const tabIDs = new Set(['overview', 'outputs', 'file', 'diagnostics'])
const tab = ref(pageLocation.value.tab)

if (!tabIDs.has(tab.value)) tab.value = 'overview'

const tabs = computed<TabItem[]>(() => [
  { id: 'overview', label: t('list.tab.composition') },
  { id: 'outputs', label: t('list.tab.outputs') },
  { id: 'file', label: t('list.tab.file') },
  { id: 'diagnostics', label: t('list.tab.diagnostics') },
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
  if (view.archived.value) return t('list.status.archived')
  if (hasBuildFailure.value) return t('list.status.failed')
  if (!published.value) return t('list.status.empty')
  return warned.value ? t('list.status.warning') : t('list.status.ready')
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
    return t('list.notice.build.ruleLimit.body', {
      projected: failure.projectedRules,
      maximum: failure.maximumRules,
    })
  }
  return tor(
    `list.notice.build.${failure.code}.body`,
    t('list.notice.build.unknown.body'),
  )
})

const facts = computed<Fact[]>(() => {
  const list = view.list.value
  if (list === null) return []
  const items: Fact[] = [
    {
      key: 'services',
      label: t('list.facts.services'),
      value: view.serviceTitles.value.join(', '),
    },
    {
      key: 'outputs',
      label: t('list.facts.outputs'),
      value:
        view.outputs.value.length === 0
          ? t('library.noOutputs')
          : view.outputs.value.map(view.outputTitle).join(', '),
    },
  ]
  if (list.archivedAt !== '') {
    items.push({
      key: 'archived',
      label: t('list.facts.archived'),
      value: formatTime(list.archivedAt),
    })
  }
  const latest = view.latest.value
  if (latest !== null) {
    items.push({
      key: 'updated',
      label: t('list.facts.updated'),
      value: formatTime(latest.contentCreatedAt),
    })
  }
  if (view.ruleCount.value > 0) {
    items.push({
      key: 'rules',
      label: t('list.facts.rules'),
      value: tc('list.rules', view.ruleCount.value),
    })
  }
  // The rule is stated with where it came from, so "as in settings" is visibly
  // a deferral rather than a value this list chose.
  const schedule = view.schedule.value
  if (schedule !== null) {
    items.push({
      key: 'schedule',
      label: t('list.facts.schedule'),
      value: schedule.followsDefault
        ? t('list.schedule.followsDefault', {
            rule: t(`settings.refresh.${schedule.effective || 'off'}`),
          })
        : t(`settings.refresh.${schedule.effective || 'off'}`),
    })
    if (schedule.lastRefreshedAt !== '') {
      items.push({
        key: 'refreshed',
        label: t('list.facts.refreshed'),
        value: schedule.lastRefreshFailed
          ? t('list.schedule.lastFailed', {
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
      label: t('list.technical.artifact'),
      mono: true,
      value: latest.id,
    })
    items.push({
      key: 'size',
      label: t('list.technical.size'),
      value: formatBytes(latest.sizeBytes),
    })
    if (latest.contentType !== '') {
      items.push({
        key: 'contentType',
        label: t('list.technical.contentType'),
        mono: true,
        value: latest.contentType,
      })
    }
  }
  if (view.list.value !== null) {
    items.push({
      key: 'list',
      label: t('list.technical.list'),
      mono: true,
      value: view.list.value.id,
    })
  }
  if (view.snapshotID.value !== '') {
    items.push({
      key: 'snapshot',
      label: t('list.technical.snapshot'),
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
// The editor asks the server what the draft weighs in the formats this list
// already publishes, so it takes them already named for this reader.
const editorOutputs = computed(() =>
  view.outputs.value.map((output) => ({
    id: output.id,
    targetID: output.targetID,
    title: view.outputTitle(output),
  })),
)
const importedOrdinal = computed(() =>
  legacyImportedOrdinal(view.list.value?.name ?? ''),
)
const displayName = computed(() => {
  const ordinal = importedOrdinal.value
  return ordinal === null
    ? (view.list.value?.name ?? '')
    : t('library.imported.name', { number: ordinal })
})

watch(tab, (value) => {
  if (value === 'file') void view.openContent()
  if (value === 'diagnostics') void view.openDiagnostics()
  void router.replace({ hash: listPageHash({ tab: value }) })
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
    await router.replace({ hash: listPageHash({ tab: tab.value }) })
  }
  heading.value?.focus()
})

function formatTime(value: string): string {
  const parsed = new Date(value)
  return Number.isNaN(parsed.valueOf()) ? '—' : dateTime.value.format(parsed)
}

function reasonLabel(code: string): string {
  return tor(
    `list.diagnostics.reason.${code}`,
    t('list.diagnostics.reason.unknown'),
  )
}

async function onSave(
  name: string,
  composition: ListComposition,
): Promise<void> {
  if (await view.save(name, composition)) emit('changed')
}

async function onBind(targetID: string): Promise<void> {
  if (await view.bind(targetID)) emit('changed')
}

async function onArchive(next: boolean): Promise<void> {
  if (await view.setArchived(next)) emit('changed')
}

function exportLabel(format: ExportFormat): string {
  return tor(
    `export.format.${format.rendererID}`,
    format.fileExtension.toUpperCase(),
  )
}

const listMenuItems = computed<MenuItem[]>(() => {
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
    label: t('library.download.menu'),
  })
  const deployItems = view.outputs.value
    .filter(view.deployable)
    .map((output) => ({
      icon: 'send' as const,
      key: `send:${output.id}`,
      label: t('library.sendTarget', { target: view.outputTitle(output) }),
      to: `/profiles/${props.listId}/send/${output.id}`,
    }))
  if (deployItems.length === 1) items.push(deployItems[0] as MenuItem)
  if (deployItems.length > 1) {
    items.push({
      children: deployItems,
      icon: 'send',
      key: 'send',
      label: t('library.send'),
    })
  }
  if (!view.archived.value) {
    items.push({
      disabled: view.busy.value || view.outputs.value.length === 0,
      icon: 'refresh',
      key: 'rebuild',
      label: t('list.rebuild'),
    })
  }
  items.push({
    disabled: view.busy.value,
    icon: 'archive',
    key: view.archived.value ? 'restore' : 'archive',
    label: view.archived.value ? t('list.restore') : t('list.archive'),
    separatorBefore: true,
  })
  return items
})

async function onListMenu(key: string): Promise<void> {
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
    class="list"
  >
    <div
      v-if="view.state.value === 'loading'"
      :aria-label="t('list.loading')"
      aria-busy="true"
      class="list__loading rv-loading-feedback"
      role="status"
    >
      <span class="list__visually-hidden">{{ t('list.loading') }}</span>
      <span
        aria-hidden="true"
        class="list__loading-line list__loading-line--breadcrumb"
      />
      <span
        aria-hidden="true"
        class="list__loading-line list__loading-line--title"
      />
      <span aria-hidden="true" class="list__loading-tabs">
        <span v-for="index in 4" :key="index" />
      </span>
      <span aria-hidden="true" class="list__loading-panel" />
    </div>
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
      v-else-if="view.state.value === 'failed'"
      :body="t('library.failed.body')"
      live
      :title="t('library.failed')"
      tone="failed"
    >
      <template #action>
        <RvButton @click="view.initialize">{{ t('action.retry') }}</RvButton>
      </template>
    </RvStateNotice>

    <template v-else>
      <header class="list__header">
        <nav :aria-label="t('list.breadcrumb')" class="list__breadcrumb">
          <NuxtLink class="list__breadcrumb-link" to="/">
            {{ t('library.title') }}
          </NuxtLink>
        </nav>
        <h1 :id="titleID" ref="heading" class="list__title" tabindex="-1">
          {{ displayName }}
        </h1>
        <div class="list__header-tools">
          <RvStatus :label="statusLabel" :tone="statusTone" />
          <RvMenu
            :items="listMenuItems"
            :label="t('library.menu', { name: displayName })"
            @select="onListMenu"
          />
        </div>
      </header>

      <RvStateNotice
        v-if="importedOrdinal !== null"
        :body="t('list.notice.imported.body')"
        :title="t('list.notice.imported')"
        tone="waiting"
      />
      <RvStateNotice
        v-if="view.exportFailed.value"
        :body="t('library.export.failed.body')"
        live
        :title="t('library.export.failed')"
        tone="failed"
      />
      <RvStateNotice
        v-if="view.archived.value"
        :body="t('list.notice.archived.body')"
        :title="t('list.notice.archived')"
        tone="waiting"
      />
      <RvStateNotice
        v-if="view.work.value === 'adding'"
        :body="
          t('list.notice.preparing.body', {
            target: setupTargetTitle,
          })
        "
        live
        :title="t('list.notice.preparing')"
        tone="busy"
      />
      <RvStateNotice
        v-if="view.work.value === 'failed' && buildFailure === null"
        :body="t('list.notice.action.failed.body')"
        live
        :title="t('list.notice.action.failed')"
        tone="failed"
      />
      <RvStateNotice
        v-if="view.stale.value"
        :body="t('list.notice.stale.body')"
        :title="t('list.notice.stale')"
        tone="warning"
      />
      <RvStateNotice
        v-if="buildFailure !== null"
        :body="buildFailureBody"
        live
        :title="t('list.notice.build.failed')"
        tone="failed"
      />
      <RvStateNotice
        v-if="view.partialCoverageCount.value > 0"
        :body="
          t('list.notice.partial.body', {
            count: view.partialCoverageCount.value,
          })
        "
        :title="t('list.notice.partial')"
        tone="warning"
      />
      <RvStateNotice
        v-if="view.degradedSources.value.length > 0"
        :body="
          t('list.notice.degraded.body', {
            count: view.degradedSources.value.length,
          })
        "
        :title="t('list.notice.degraded')"
        tone="warning"
      />

      <section
        v-if="view.subscriptionURL.value !== ''"
        aria-labelledby="list-subscription"
        class="list__secret"
      >
        <h2 id="list-subscription" class="list__section-title">
          {{ t('list.subscription', { target: selectedTitle }) }}
          <RvInfoTip
            :label="t('list.subscription.info')"
            :text="t('list.subscription.info.text')"
          />
        </h2>
        <p class="list__secret-value">
          {{
            view.revealed.value
              ? view.subscriptionURL.value
              : view.maskedSubscription.value
          }}
        </p>
        <div class="list__secret-actions">
          <RvButton
            v-if="!view.revealed.value"
            size="compact"
            @click="view.reveal"
          >
            {{ t('list.subscription.reveal') }}
          </RvButton>
          <RvCopyButton
            :copied-label="t('list.subscription.copied')"
            :failed-label="t('list.subscription.copyFailed')"
            :label="t('list.subscription.copy')"
            :value="view.subscriptionURL.value"
          />
        </div>
      </section>
      <p
        v-else-if="expert && view.latest.value !== null"
        class="list__secret-gone"
      >
        {{ t('list.subscription.gone') }}
      </p>

      <RvTabs v-model="tab" :label="t('list.tabs')" :tabs="tabs" />

      <div
        v-show="tab === 'overview'"
        id="rv-panel-overview"
        aria-labelledby="rv-tab-overview"
        class="list__panel list__panel--composition"
        role="tabpanel"
      >
        <ListEditor
          v-if="!view.archived.value && view.list.value !== null"
          :busy="view.busy.value"
          :categories="view.catalog.value?.categories ?? []"
          :exclusions="view.list.value.exclusions"
          :service-domains="view.list.value.serviceDomains"
          :name="displayName"
          :outputs="editorOutputs"
          :priority="view.list.value.priority"
          :selected="view.list.value.services"
          :selected-categories="view.list.value.categories"
          :services="view.catalog.value?.serviceDetails ?? []"
          @save="onSave"
        />
        <RvFacts v-else-if="facts.length > 0" :items="facts" />
        <section
          v-if="expert && technicalFacts.length > 0"
          aria-labelledby="list-technical"
          class="list__technical"
        >
          <h2 id="list-technical" class="list__section-title">
            {{ t('list.technical') }}
          </h2>
          <RvFacts :items="technicalFacts" />
        </section>
      </div>

      <div
        v-show="tab === 'outputs'"
        id="rv-panel-outputs"
        aria-labelledby="rv-tab-outputs"
        class="list__panel"
        role="tabpanel"
      >
        <OutputsPanel
          :archived="view.archived.value"
          :busy="view.busy.value"
          :deployable="view.deployable"
          :devices="view.devices.value"
          :list-id="listId"
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
        class="list__panel"
        role="tabpanel"
      >
        <p v-if="view.outputs.value.length > 1" class="list__panel-scope">
          {{ t('list.panel.scope', { target: selectedTitle }) }}
        </p>
        <RvStateNotice
          v-if="view.latest.value === null"
          :title="t('list.diagnostics.none')"
          tone="waiting"
        />
        <RvStateNotice
          v-else-if="view.contentState.value === 'loading'"
          live
          :title="t('list.file.loading')"
          tone="busy"
        />
        <RvStateNotice
          v-else-if="view.contentState.value === 'failed'"
          :body="t('list.file.failed.body')"
          live
          :title="t('list.file.failed')"
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
          :caption="tc('list.lines', view.lineCount.value)"
          :text="view.content.value.text"
        >
          <template #action>
            <RvCopyButton
              :copied-label="t('list.file.copied')"
              :failed-label="t('list.subscription.copyFailed')"
              :label="t('list.file.copy')"
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
        class="list__panel"
        role="tabpanel"
      >
        <p v-if="view.outputs.value.length > 1" class="list__panel-scope">
          {{ t('list.panel.scope', { target: selectedTitle }) }}
        </p>
        <RvStateNotice
          v-if="view.snapshotID.value === ''"
          :title="t('list.diagnostics.none')"
          tone="waiting"
        />
        <RvStateNotice
          v-else-if="view.diagnosticsState.value === 'loading'"
          live
          :title="t('list.diagnostics.loading')"
          tone="busy"
        />
        <RvStateNotice
          v-else-if="view.diagnosticsState.value === 'failed'"
          :body="t('list.diagnostics.failed.body')"
          live
          :title="t('list.diagnostics.failed')"
          tone="warning"
        >
          <template #action
            ><RvButton size="compact" @click="view.openDiagnostics()">{{
              t('action.retry')
            }}</RvButton></template
          >
        </RvStateNotice>
        <template v-else-if="view.diagnosticsState.value === 'ready'">
          <p class="list__prose">{{ t('list.diagnostics.perService') }}</p>
          <ul class="list__counts">
            <li
              v-for="entry in view.rulesByService.value"
              :key="entry.id"
              class="list__count"
            >
              <span>{{ view.serviceTitle(entry.id) }}</span>
              <strong>{{ tc('list.rules', entry.count) }}</strong>
            </li>
          </ul>
          <p
            v-if="view.diagnostics.value.every((rule) => !rule.excluded)"
            class="list__prose"
          >
            {{ t('list.diagnostics.empty') }}
          </p>
          <ul class="list__rules">
            <li
              v-for="rule in view.diagnostics.value"
              :key="`${rule.excluded}-${rule.serviceID}-${rule.value}`"
              class="list__rule"
            >
              <span class="list__rule-state">
                {{
                  rule.excluded
                    ? t('list.diagnostics.excluded')
                    : t('list.diagnostics.included')
                }}
              </span>
              <span class="list__rule-service">
                {{ view.serviceTitle(rule.serviceID) }}
              </span>
              <span class="list__rule-value">{{ rule.value }}</span>
              <span class="list__rule-reason">
                {{ rule.reasons.map(reasonLabel).join(', ') }}
              </span>
            </li>
          </ul>
        </template>
      </div>
    </template>
  </section>
</template>

<style scoped src="./ListView.css"></style>
