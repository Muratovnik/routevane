import { computed, ref, watch } from 'vue'

import {
  addListSource,
  asListDetail,
  createCustomList,
  loadListContents,
  refreshList,
  removeListSource,
  setListSourceEnabled,
  setListValues,
  updateCustomList,
  type DomainVerdict,
} from '@/shared/api/catalog'
import { RoutevaneAPIError } from '@/shared/api/http'
import { useLocale } from '@/shared/i18n/useLocale'
import {
  normalizeDomain,
  parseDestinationList,
} from '@/shared/lib/destinationList'
import { libraryPageHash } from '@/shared/lib/listsHash'
import { useDebouncedMutation } from '@/shared/model/useDebouncedMutation'
import type { ChoiceOption } from '@/shared/ui/types'

import type { ListContents, ListContentsRow, ListDetail } from './types'

export type ListDetailProps = {
  mode: 'compose' | 'library'
  creating?: boolean
  disabled?: boolean
  included?: boolean
  /** The route the footer controls. Blank while a draft has no name yet. */
  profileName?: string
  /**
   * Whether that route is still an unsaved draft. Membership then waits for a
   * save, and the footer names no route: the composer proposes the name from
   * the lists picked, so naming it here would read as naming this list.
   */
  pending?: boolean
  list: ListDetail | null
}

/**
 * What this card reports to whoever opened it. The model states the outcome and
 * the component turns it into a component event, so nothing here depends on how
 * the surface announces itself.
 */
export type ListDetailReport = {
  close: () => void
  created: (detail: ListDetail) => void
  updated: (detail: ListDetail) => void
  /**
   * The operator asked for this list to stop existing. The flow that owns the
   * library confirms it and reports the refusal, so one confirmation and one
   * refusal serve both ways in.
   */
  remove: (detail: ListDetail) => void
}

// The batch endpoint takes at most this many values in one request, so a long
// import is sent in several rather than refused.
const BATCH_LIMIT = 1024

/**
 * Everything one list card reads and writes: its contents, its sources, the
 * drafts the operator types into it, and what each request answered.
 *
 * The card itself is presentation. This is where the requests live, because a
 * component of the entities layer presents what it is given and the slice's
 * model is what talks to the transport.
 *
 * `library` is the curating flow: the name, the entries, the sources and the
 * list's own existence are the subject, and every switch here writes the
 * server. `compose` is the route being built: the same table is only read, and
 * the one act that route owns is the footer — whether the list is in this route
 * at all. Both flows can refresh existing source observations; only the library
 * edits list configuration.
 */
