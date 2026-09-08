import { computed, ref, watch, type Ref } from 'vue'

import { usePublishedProfile } from '@/entities/profile-build/model/publishedProfile'
import { useFreshCatalog } from '@/shared/model/useFreshCatalog'
import { useSurfacePreferences } from '@/shared/model/useSurfacePreferences'
import {
  loadArtifactContent,
  loadDiagnostics,
  type ArtifactContent,
  type DiagnosticRule,
} from '@/shared/api/artifacts'
import {
  loadCatalogCached,
  localizedTargetTitle,
  type Catalog,
  type TargetOption,
} from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'
import { loadDeployableTargets } from '@/shared/api/deploy'
import { loadDevices, type DeviceCard } from '@/shared/api/devices'
import {
  downloadProfileExport,
  loadExportFormats,
  type ExportFormat,
} from '@/shared/api/exports'
import { RoutevaneAPIError } from '@/shared/api/http'
import {
  archiveProfile,
  isArchived,
  loadProfile,
  refreshProfile,
  restoreProfile,
  saveProfileRefreshInterval,
  updateProfile,
  type ProfileComposition,
  type RefreshInterval,
  type Schedule,
  type ProfileRecord,
} from '@/shared/api/profiles'
import {
  addOutput,
  buildOutput,
  setOutputDevice,
  type OutputCard,
} from '@/shared/api/outputs'

export type ProfileState = 'loading' | 'ready' | 'missing' | 'failed'
export type LoadState = 'idle' | 'loading' | 'ready' | 'failed'
export type WorkState =
  | 'idle'
  | 'saving'
  | 'adding'
  | 'binding'
  | 'rebuilding'
  | 'archiving'
  | 'failed'

/**
 * One stored profile and the formats it publishes in. The server names its facts;
 * the fresh build this tab just finished adds what the server does not repeat —
 * the one-time subscription link and the build summary. Nothing is invented to
 * fill a gap: a fact the session does not hold is simply not stated.
 */
