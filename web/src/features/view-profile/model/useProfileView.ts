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
import {
  loadDeployableTargets,
  type ConnectionRequirements,
  type DeployableTarget,
} from '@/shared/api/deploy'
import { loadDevices, type DeviceCard } from '@/shared/api/devices'
import {
  downloadProfileExport,
  loadExportFormats,
  type ExportFormat,
} from '@/shared/api/exports'
import {
  failureKind,
  RoutevaneAPIError,
  type FailureKind,
} from '@/shared/api/http'
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
  reissueSubscription,
  removeOutput,
  setOutputDevice,
  type OutputCard,
} from '@/shared/api/outputs'

export type ProfileState = 'loading' | 'ready' | 'missing' | 'failed'
type ProfileDetail = Awaited<ReturnType<typeof loadProfile>>
type DeviceRegistry = Awaited<ReturnType<typeof loadDevices>>
export type AutomaticDeliveryCapability =
  'available' | 'unavailable' | 'unknown'
export type LoadState = 'idle' | 'loading' | 'ready' | 'failed'
export type WorkState =
  | 'idle'
  | 'saving'
  | 'adding'
  | 'binding'
  | 'rebuilding'
  | 'archiving'
  | 'reissuing'
  | 'removing'
  | 'failed'

// Links reissued in this tab, keyed by output. A reissue answers with a link
// and nothing else, so it cannot be filed as a build; it is the same one-time
// secret, held in memory for the life of the tab and never anywhere else.
const reissuedLinks = ref<Record<string, string>>({})

/**
 * The message a refused action on one format states. Server English never
 * reaches the operator: a code the screen knows is named, an absent format is
 * a fact, and everything else is the common failure of that request.
 */