export const useListDetail = (
  props: ListDetailProps,
  report: ListDetailReport,
) => {
  const { t, tc, tor } = useLocale()

  const composing = computed(() => props.mode === 'compose')
  const curating = computed(() => props.mode === 'library')

  // One card, two shapes: an existing list shows its single contents table and
  // its sources; the creation form asks for a name and the first domains.
  const active = computed(() => props.list !== null || props.creating === true)

  // --- creation form -----------------------------------------------------
  const titleDraft = ref('')
  const titleError = ref('')
  const domainsDraft = ref('')
  const domainsError = ref('')
  const saving = ref(false)

  // --- existing list: contents -------------------------------------------
  const contents = ref<ListContents | null>(null)
  const contentsState = ref<'idle' | 'loading' | 'ready' | 'failed'>('idle')
  const refreshing = ref(false)
  const observing = ref(false)
  // Reading the sources is reported where the reading is asked for — the card —
  // while editing which sources there are is reported inside their own panel.
  const refreshError = ref('')
  const refreshSkipped = ref(0)
  const sourceError = ref('')
  const importStatus = ref('')
  const addValuesOpen = ref(false)
  const addValuesDraft = ref('')
  // A refusal about the batch belongs beside the field that carries it, inside
  // the panel it was typed in — not on the table behind that panel.
  const addError = ref('')
  const filter = ref('')
  const sourcesOpen = ref(false)
  const feedURL = ref('')
  const feedFormat = ref('text')
  const feedOpen = ref(false)
  const feedError = ref('')
  let contentsRequest = 0
  // The card reads the sources for the operator, but a card left open is not a
  // polling client: one automatic read per opening.
  let autoObserved = false

  const rows = computed<ListContentsRow[]>(() => contents.value?.rows ?? [])

  // What the list currently offers. Both flows count the same thing: a route
  // takes the list as the library holds it.
  const enabledCount = computed(
    () => effectiveRows.value.filter((row) => row.enabled).length,
  )

  // A list can stand for hundreds of destinations. The field over the table
  // narrows what is drawn by what a row says — its value and where it came from.
  const visibleRows = computed<ListContentsRow[]>(() => {
    const query = filter.value.trim().toLowerCase()
    if (query === '') return effectiveRows.value
    return effectiveRows.value.filter(
      (row) =>
        row.value.toLowerCase().includes(query) ||
        originLabel(row).toLowerCase().includes(query),
    )
  })

  const sources = computed(() => contents.value?.sources ?? [])

  // Entry and source switches are independent writes. The composables keep one
  // optimistic value and one request per resource key, so a slow row never
  // blocks a different row or replaces the card's verified contents.
  const entryMutations = useDebouncedMutation<boolean, ListContents>(
    async (value, enabled) => {
      const list = props.list
      if (list === null) return undefined
      const row = rows.value.find((candidate) => candidate.value === value)
      const verdict: DomainVerdict =
        enabled || row?.origin === 'manual' ? 'auto' : 'exclude'
      return setListValues(list.id, [value], verdict)
    },
    {
      resolve: (next, enabled, value) =>
        next?.rows.find((candidate) => candidate.value === value)?.enabled ??
        enabled,
      onSuccess: ({ result }) => {
        if (result !== undefined) reconcileMutationContents(result)
      },
    },
  )
  const sourceMutations = useDebouncedMutation<boolean, ListContents>(
    async (sourceID, enabled) => {
      const list = props.list
      if (list === null) return undefined
      return setListSourceEnabled(list.id, sourceID, enabled)
    },
    {
      resolve: (next, enabled, sourceID) =>
        next?.sources.find((source) => source.id === sourceID)?.enabled ??
        enabled,
      onSuccess: ({ result }) => {
        if (result !== undefined) reconcileMutationContents(result)
      },
    },
  )

  const effectiveRows = computed<ListContentsRow[]>(() =>
    rows.value.map((row) => {
      const enabled = entryMutations.getValue(row.value)
      return enabled === undefined || enabled === row.enabled
        ? row
        : { ...row, enabled }
    }),
  )
  const effectiveSources = computed(() =>
    sources.value.map((source) => {
      const enabled = sourceMutations.getValue(source.id)
      return enabled === undefined || enabled === source.enabled
        ? source
        : { ...source, enabled }
    }),
  )

  // Before the contents land, the catalog entry already knows how many sources
  // the list has, so the control that opens them never starts at nothing.
  const sourceCount = computed(() =>
    contents.value === null
      ? (props.list?.sourceCount ?? props.list?.sources?.length ?? 0)
      : sources.value.length,
  )

  const feedFormats = computed<ChoiceOption[]>(() => [
    { label: t('listCard.feed.format.text'), value: 'text' },
    { label: t('listCard.feed.format.domainList'), value: 'domain-list' },
    { label: t('listCard.feed.format.json'), value: 'json' },
  ])

  // Reads, destructive actions and dismissal stay guarded while any mutation is
  // queued or in flight. Switches use the narrower guard below so each row can
  // still accept a new last intent while another row is pending.
  const mutationPending = computed(
    () => entryMutations.pending.value || sourceMutations.pending.value,
  )
  const dismissalBlocked = computed(
    () =>
      props.disabled === true ||
      saving.value ||
      refreshing.value ||
      mutationPending.value,
  )
  const interactionBusy = computed(
    () => dismissalBlocked.value || contentsState.value === 'loading',
  )
  const toggleBlocked = computed(
    () =>
      props.disabled === true ||
      saving.value ||
      refreshing.value ||
      contentsState.value === 'loading',
  )

  // Where this list is curated. The draft behind the card is unsaved, so the
  // other flow opens in a tab of its own rather than over it.
  const libraryHref = computed(() => {
    const list = props.list
    if (list === null) return '/lists'
    return `/lists${libraryPageHash({
      category: list.categories[0] ?? '',
      list: list.id,
    })}`
  })

  // The footer names the route it is talking about, unless that route is a draft
  // with no stored name to use.
  const membershipLabel = computed(() => {
    const name = (props.profileName ?? '').trim()
    if (name === '' || props.pending === true) {
      return props.included === true
        ? t('listCard.membership.in.unnamed')
        : t('listCard.membership.out.unnamed')
    }
    return props.included === true
      ? t('listCard.membership.in', { name })
      : t('listCard.membership.out', { name })
  })

  // Declared above the watch that starts it, because that watch runs immediately:
  // the first read of a card begins while this setup is still evaluating. The
  // helpers this reaches after its own `await` are declared below.
  const openContents = async (listID: string): Promise<void> => {
    const request = ++contentsRequest
    contentsState.value = 'loading'
    try {
      const loaded = await loadListContents(listID)
      if (request !== contentsRequest) return
      applyContents(request, loaded)
    } catch {
      if (request !== contentsRequest) return
      contentsState.value = 'failed'
      return
    }
    if (shouldObserve()) {
      // The card may have closed or switched while the contents request was in
      // flight. Do not let a late first read start source work for another card.
      if (request !== contentsRequest) return
      autoObserved = true
      await runRefresh(true)
    }
  }

  watch(
    [() => props.list, () => props.creating],
    ([list, creating]) => {
      contentsRequest += 1
      entryMutations.cancel()
      sourceMutations.cancel()
      titleDraft.value = list?.title ?? ''
      titleError.value = ''
      domainsDraft.value = ''
      domainsError.value = ''
      refreshError.value = ''
      refreshSkipped.value = 0
      sourceError.value = ''
      importStatus.value = ''
      addValuesOpen.value = false
      addValuesDraft.value = ''
      addError.value = ''
      filter.value = ''
      sourcesOpen.value = false
      feedOpen.value = false
      feedURL.value = ''
      feedFormat.value = 'text'
      saving.value = false
      // A switch/close invalidates any request from the previous card. Keep the
      // new card's loading state independent of that stale promise; its finally
      // block is generation-guarded below so an old response cannot clear this
      // card's state.
      refreshing.value = false
      observing.value = false
      contents.value = null
      contentsState.value = 'idle'
      autoObserved = false
      if (list !== null && creating !== true) void openContents(list.id)
    },
    { immediate: true },
  )

  // A card that shows a list before its sources were ever read shows almost
  // nothing, and the operator has no way to know that. So the card reads them
  // itself the first time it is opened on unread contents — in either flow,
  // because reading a source changes no route and no list.
  const shouldObserve = (): boolean => {
    const loaded = contents.value
    return (
      !autoObserved &&
      props.creating !== true &&
      props.list !== null &&
      loaded !== null &&
      !loaded.observed &&
      loaded.sources.some((source) => source.enabled)
    )
  }

  // applyContents lands a mutation's answer, unless the card moved on.
  const applyContents = (request: number, next: ListContents): void => {
    if (request !== contentsRequest) return
    contents.value = next
    for (const row of next.rows) entryMutations.seed(row.value, row.enabled)
    for (const source of next.sources)
      sourceMutations.seed(source.id, source.enabled)
    contentsState.value = 'ready'
  }

  // A toggle response is authoritative for the rows and sources it observed,
  // but it must not erase another key's optimistic intent while that intent is
  // queued or in flight. Keep pending rows/sources that the response omitted so
  // their latest visible value remains available to the effective projections;
  // non-pending omissions (notably a removed manual row) settle immediately.
  const reconcileMutationContents = (next: ListContents): void => {
    const current = contents.value
    if (current === null) {
      contents.value = next
      for (const row of next.rows) entryMutations.seed(row.value, row.enabled)
      for (const source of next.sources)
        sourceMutations.seed(source.id, source.enabled)
      contentsState.value = 'ready'
      return
    }

    const authoritativeRows = new Map(
      next.rows.map((row) => [row.value, row] as const),
    )
    const retainedRows = current.rows.filter(
      (row) =>
        entryMutations.isPending(row.value) &&
        !authoritativeRows.has(row.value),
    )
    const authoritativeSources = new Map(
      next.sources.map((source) => [source.id, source] as const),
    )
    const retainedSources = current.sources.filter(
      (source) =>
        sourceMutations.isPending(source.id) &&
        !authoritativeSources.has(source.id),
    )

    contents.value = {
      ...next,
      rows: [...next.rows, ...retainedRows],
      sources: [...next.sources, ...retainedSources],
    }
    for (const row of next.rows) {
      if (!entryMutations.isPending(row.value))
        entryMutations.seed(row.value, row.enabled)
    }
    for (const source of next.sources) {
      if (!sourceMutations.isPending(source.id))
        sourceMutations.seed(source.id, source.enabled)
    }
    contentsState.value = 'ready'
  }

  const onRefreshSources = (): Promise<void> => runRefresh(false)

  // The composing flow has no source-editing controls, but an automatic read is
  // still recoverable in place. Its retry is the same read as the library button;
  // it never changes route membership or opens the library editor.
  const onRetryRefresh = (): Promise<void> => runRefresh(composing.value)

  // One read of the sources, whether the card asked for it or the operator did.
  // A failed read never removes the rows already on the table.
  const runRefresh = async (automatic: boolean): Promise<void> => {
    const list = props.list
    if (
      list === null ||
      refreshing.value ||
      mutationPending.value ||
      saving.value ||
      props.disabled === true
    )
      return
    const request = contentsRequest
    refreshing.value = true
    observing.value = automatic
    refreshError.value = ''
    refreshSkipped.value = 0
    try {
      const result = await refreshList(list.id)
      const loaded = await loadListContents(list.id)
      applyContents(request, loaded)
      if (request === contentsRequest)
        refreshSkipped.value = result.skippedEntries
    } catch (error) {
      if (request === contentsRequest) {
        refreshError.value =
          error instanceof RoutevaneAPIError &&
          error.status === 422 &&
          error.code === 'source_unavailable'
            ? t('listCard.refresh.failed.source')
            : t('listCard.refresh.failed.generic')
      }
    } finally {
      // Closing or switching cards invalidates this generation. In particular,
      // an old request must not clear a newer card's busy state or hide its
      // retry action.
      if (request === contentsRequest) {
        refreshing.value = false
        observing.value = false
      }
    }
  }

  const onToggleSource = (sourceID: string, enabled: boolean): void => {
    if (props.list === null || toggleBlocked.value) return
    sourceError.value = ''
    sourceMutations.mutate(sourceID, enabled)
  }

  const onRemoveSource = async (sourceID: string): Promise<void> => {
    const list = props.list
    if (list === null || interactionBusy.value) return
    const request = contentsRequest
    sourceError.value = ''
    saving.value = true
    try {
      applyContents(request, await removeListSource(list.id, sourceID))
    } catch {
      if (request === contentsRequest)
        sourceError.value = t('listCard.action.failed')
    } finally {
      if (request === contentsRequest) saving.value = false
    }
  }

  // A row's switch belongs to the library alone, and speaks the verdict
  // language: switching an offered row off records an exclude; switching it back
  // on removes the standing verdict; a row the operator added and then switched
  // off is simply taken back.
  const onToggleRow = (row: ListContentsRow, enabled: boolean): void => {
    if (props.list === null || toggleBlocked.value) return
    entryMutations.mutate(row.value, enabled)
  }

  // The creation form still writes a custom list, which the catalog defines by
  // domains alone, so it keeps the narrower grammar and says so.
  const parsedDomains = (
    raw: string,
    assign: (message: string) => void,
  ): string[] | null => {
    const lines = raw.split(/\r?\n/)
    const result: string[] = []
    for (let index = 0; index < lines.length; index += 1) {
      const line = lines[index]?.trim() ?? ''
      if (line === '') continue
      const domain = normalizeDomain(line)
      if (domain === null) {
        assign(t('listCard.new.domains.invalid', { line: index + 1 }))
        return null
      }
      result.push(domain)
    }
    const unique = [...new Set(result)].sort()
    if (unique.length > 64) {
      assign(t('listCard.new.domains.limit'))
      return null
    }
    assign('')
    return unique
  }

  // The batch endpoint refuses everything when one value is malformed, so a
  // rejected line is named rather than counted, and nothing is sent until the
  // whole draft reads.
  const firstRejectedLine = (raw: string): number => {
    const lines = raw.split(/\r?\n/)
    for (let index = 0; index < lines.length; index += 1) {
      if (parseDestinationList(lines[index] ?? '').skipped > 0) return index + 1
    }
    return 1
  }

  const includeValues = async (
    listID: string,
    values: string[],
  ): Promise<void> => {
    const request = contentsRequest
    let latest: ListContents | null = null
    for (let index = 0; index < values.length; index += BATCH_LIMIT) {
      latest = await setListValues(
        listID,
        values.slice(index, index + BATCH_LIMIT),
        'include',
      )
    }
    if (latest !== null) applyContents(request, latest)
  }

  const onAddValues = async (): Promise<void> => {
    const list = props.list
    if (list === null || interactionBusy.value) return
    const parsed = parseDestinationList(addValuesDraft.value)
    if (parsed.skipped > 0) {
      addError.value = t('listCard.domains.invalid', {
        line: firstRejectedLine(addValuesDraft.value),
      })
      return
    }
    if (parsed.values.length === 0) {
      addError.value = t('listCard.domains.required')
      return
    }
    // A hand-typed batch past the endpoint's bound is a paste accident; the file
    // import is the path built for a set that large.
    if (parsed.values.length > BATCH_LIMIT) {
      addError.value = t('listCard.domains.limit')
      return
    }
    addError.value = ''
    importStatus.value = ''
    saving.value = true
    try {
      await includeValues(list.id, parsed.values)
      addValuesDraft.value = ''
      addError.value = ''
      addValuesOpen.value = false
    } catch {
      addError.value = t('listCard.action.failed')
    } finally {
      saving.value = false
    }
  }

  // Closing the panel drops the complaint, never the typing: an operator who
  // closes it to read the table behind finds the draft where they left it.
  const onAddValuesOpen = (open: boolean): void => {
    if (!open && interactionBusy.value) return
    addValuesOpen.value = open
    if (!open) addError.value = ''
  }

  const onSourcesOpenChange = (open: boolean): void => {
    if (!open && interactionBusy.value) return
    sourcesOpen.value = open
  }

  // A routes file an operator already has is a set of destinations, so it is read
  // here rather than retyped. The file never leaves the browser: what is sent is
  // the destinations it named.
  const onImportFile = async (file: File): Promise<void> => {
    const list = props.list
    if (list === null || interactionBusy.value) return
    addError.value = ''
    importStatus.value = ''
    saving.value = true
    try {
      const parsed = parseDestinationList(await file.text())
      if (parsed.values.length === 0) {
        addError.value = t('listCard.import.empty')
        return
      }
      await includeValues(list.id, parsed.values)
      // What the file could not be read for is reported on the card, beside the
      // rows the file did land in.
      if (parsed.skipped > 0)
        importStatus.value = tc('listCard.import.skipped', parsed.skipped)
      addValuesOpen.value = false
    } catch {
      addError.value = t('listCard.action.failed')
    } finally {
      saving.value = false
    }
  }

  const onAddFeed = async (): Promise<void> => {
    const list = props.list
    if (list === null || interactionBusy.value) return
    if (feedURL.value.trim() === '') {
      feedError.value = t('listCard.feed.url.required')
      return
    }
    const request = contentsRequest
    saving.value = true
    feedError.value = ''
    try {
      await addListSource(list.id, feedURL.value.trim(), feedFormat.value)
      const loaded = await loadListContents(list.id)
      applyContents(request, loaded)
      feedURL.value = ''
      feedOpen.value = false
    } catch {
      feedError.value = t('listCard.feed.failed')
    } finally {
      saving.value = false
    }
  }

  const onRenameCustom = async (): Promise<void> => {
    const list = props.list
    if (list === null || list.custom !== true || interactionBusy.value) return
    const title = titleDraft.value.trim()
    if (title === '') {
      titleError.value = t('listCard.title.invalid')
      return
    }
    saving.value = true
    titleError.value = ''
    try {
      const record = await updateCustomList(
        list.id,
        title,
        (list.domains ?? []).map((domain) => domain.value),
      )
      report.updated(asListDetail(record))
    } catch {
      titleError.value = t('listCard.save.failed')
    } finally {
      saving.value = false
    }
  }

  const onCreate = async (): Promise<void> => {
    if (saving.value) return
    const domains = parsedDomains(domainsDraft.value, (message) => {
      domainsError.value = message
    })
    if (domains === null) return
    if (domains.length === 0) {
      domainsError.value = t('listCard.domains.required')
      return
    }
    const title = titleDraft.value.trim()
    if (title === '') {
      titleError.value = t('listCard.title.invalid')
      return
    }
    titleError.value = ''
    saving.value = true
    try {
      const record = await createCustomList(title, domains)
      report.created(asListDetail(record))
      requestClose()
    } catch {
      domainsError.value = t('listCard.save.failed')
    } finally {
      saving.value = false
    }
  }

  const onRemove = (): void => {
    if (props.list !== null && !interactionBusy.value) report.remove(props.list)
  }

  // A row is named by what it is and where it came from — the source's own name,
  // the catalog, or the operator. The value already says whether it is a domain,
  // an address or a network, so the caption does not repeat it.
  const originLabel = (row: ListContentsRow): string => {
    if (row.missing) return t('listCard.origin.missing')
    if (row.origin === 'catalog') return t('listCard.origin.catalog')
    if (row.origin === 'manual') return t('listCard.origin.manual')
    return row.origin
  }

  const sourceLabel = (id: string, type: string): string => {
    if (id.startsWith('feed-')) return t('listCard.source.custom')
    return tor(`listDetail.source.${type}`, type)
  }

  const requestClose = (): void => {
    if (dismissalBlocked.value) return
    contentsRequest += 1
    sourcesOpen.value = false
    report.close()
  }

  const onOpenChange = (open: boolean): void => {
    if (!open && !dismissalBlocked.value) requestClose()
  }

  return {
    active,
    addError,
    addValuesDraft,
    addValuesOpen,
    composing,
    contents,
    contentsState,
    curating,
    dismissalBlocked,
    domainsDraft,
    domainsError,
    effectiveSources,
    enabledCount,
    entryMutations,
    feedError,
    feedFormat,
    feedFormats,
    feedOpen,
    feedURL,
    filter,
    importStatus,
    interactionBusy,
    libraryHref,
    membershipLabel,
    observing,
    onAddFeed,
    onAddValues,
    onAddValuesOpen,
    onCreate,
    onImportFile,
    onOpenChange,
    onRefreshSources,
    onRemove,
    onRemoveSource,
    onRenameCustom,
    onRetryRefresh,
    onSourcesOpenChange,
    onToggleRow,
    onToggleSource,
    openContents,
    originLabel,
    refreshError,
    refreshing,
    refreshSkipped,
    requestClose,
    rows,
    saving,
    sourceCount,
    sourceError,
    sourceLabel,
    sourceMutations,
    sources,
    sourcesOpen,
    titleDraft,
    titleError,
    toggleBlocked,
    visibleRows,
    /**
     * Which reading of the contents the card is on. A caller that awaits a
     * frame of its own compares this before acting, so work begun for one list
     * never lands on the next one.
     */
    activeRequest: (): number => contentsRequest,
  }
}