export const useProfileView = (profileID: () => string) => {
  const publishedProfile = usePublishedProfile()
  const preferences = useSurfacePreferences()
  const { locale } = useLocale()

  const state = ref<ProfileState>('loading')
  const profile = ref<ProfileRecord | null>(null)
  const resolved = ref<string[]>([])
  const missingCategories = ref<string[]>([])
  const schedule = ref<Schedule | null>(null)
  const scheduleState = ref<'idle' | 'saving' | 'failed'>('idle')
  const requestedInterval = ref<RefreshInterval>('off')
  let scheduleRevision = 0
  watch(profileID, () => {
    scheduleRevision += 1
    scheduleState.value = 'idle'
  })
  const outputs = ref<OutputCard[]>([])
  const catalog = ref<Catalog | null>(null)
  const deployableIDs = ref<Set<string>>(new Set())
  const devices = ref<DeviceCard[]>([])
  const selectedOutputID = ref('')
  const content = ref<ArtifactContent | null>(null)
  const contentState = ref<LoadState>('idle')
  const diagnostics = ref<DiagnosticRule[]>([])
  const diagnosticsState = ref<LoadState>('idle')
  const revealed = ref(false)
  const work = ref<WorkState>('idle')
  const rebuildFailed = ref(false)
  const exportFormats = ref<ExportFormat[]>([])
  const exporting = ref('')
  const exportFailed = ref(false)

  const busy = computed(() =>
    ['saving', 'adding', 'binding', 'rebuilding', 'archiving'].includes(
      work.value,
    ),
  )

  // An archived profile is fully readable and fully downloadable; what it no
  // longer accepts is change. The screen asks this once and every write is
  // disabled from the same answer, so no control can be left enabled against a
  // server that will refuse it.
  const archived = computed(
    () => profile.value !== null && isArchived(profile.value),
  )

  const selectedOutput = computed<OutputCard | null>(
    () =>
      outputs.value.find((output) => output.id === selectedOutputID.value) ??
      null,
  )
  const latest = computed(() => selectedOutput.value?.latest ?? null)
  const selectedAttempt = computed(
    () => selectedOutput.value?.lastAttempt ?? null,
  )
  const failedOutputs = computed(() =>
    outputs.value.filter((output) => output.lastAttempt?.status === 'failed'),
  )
  const fresh = computed(() =>
    selectedOutputID.value === ''
      ? null
      : publishedProfile.forOutput(selectedOutputID.value),
  )
  const snapshotID = computed(
    () => fresh.value?.snapshotID ?? latest.value?.snapshotID ?? '',
  )
  const subscriptionURL = computed(() => fresh.value?.subscriptionURL ?? '')
  watch(
    [selectedOutputID, subscriptionURL],
    () => {
      revealed.value = false
    },
    { flush: 'sync' },
  )

  // What the profile publishes right now, as the server resolved it. The screen
  // never resolves references itself: a stale catalog copy would disagree with
  // the build.
  const listTitles = computed<string[]>(() => resolved.value.map(listTitle))

  // A target already bound to this profile is not offered again, and a target
  // hidden on the Targets screen stays out of the picker.
  const availableTargets = computed<TargetOption[]>(() => {
    const bound = new Set(outputs.value.map((output) => output.targetID))
    return (catalog.value?.targets ?? []).filter(
      (target) =>
        !bound.has(target.id) &&
        !preferences.hiddenTargets.value.includes(target.id),
    )
  })

  const targetGroups = computed<{ kind: string; targets: TargetOption[] }[]>(
    () =>
      (['router', 'app'] as const)
        .map((kind) => ({
          kind,
          targets: availableTargets.value.filter(
            (target) => target.kind === kind,
          ),
        }))
        .filter((group) => group.targets.length > 0),
  )

  const deployable = (output: OutputCard): boolean =>
    output.latest !== null && deployableIDs.value.has(output.targetID)

  const targetOption = (targetID: string): TargetOption | null =>
    catalog.value?.targets.find((target) => target.id === targetID) ?? null

  /**
   * What to call a format on this screen. The catalog answers in the reader's
   * language where it can; an output whose target has since left the catalog
   * keeps the title stored with it rather than showing an identifier.
   */
  const targetTitle = (targetID: string, fallback = ''): string =>
    localizedTargetTitle(
      targetOption(targetID),
      locale.value,
      fallback === '' ? targetID : fallback,
    )

  const outputTitle = (output: OutputCard): string =>
    targetTitle(output.targetID, output.targetTitle)

  const ruleCount = computed(() => fresh.value?.ruleCount ?? 0)
  const partialCoverageCount = computed(() =>
    fresh.value !== null && fresh.value.partialCoverage
      ? fresh.value.partialCoverageCount
      : 0,
  )
  const degradedSources = computed<string[]>(
    () => fresh.value?.degradedSources ?? [],
  )
  const stale = computed(
    () =>
      fresh.value?.stale === true ||
      rebuildFailed.value ||
      (latest.value !== null && selectedAttempt.value?.status === 'failed'),
  )

  // The link is masked until asked for, so a screen left open on a desk does
  // not display a working secret.
  const maskedSubscription = computed(() => {
    if (subscriptionURL.value === '') return ''
    try {
      const url = new URL(subscriptionURL.value)
      return `${url.origin}/v1/subscriptions/${'•'.repeat(12)}`
    } catch {
      return '•'.repeat(24)
    }
  })

  const lineCount = computed(() => {
    const text = content.value?.text
    if (text === undefined || text === '') return 0
    return text.trimEnd().split('\n').length
  })

  const rulesByList = computed<{ id: string; count: number }[]>(() => {
    const counts = new Map<string, number>()
    for (const rule of diagnostics.value) {
      if (rule.excluded) continue
      counts.set(rule.listID, (counts.get(rule.listID) ?? 0) + 1)
    }
    return [...counts.entries()]
      .map(([id, count]) => ({ count, id }))
      .sort((left, right) => left.id.localeCompare(right.id))
  })

  const initialize = async (): Promise<void> => {
    const revision = scheduleRevision
    state.value = 'loading'
    devices.value = []
    const [detail, loadedCatalog, deployables, formats, registry] =
      await Promise.allSettled([
        loadProfile(profileID()),
        loadCatalogCached(),
        loadDeployableTargets(),
        loadExportFormats(),
        loadDevices(),
      ])
    if (loadedCatalog.status === 'fulfilled')
      catalog.value = loadedCatalog.value
    if (deployables.status === 'fulfilled') {
      deployableIDs.value = new Set(
        deployables.value.map((entry) => entry.targetID),
      )
    }
    if (formats.status === 'fulfilled') exportFormats.value = formats.value
    if (registry.status === 'fulfilled') devices.value = registry.value.devices
    if (detail.status === 'rejected') {
      // A profile the server has never heard of (404) is a different fact from a
      // profile the server cannot answer for right now: only the first is
      // "missing". Everything else — 5xx, a network failure, a contract the
      // client cannot decode — leaves the profile possibly still there, so the
      // screen offers retry instead of "not found".
      state.value =
        detail.reason instanceof RoutevaneAPIError &&
        detail.reason.status === 404
          ? 'missing'
          : 'failed'
      return
    }
    profile.value = detail.value.profile
    resolved.value = detail.value.resolved
    missingCategories.value = detail.value.missingCategories
    if (revision === scheduleRevision && scheduleState.value !== 'saving')
      schedule.value = detail.value.schedule
    outputs.value = detail.value.outputs
    if (!outputs.value.some((output) => output.id === selectedOutputID.value)) {
      selectedOutputID.value =
        outputs.value.find((output) => output.latest !== null)?.id ??
        outputs.value[0]?.id ??
        ''
    }
    state.value = 'ready'
  }

  // Bumped whenever the loaded detail is discarded (a new output picked, or a
  // mutation about to re-fetch). A load-in-flight from before the bump is for
  // an output the screen no longer shows: loadOnce checks this at resolution
  // time so that late answer is dropped instead of overwriting the state of
  // whatever is selected now.
  const detailRequestKey = ref(0)

  const selectOutput = (id: string): void => {
    if (id === selectedOutputID.value) return
    selectedOutputID.value = id
    forgetLoadedDetail()
  }

  const forgetLoadedDetail = (): void => {
    detailRequestKey.value += 1
    content.value = null
    contentState.value = 'idle'
    diagnostics.value = []
    diagnosticsState.value = 'idle'
  }

  // The one loading automaton both content and diagnostics run: guard while
  // already loading or loaded, mark loading, fetch, and land on 'ready' or
  // 'failed' — but only if nothing has forgotten this detail meanwhile.
  const loadOnce = async <T>(
    loadState: Ref<LoadState>,
    fetcher: () => Promise<T>,
    assign: (value: T) => void,
  ): Promise<void> => {
    if (loadState.value === 'loading' || loadState.value === 'ready') return
    loadState.value = 'loading'
    const key = detailRequestKey.value
    try {
      const value = await fetcher()
      if (detailRequestKey.value !== key) return
      assign(value)
      loadState.value = 'ready'
    } catch {
      if (detailRequestKey.value !== key) return
      loadState.value = 'failed'
    }
  }

  const openContent = async (): Promise<void> => {
    if (latest.value === null) return
    const artifactID = latest.value.id
    await loadOnce(
      contentState,
      () => loadArtifactContent(artifactID),
      (value) => {
        content.value = value
      },
    )
  }

  const openDiagnostics = async (): Promise<void> => {
    if (snapshotID.value === '') return
    const id = snapshotID.value
    await loadOnce(
      diagnosticsState,
      () => loadDiagnostics(id),
      (value) => {
        diagnostics.value = value
      },
    )
  }

  /**
   * publish refreshes the profile once and rebuilds the outputs named. A failure
   * never removes a published file — the previous artifact stays and the screen
   * says the shown file is the previous verified one.
   */
  const publish = async (ids: string[]): Promise<boolean> => {
    if (ids.length === 0) return true
    try {
      await refreshProfile(profileID())
    } catch {
      rebuildFailed.value = true
      return false
    }
    let complete = true
    for (const id of ids) {
      try {
        const built = await buildOutput(id)
        publishedProfile.publish({
          ...built,
          stale: false,
          subscriptionURL:
            built.subscriptionURL ||
            publishedProfile.forOutput(id)?.subscriptionURL ||
            '',
        })
      } catch {
        complete = false
      }
    }
    rebuildFailed.value = !complete
    return complete
  }

  const rebuild = async (): Promise<void> => {
    if (busy.value || outputs.value.length === 0) return
    work.value = 'rebuilding'
    rebuildFailed.value = false
    const done = await publish(outputs.value.map((output) => output.id))
    forgetLoadedDetail()
    await initialize()
    work.value = done ? 'idle' : 'failed'
  }

  /**
   * save stores a new name and composition and republishes every output, so a
   * saved profile and its files never quietly disagree.
   */
  const save = async (
    name: string,
    composition: ProfileComposition,
  ): Promise<boolean> => {
    if (busy.value || profile.value === null) return false
    work.value = 'saving'
    rebuildFailed.value = false
    try {
      await updateProfile(profileID(), name, composition)
    } catch {
      work.value = 'failed'
      return false
    }
    const done = await publish(outputs.value.map((output) => output.id))
    forgetLoadedDetail()
    await initialize()
    work.value = done ? 'idle' : 'failed'
    return true
  }

  /**
   * setSchedule records this profile's own refresh rule. It does not refresh: the
   * operator chose when, not now, and refreshing on the click would be a second
   * unrequested trip to the network.
   */
  const setSchedule = async (interval: RefreshInterval): Promise<boolean> => {
    if (scheduleState.value === 'saving' || archived.value) return false
    const id = profileID()
    scheduleRevision += 1
    requestedInterval.value = interval
    scheduleState.value = 'saving'
    try {
      const saved = await saveProfileRefreshInterval(id, interval)
      if (profileID() !== id) return false
      schedule.value = saved
      scheduleState.value = 'idle'
      return true
    } catch {
      if (profileID() === id) scheduleState.value = 'failed'
      return false
    }
  }

  const retrySchedule = (): Promise<boolean> =>
    setSchedule(requestedInterval.value)

  /**
   * setArchived takes the profile off the shelf or puts it back. Neither direction
   * refreshes or rebuilds: archiving must not touch the file subscribers are
   * receiving, and restoring returns the profile exactly as it left.
   */
  const setArchived = async (next: boolean): Promise<boolean> => {
    if (busy.value || profile.value === null) return false
    work.value = 'archiving'
    try {
      await (next ? archiveProfile(profileID()) : restoreProfile(profileID()))
      await initialize()
      work.value = 'idle'
      return true
    } catch {
      work.value = 'failed'
      await initialize()
      return false
    }
  }

  /**
   * bind adds a format to this profile and publishes it at once, because a format
   * with no file is a promise the screen cannot keep. The subscription URL it
   * returns is shown once and never read back.
   */
  const bind = async (targetID: string): Promise<boolean> => {
    if (busy.value) return false
    work.value = 'adding'
    rebuildFailed.value = false
    try {
      const created = await addOutput(profileID(), targetID)
      await refreshProfile(profileID())
      const built = await buildOutput(created.output.id)
      publishedProfile.publish({
        ...built,
        stale: false,
        subscriptionURL: built.subscriptionURL,
      })
      selectedOutputID.value = created.output.id
      forgetLoadedDetail()
      await initialize()
      work.value = 'idle'
      return true
    } catch {
      work.value = 'failed'
      await initialize()
      return false
    }
  }

  const bindDevice = async (
    outputID: string,
    deviceID: string,
  ): Promise<boolean> => {
    if (busy.value) return false
    work.value = 'binding'
    try {
      await setOutputDevice(outputID, deviceID)
      await initialize()
      work.value = 'idle'
      return true
    } catch {
      work.value = 'failed'
      return false
    }
  }

  const download = async (formatID: string): Promise<boolean> => {
    if (exporting.value !== '') return false
    exporting.value = formatID
    exportFailed.value = false
    try {
      await downloadProfileExport(profileID(), formatID)
      return true
    } catch {
      exportFailed.value = true
      return false
    } finally {
      exporting.value = ''
    }
  }

  const reveal = (): void => {
    if (subscriptionURL.value !== '') revealed.value = true
  }

  const listTitle = (id: string): string =>
    catalog.value?.listDetails.find((detail) => detail.id === id)?.title ?? id

  /**
   * registerCatalog replaces the whole copy this screen holds rather than
   * patching it: a membership change reaches lists, categories and the
   * profiles that follow them at once, and this screen owns exactly one copy of
   * that fact. Editing a profile writes nothing global (ADR 0029), so the change
   * always came from «Списки» in another tab.
   */
  const registerCatalog = (next: Catalog): void => {
    catalog.value = next
  }

  const catalogRefresh = useFreshCatalog(
    registerCatalog,
    () => busy.value,
    profileID,
  )

  return {
    catalogRefresh,
    archived,
    availableTargets,
    bind,
    bindDevice,
    busy,
    catalog,
    content,
    contentState,
    degradedSources,
    deployable,
    devices,
    diagnostics,
    diagnosticsState,
    download,
    exportFailed,
    exportFormats,
    exporting,
    failedOutputs,
    fresh,
    initialize,
    latest,
    lineCount,
    profile,
    maskedSubscription,
    missingCategories,
    schedule,
    scheduleState,
    retrySchedule,
    setSchedule,
    openContent,
    openDiagnostics,
    outputs,
    outputTitle,
    resolved,
    partialCoverageCount,
    rebuild,
    rebuildFailed,
    registerCatalog,
    reveal,
    revealed,
    ruleCount,
    rulesByList,
    save,
    selectedOutput,
    selectedAttempt,
    selectedOutputID,
    selectOutput,
    listTitle,
    listTitles,
    setArchived,
    snapshotID,
    stale,
    state,
    subscriptionURL,
    targetGroups,
    targetTitle,
    work,
  }
}
