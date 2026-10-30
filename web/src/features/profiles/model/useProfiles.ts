import { computed, ref } from 'vue'

import { insertNewestFirst } from '@/features/profiles/lib/profileOrder'
import { usePublishedProfile } from '@/entities/profile-build/model/publishedProfile'
import { loadArtifactContent } from '@/shared/api/artifacts'
import {
  loadCatalogCached,
  localizedTargetTitle,
  type Catalog,
} from '@/shared/api/catalog'
import { loadDeployableTargets } from '@/shared/api/deploy'
import { loadDevices } from '@/shared/api/devices'
import { useLocale } from '@/shared/i18n/useLocale'
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
  deleteProfile,
  loadProfile,
  loadProfilePage,
  refreshProfile,
  restoreProfile,
  type ProfileCard,
  type ProfileShelf,
} from '@/shared/api/profiles'
import { buildOutput, type OutputCard } from '@/shared/api/outputs'

export type LibraryState = 'loading' | 'ready' | 'empty' | 'failed'

/** Why a permanent deletion did not happen, in terms a screen words itself. */
export type DeleteFailure = '' | 'notArchived' | 'gone' | 'failed'

/**
 * The library: every profile the service stores, newest first. The server is the
 * source of truth — nothing here survives in browser storage, so two tabs and
 * two browsers see the same shelf.
 *
 * The library is read one shelf at a time. The server filters before it bounds
 * the page, so the archive is as complete as the active shelf, and it states how
 * many profiles are archived whichever shelf was read: the count is the
 * library's, not the number of rows this tab happens to hold.
 *
 * The catalog, the connections and the deployment surface only decorate the rows
 * (list titles, connection names, which outputs can offer "send"). Their failure
 * degrades the decoration, never the shelf itself.
 */