export const outputActionError = (error: unknown): string => {
  if (
    !(error instanceof RoutevaneAPIError) ||
    failureKind(error) === 'unreachable'
  )
    return 'failure.unreachable.body'
  if (error.code === 'profile_archived') return 'outputs.error.archived'
  if (error.code === 'subscription_not_issued')
    return 'outputs.reissue.error.notIssued'
  if (error.status === 404) return 'outputs.error.missing'
  return 'error.unexpected'
}

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
  // What a failed first read was, so the screen words it by what happened.
  const failure = ref<FailureKind>('unreachable')
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
  const connectionRequirements = ref(new Map<string, ConnectionRequirements>())
  const secretStoreAvailable = ref<boolean | null>(null)
  const devices = ref<DeviceCard[]>([])
  let initializeRequest = 0
  // The profile whose detail the screen currently holds, and whether the last
  // read of it failed while that detail stayed on screen.
  let loadedID = ''
  const rereadFailed = ref(false)
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
    [
      'saving',
      'adding',
      'binding',
      'rebuilding',
      'archiving',
      'reissuing',
      'removing',
    ].includes(work.value),
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
  const subscriptionURL = computed(
    () =>
      reissuedLinks.value[selectedOutputID.value] ??
      fresh.value?.subscriptionURL ??
      '',
  )
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

  // Formats that are not on this profile yet but are kept out of the picker by
  // the operator's own choice in Settings: what an empty picker says depends
  // on whether anything was merely hidden.
  const hiddenTargetCount = computed<number>(() => {
    const bound = new Set(outputs.value.map((output) => output.targetID))
    return (catalog.value?.targets ?? []).filter(
      (target) =>
        !bound.has(target.id) &&
        preferences.hiddenTargets.value.includes(target.id),
    ).length
  })

  const deployable = (output: OutputCard): boolean =>
    output.latest !== null && deployableIDs.value.has(output.targetID)

  // Consent can survive a copied database while the operating system's stored
  // password cannot. Check current requirements and platform support separately
  // from that flag; neither condition limits a manual send with its own password.
  const automaticDelivery = (
    device: DeviceCard,
  ): AutomaticDeliveryCapability => {
    const requirements = connectionRequirements.value.get(device.targetID)
    if (!device.deployable || requirements === undefined) return 'unknown'
    if (!requirements.needsCredential) return 'available'
    if (secretStoreAvailable.value === null) return 'unknown'
    return secretStoreAvailable.value ? 'available' : 'unavailable'
  }

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

  // Whether this read refreshes the profile already on screen. Only the first
  // read of a profile is a loading state, and only it starts from nothing.
  const beginRead = (id: string): boolean => {
    const reread =
      state.value === 'ready' && loadedID === id && profile.value !== null
    if (!reread) {
      state.value = 'loading'
      rereadFailed.value = false
      devices.value = []
      connectionRequirements.value = new Map()
      secretStoreAvailable.value = null
    }
    return reread
  }

  // A capability claim is never carried past a read that failed to confirm it:
  // automatic delivery falls back to unknown, while the devices already listed
  // stay on screen.
  const applyDelivery = (
    deployables: PromiseSettledResult<DeployableTarget[]>,
    registry: PromiseSettledResult<DeviceRegistry>,
  ): void => {
    if (deployables.status === 'fulfilled') {
      deployableIDs.value = new Set(
        deployables.value.map((entry) => entry.targetID),
      )
      connectionRequirements.value = new Map(
        deployables.value.map((entry) => [entry.targetID, entry.requirements]),
      )
    } else {
      connectionRequirements.value = new Map()
    }
    if (registry.status === 'fulfilled') {
      devices.value = registry.value.devices
      secretStoreAvailable.value = registry.value.secretStoreAvailable
    } else {
      secretStoreAvailable.value = null
    }
  }

  const applyDetail = (detail: ProfileDetail, revision: number): void => {
    rereadFailed.value = false
    profile.value = detail.profile
    resolved.value = detail.resolved
    missingCategories.value = detail.missingCategories
    if (revision === scheduleRevision && scheduleState.value !== 'saving')
      schedule.value = detail.schedule
    outputs.value = detail.outputs
    if (!outputs.value.some((output) => output.id === selectedOutputID.value)) {
      selectedOutputID.value =
        outputs.value.find((output) => output.latest !== null)?.id ??
        outputs.value[0]?.id ??
        ''
    }
    state.value = 'ready'
  }

  /**
   * initialize reads the profile and everything its facets show. Every write on
   * this screen reads the profile again, and that read keeps the page it
   * refreshes mounted — the facets not on show hold the operator's unsaved
   * drafts — replacing values as they arrive. A re-read that fails leaves the
   * last confirmed profile on screen and says so; only a profile the server no
   * longer has stops being shown.
   */
  const initialize = async (): Promise<void> => {
    const request = ++initializeRequest
    const id = profileID()
    const revision = scheduleRevision
    const reread = beginRead(id)
    const [detail, loadedCatalog, deployables, formats, registry] =
      await Promise.allSettled([
        loadProfile(id),
        loadCatalogCached(),
        loadDeployableTargets(),
        loadExportFormats(),
        loadDevices(),
      ])
    if (request !== initializeRequest || id !== profileID()) return
    if (loadedCatalog.status === 'fulfilled')
      catalog.value = loadedCatalog.value
    applyDelivery(deployables, registry)
    if (formats.status === 'fulfilled') exportFormats.value = formats.value
    if (detail.status === 'fulfilled') {
      loadedID = id
      applyDetail(detail.value, revision)
      return
    }
    // A profile the server has never heard of (404) is a different fact from a
    // profile the server cannot answer for right now: only the first is
    // "missing". Everything else — 5xx, a network failure, a contract the
    // client cannot decode — leaves the profile possibly still there, so the
    // screen offers retry instead of "not found".
    const missing =
      detail.reason instanceof RoutevaneAPIError && detail.reason.status === 404
    if (reread && !missing) rereadFailed.value = true
    else if (missing) state.value = 'missing'
    else {
      failure.value = failureKind(detail.reason)
      state.value = 'failed'
    }
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

  /** rebuildOutput refreshes the profile and rebuilds one format only. */
  const rebuildOutput = async (outputID: string): Promise<boolean> => {
    if (busy.value || archived.value) return false
    work.value = 'rebuilding'
    rebuildFailed.value = false
    const done = await publish([outputID])
    forgetLoadedDetail()
    await initialize()
    work.value = done ? 'idle' : 'failed'
    return done
  }

  /**
   * reissue withdraws a format's link and shows its replacement once, exactly
   * as a first publication does. Its answer is a message key on refusal, or
   * null once the new link is on screen.
   */
  const reissue = async (outputID: string): Promise<string | null> => {
    if (busy.value) return 'error.unexpected'
    work.value = 'reissuing'
    try {
      const link = await reissueSubscription(outputID)
      reissuedLinks.value = { ...reissuedLinks.value, [outputID]: link }
      selectOutput(outputID)
      return null
    } catch (error) {
      return outputActionError(error)
    } finally {
      work.value = 'idle'
    }
  }

  /**
   * remove retires one format for good (ADR 0051). The profile is read again,
   * because the format, its link and its binding are gone from the server.
   */
  const remove = async (outputID: string): Promise<string | null> => {
    if (busy.value) return 'error.unexpected'
    work.value = 'removing'
    try {
      await removeOutput(outputID)
    } catch (error) {
      work.value = 'idle'
      return outputActionError(error)
    }
    reissuedLinks.value = Object.fromEntries(
      Object.entries(reissuedLinks.value).filter(([id]) => id !== outputID),
    )
    forgetLoadedDetail()
    await initialize()
    work.value = 'idle'
    return null
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
    automaticDelivery,
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
    failure,
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
    rebuildOutput,
    reissue,
    remove,
    registerCatalog,
    rereadFailed,
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
    hiddenTargetCount,
    targetGroups,
    targetTitle,
    work,
  }
}
