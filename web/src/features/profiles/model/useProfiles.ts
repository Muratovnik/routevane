import { computed, ref } from 'vue'

import { loadArtifactContent } from '@/shared/api/artifacts'
import {
  loadCatalogCached,
  localizedTargetTitle,
  type Catalog,
} from '@/shared/api/catalog'
import { loadDeployableTargets } from '@/shared/api/deploy'
import { useLocale } from '@/shared/i18n/useLocale'
import {
  downloadProfileExport,
  loadExportFormats,
  type ExportFormat,
} from '@/shared/api/exports'
import {
  archiveProfile,
  isArchived,
  loadProfilePage,
  restoreProfile,
  type ProfileCard,
} from '@/shared/api/profiles'
import type { OutputCard } from '@/shared/api/outputs'

export type LibraryState = 'loading' | 'ready' | 'empty' | 'failed'

/**
 * The library: every profile the service stores, newest first. The server is the
 * source of truth — nothing here survives in browser storage, so two tabs and
 * two browsers see the same shelf.
 *
 * The catalog and the deployment surface only decorate the rows (list
 * titles, which outputs can offer "send"). Their failure degrades the
 * decoration, never the shelf itself.
 */
export const useProfiles = () => {
  const { locale } = useLocale()
  const cards = ref<ProfileCard[]>([])
  const state = ref<LibraryState>('loading')
  const catalog = ref<Catalog | null>(null)
  const deployableIDs = ref<Set<string>>(new Set())
  const copiedID = ref('')
  const copyFailedID = ref('')
  const shelving = ref('')
  const exportFormats = ref<ExportFormat[]>([])
  const exporting = ref('')
  const exportFailedID = ref('')
  const nextCursor = ref('')
  const loadingMore = ref(false)
  const moreFailed = ref(false)
  // Each initial read or archival change supersedes a pending continuation.
  // A response that no longer describes this library must not append itself.
  let readGeneration = 0

  // The shelf and the archive are one server read split here. An archived profile
  // leaves the main view without leaving the library: its file and its
  // subscription are still working, and an operator who cannot find the row
  // cannot find them either.
  const rows = computed(() => cards.value.filter((card) => !isArchived(card)))
  const archived = computed(() => cards.value.filter(isArchived))

  const initialize = async (): Promise<void> => {
    const generation = ++readGeneration
    state.value = 'loading'
    nextCursor.value = ''
    loadingMore.value = false
    moreFailed.value = false
    const [profiles, loadedCatalog, deployables, formats] =
      await Promise.allSettled([
        loadProfilePage(),
        loadCatalogCached(),
        loadDeployableTargets(),
        loadExportFormats(),
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
    if (profiles.status === 'rejected') {
      state.value = 'failed'
      return
    }
    cards.value = profiles.value.profiles
    nextCursor.value = profiles.value.nextCursor
    state.value = profiles.value.profiles.length === 0 ? 'empty' : 'ready'
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
      const page = await loadProfilePage(cursor)
      if (generation !== readGeneration || nextCursor.value !== cursor)
        return false
      cards.value = [...cards.value, ...page.profiles]
      nextCursor.value = page.nextCursor
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

  // The first published output anchors the row timestamp and the copy fallback.
  // Download choices themselves are enumerated by the view, so several formats
  // never collapse into an arbitrary default.
  const downloadable = (card: ProfileCard): OutputCard | null =>
    card.outputs.find((output) => output.latest !== null) ?? null

  const deployableOutputs = (card: ProfileCard): OutputCard[] =>
    card.outputs.filter(
      (output) =>
        output.latest !== null && deployableIDs.value.has(output.targetID),
    )

  const sendable = (card: ProfileCard): boolean =>
    deployableOutputs(card).length > 0

  // Archiving is offered on the row and restoring where the archived profile is
  // found. Neither rebuilds: what the profile publishes is what it published when
  // it was archived, and changing that is the operator's next decision.
  const setArchived = async (
    card: ProfileCard,
    next: boolean,
  ): Promise<boolean> => {
    if (shelving.value !== '') return false
    shelving.value = card.id
    // The archive response changes the page split, so a continuation begun
    // before this action no longer belongs beside the confirmed shelf.
    readGeneration += 1
    loadingMore.value = false
    moreFailed.value = false
    try {
      const profile = await (next
        ? archiveProfile(card.id)
        : restoreProfile(card.id))
      cards.value = cards.value.map((current) =>
        current.id === card.id
          ? {
              ...current,
              archivedAt: profile.archivedAt,
              updatedAt: profile.updatedAt,
            }
          : current,
      )
      return true
    } catch {
      return false
    } finally {
      shelving.value = ''
    }
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
   */
  const copyContents = async (card: ProfileCard): Promise<void> => {
    copiedID.value = ''
    copyFailedID.value = ''
    const output = downloadable(card)
    if (output === null || output.latest === null) return
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
    exportFailedID.value = ''
    try {
      await downloadProfileExport(card.id, formatID)
    } catch {
      exportFailedID.value = card.id
    } finally {
      exporting.value = ''
    }
  }

  return {
    archived,
    composition,
    copiedID,
    copyContents,
    copyFailedID,
    deployableOutputs,
    downloadable,
    exportFailedID,
    exportFile,
    exportFormats,
    exporting,
    initialize,
    outputTitle,
    rows,
    sendable,
    listTitle,
    loadMore,
    loadingMore,
    moreFailed,
    nextCursor,
    setArchived,
    shelving,
    state,
  }
}