export const useProfiles = () => {
  const { locale } = useLocale()
  const published = usePublishedProfile()
  const cards = ref<ProfileCard[]>([])
  const shelf = ref<ProfileShelf>('active')
  const archivedCount = ref(0)
  const state = ref<LibraryState>('loading')
  // What a failed first read was, so the screen words it by what happened.
  const failure = ref<FailureKind>('unreachable')
  const catalog = ref<Catalog | null>(null)
  const deployableIDs = ref<Set<string>>(new Set())
  const connectionNames = ref<Map<string, string>>(new Map())
  const copiedID = ref('')
  const copyFailedID = ref('')
  const shelving = ref('')
  // A refused archive or restore, named by the profile and the direction it
  // was asked to go.
  const shelveFailed = ref<{ id: string; archive: boolean } | null>(null)
  // The profile the operator just archived, kept so the shelf can offer to put
  // it back. It is withdrawn by the next action like every other outcome.
  const justArchived = ref<ProfileCard | null>(null)
  const justRestored = ref<ProfileCard | null>(null)
  const justDeleted = ref<ProfileCard | null>(null)
  const deleting = ref('')
  const deleteFailure = ref<DeleteFailure>('')
  // The code of the last refused deletion, so a refusal at a router can say
  // which one and why.
  const deleteRefusal = ref('')
  const rebuilding = ref('')
  const rebuiltID = ref('')
  const rebuildFailedID = ref('')
  const exportFormats = ref<ExportFormat[]>([])
  const exporting = ref('')
  const exportFailedID = ref('')
  const nextCursor = ref('')
  const loadingMore = ref(false)
  const moreFailed = ref(false)
  // Each initial read, shelf change or archival change supersedes a pending
  // continuation. A response that no longer describes this shelf must not
  // append itself.
  let readGeneration = 0

  // The rows of the shelf being read. Archived rows are in this one table too:
  // the filter decides which half of the library it holds.
  const rows = computed(() => cards.value)

  const settle = (): void => {
    state.value =
      cards.value.length === 0 && archivedCount.value === 0 ? 'empty' : 'ready'
  }

  // The shelf's first page. A shelf that ended up empty while the archive is
  // not is a legitimate screen ("every profile is archived"), not an empty
  // library.
  const readFirstPage = async (generation: number): Promise<boolean> => {
    try {
      const page = await loadProfilePage(shelf.value)
      if (generation !== readGeneration) return false
      cards.value = page.profiles
      nextCursor.value = page.nextCursor
      archivedCount.value = page.archivedCount
      settle()
      return true
    } catch (error) {
      if (generation === readGeneration) {
        failure.value = failureKind(error)
        state.value = 'failed'
      }
      return false
    }
  }

  const initialize = async (): Promise<void> => {
    const generation = ++readGeneration
    state.value = 'loading'
    nextCursor.value = ''
    loadingMore.value = false
    moreFailed.value = false
    const [, loadedCatalog, deployables, formats, devices] =
      await Promise.allSettled([
        readFirstPage(generation),
        loadCatalogCached(),
        loadDeployableTargets(),
        loadExportFormats(),
        loadDevices(),
      ])
    if (generation !== readGeneration) return
    if (loadedCatalog.status === 'fulfilled') {
      catalog.value = loadedCatalog.value
    }
    if (deployables.status === 'fulfilled') {
      deployableIDs.value = new Set(
        deployables.value.map((entry) => entry.targetID),
      )
    }
    if (formats.status === 'fulfilled') exportFormats.value = formats.value
    if (devices.status === 'fulfilled') {
      connectionNames.value = new Map(
        devices.value.devices.map((device) => [device.id, device.name]),
      )
    }
  }

  // The shelf states the outcome of the last action taken on it, so each
  // action starts by withdrawing whatever the previous one reported: an earlier
  // success never stands in for a later failure.
  const clearOutcomes = (): void => {
    copiedID.value = ''
    copyFailedID.value = ''
    exportFailedID.value = ''
    shelveFailed.value = null
    justArchived.value = null
    justRestored.value = null
    justDeleted.value = null
    rebuiltID.value = ''
    rebuildFailedID.value = ''
  }

  /**
   * selectShelf swaps the half of the library the table shows. The previous
   * rows are withdrawn first: they belong to the other half, and the loading
   * state says the new half is on its way.
   */
  const selectShelf = async (
    next: ProfileShelf,
    keepOutcome = false,
  ): Promise<void> => {
    if (next === shelf.value && state.value !== 'failed') return
    const generation = ++readGeneration
    shelf.value = next
    // A change the operator made to the shelf reports itself; a change the
    // shelf made on its own — leaving an archive that just emptied — must not
    // withdraw that report.
    if (!keepOutcome) clearOutcomes()
    state.value = 'loading'
    cards.value = []
    nextCursor.value = ''
    loadingMore.value = false
    moreFailed.value = false
    await readFirstPage(generation)
  }

  // A failed continuation preserves already confirmed rows and its cursor, so
  // retry asks for exactly the page that did not arrive instead of sending the
  // operator back to the start of a long library.
  const loadMore = async (): Promise<boolean> => {
    if (nextCursor.value === '' || loadingMore.value) return false
    const generation = readGeneration
    const cursor = nextCursor.value
    loadingMore.value = true
    moreFailed.value = false
    try {
      const page = await loadProfilePage(shelf.value, cursor)
      if (generation !== readGeneration || nextCursor.value !== cursor)
        return false
      cards.value = [...cards.value, ...page.profiles]
      nextCursor.value = page.nextCursor
      archivedCount.value = page.archivedCount
      return true
    } catch {
      if (generation === readGeneration) moreFailed.value = true
      return false
    } finally {
      if (generation === readGeneration) loadingMore.value = false
    }
  }

  const listTitle = (id: string): string =>
    catalog.value?.listDetails.find((detail) => detail.id === id)?.title ?? id

  // The row states what the profile publishes, not how it was written: a
  // reference and a named list are the same thing to whoever reads the
  // library.
  const composition = (card: ProfileCard): string =>
    card.resolved.map(listTitle).join(', ')

  // A row names its formats in the reader's language where the catalog offers
  // one, and keeps the stored title when the target has left the catalog.
  const outputTitle = (output: OutputCard): string =>
    localizedTargetTitle(
      catalog.value?.targets.find((target) => target.id === output.targetID),
      locale.value,
      output.targetTitle,
    )

  // The connection a format delivers to, when it has one and the registry could
  // be read. An unreadable registry degrades the name, never the format.
  const connectionName = (output: OutputCard): string =>
    output.deviceID === ''
      ? ''
      : (connectionNames.value.get(output.deviceID) ?? '')

  const publishedOutputs = (card: ProfileCard): OutputCard[] =>
    card.outputs.filter((output) => output.latest !== null)

  // The first published output anchors the row timestamp. Copy and download
  // choices are enumerated by the view, so several formats never collapse into
  // an arbitrary default.
  const downloadable = (card: ProfileCard): OutputCard | null =>
    publishedOutputs(card)[0] ?? null

  const deployableOutputs = (card: ProfileCard): OutputCard[] =>
    publishedOutputs(card).filter((output) =>
      deployableIDs.value.has(output.targetID),
    )

  // A format whose latest attempt failed is a failure the row must state, with
  // or without an earlier published file behind it.
  const buildFailed = (card: ProfileCard): boolean =>
    card.outputs.some((output) => output.lastAttempt?.status === 'failed')

  // A rebuild only offers itself where every format has already been built: the
  // first successful build issues a subscription link that is shown once, and a
  // shelf row has nowhere to show it.
  const rebuildable = (card: ProfileCard): boolean =>
    card.outputs.length > 0 &&
    card.outputs.every((output) => output.latest !== null)

  const withdraw = (card: ProfileCard): void => {
    cards.value = cards.value.filter((current) => current.id !== card.id)
  }

  // An archive left empty has nothing to show. The operator moves back to the
  // shelf that holds profiles instead of staring at an empty filter.
  const leaveEmptyArchive = async (): Promise<void> => {
    if (shelf.value === 'archived' && archivedCount.value === 0)
      await selectShelf('active', true)
  }

  // Archiving is offered on the row and restoring where the archived profile is
  // found. Neither rebuilds: what the profile publishes is what it published when
  // it was archived, and changing that is the operator's next decision.
  const setArchived = async (
    card: ProfileCard,
    next: boolean,
  ): Promise<boolean> => {
    if (shelving.value !== '') return false
    shelving.value = card.id
    clearOutcomes()
    // The archive response changes the page split, so a continuation begun
    // before this action no longer belongs beside the confirmed shelf.
    readGeneration += 1
    loadingMore.value = false
    moreFailed.value = false
    try {
      const profile = await (next
        ? archiveProfile(card.id)
        : restoreProfile(card.id))
      const moved: ProfileCard = {
        ...card,
        archivedAt: profile.archivedAt,
        updatedAt: profile.updatedAt,
      }
      if (next) {
        withdraw(card)
        archivedCount.value += 1
        justArchived.value = moved
      } else {
        archivedCount.value = Math.max(0, archivedCount.value - 1)
        justRestored.value = moved
        if (shelf.value === 'archived') withdraw(card)
        else cards.value = insertNewestFirst(cards.value, moved)
        await leaveEmptyArchive()
      }
      settle()
      return true
    } catch {
      shelveFailed.value = { archive: next, id: card.id }
      return false
    } finally {
      shelving.value = ''
    }
  }

  /**
   * Deleting is permanent and only an archived profile can be deleted; the
   * server holds that rule and answers it with a code, which is what the screen
   * words. A profile the server no longer knows is gone either way, so its row
   * leaves with the same count correction a deletion makes.
   */
  const removeForever = async (
    card: ProfileCard,
    removeFromDevices = false,
  ): Promise<boolean> => {
    if (deleting.value !== '') return false
    deleting.value = card.id
    deleteFailure.value = ''
    clearOutcomes()
    readGeneration += 1
    loadingMore.value = false
    moreFailed.value = false
    try {
      await deleteProfile(card.id, removeFromDevices)
    } catch (error) {
      deleteRefusal.value =
        error instanceof RoutevaneAPIError ? (error.code ?? '') : ''
      if (error instanceof RoutevaneAPIError && error.status === 404) {
        deleteFailure.value = 'gone'
      } else if (
        error instanceof RoutevaneAPIError &&
        error.code === 'profile_not_archived'
      ) {
        deleteFailure.value = 'notArchived'
      } else {
        deleteFailure.value = 'failed'
        deleting.value = ''
        return false
      }
      deleting.value = ''
      if (deleteFailure.value === 'gone') {
        // The profile is deleted, whoever did it: the row leaves and the shelf
        // says so rather than letting it vanish.
        justDeleted.value = card
        withdraw(card)
        archivedCount.value = Math.max(0, archivedCount.value - 1)
        settle()
        await leaveEmptyArchive()
      }
      return false
    }
    withdraw(card)
    archivedCount.value = Math.max(0, archivedCount.value - 1)
    justDeleted.value = card
    deleting.value = ''
    settle()
    await leaveEmptyArchive()
    return true
  }

  /**
   * The outcome belongs to the row that produced it and stays on it: a shelf of
   * profiles has to say which one is on the clipboard, and a confirmation that
   * expires on a timer answers that only for a moment.
   *
   * The write goes to the platform clipboard directly. VueUse's `useClipboard`
   * was tried and rejected: a refused asynchronous write falls through to
   * `document.execCommand('copy')`, whose result it ignores, and it resolves as
   * a success regardless — a row would then say "copied" for a clipboard that
   * refused. The row states what happened, so the refusal is the browser's own.
   *
   * The copied file is the format the operator chose, never an arbitrary first
   * one.
   */
  const copyContents = async (
    card: ProfileCard,
    output: OutputCard,
  ): Promise<void> => {
    clearOutcomes()
    if (output.latest === null) return
    // A browser with no clipboard to write to says so on the row, instead of
    // confirming something that did not happen.
    const target: Clipboard | undefined =
      typeof navigator === 'undefined' ? undefined : navigator.clipboard
    if (target === undefined) {
      copyFailedID.value = card.id
      return
    }
    try {
      const content = await loadArtifactContent(output.latest.id)
      await target.writeText(content.text)
      copiedID.value = card.id
    } catch {
      copyFailedID.value = card.id
    }
  }

  const exportFile = async (
    card: ProfileCard,
    formatID: string,
  ): Promise<void> => {
    if (exporting.value !== '') return
    exporting.value = `${card.id}:${formatID}`
    clearOutcomes()
    try {
      await downloadProfileExport(card.id, formatID)
    } catch {
      exportFailedID.value = card.id
    } finally {
      exporting.value = ''
    }
  }

  /**
   * Rebuild refreshes the profile once and rebuilds each of its formats, the
   * way the profile page does, then re-reads the row so the content time and
   * any failure it states are the server's. A failure removes no file: the
   * previous artifact stays published.
   */
  const rebuild = async (card: ProfileCard): Promise<void> => {
    if (rebuilding.value !== '' || !rebuildable(card)) return
    rebuilding.value = card.id
    clearOutcomes()
    let complete = true
    try {
      await refreshProfile(card.id)
      for (const output of card.outputs) {
        try {
          const built = await buildOutput(output.id)
          published.publish({
            ...built,
            stale: false,
            subscriptionURL:
              built.subscriptionURL ||
              published.forOutput(output.id)?.subscriptionURL ||
              '',
          })
        } catch {
          complete = false
        }
      }
    } catch {
      complete = false
    }
    try {
      const detail = await loadProfile(card.id)
      cards.value = cards.value.map((current) =>
        current.id === card.id
          ? {
              ...current,
              ...detail.profile,
              resolved: detail.resolved,
              missingCategories: detail.missingCategories,
              outputs: detail.outputs,
            }
          : current,
      )
    } catch {
      // The row keeps what it showed; the outcome below still says what the
      // rebuild did.
    }
    rebuilding.value = ''
    if (complete) rebuiltID.value = card.id
    else rebuildFailedID.value = card.id
  }

  return {
    archivedCount,
    composition,
    connectionName,
    buildFailed,
    copiedID,
    copyContents,
    copyFailedID,
    deleteFailure,
    deleteRefusal,
    deleting,
    deployableOutputs,
    downloadable,
    exportFailedID,
    exportFile,
    exportFormats,
    exporting,
    failure,
    initialize,
    justArchived,
    justDeleted,
    justRestored,
    loadMore,
    loadingMore,
    moreFailed,
    nextCursor,
    outputTitle,
    publishedOutputs,
    rebuild,
    rebuildable,
    rebuildFailedID,
    rebuiltID,
    rebuilding,
    removeForever,
    rows,
    selectShelf,
    setArchived,
    shelf,
    shelveFailed,
    shelving,
    state,
  }
}
