<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'

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
  type ListContents,
  type ListContentsRow,
  type ListDetail,
} from '@/shared/api/catalog'
import { RoutevaneAPIError } from '@/shared/api/http'
import { useLocale } from '@/shared/i18n/useLocale'
import {
  normalizeDomain,
  parseDestinationList,
} from '@/shared/lib/destinationList'
import { libraryPageHash } from '@/shared/lib/listsHash'
import { useDebouncedMutation } from '@/shared/model/useDebouncedMutation'
import type { ChoiceOption } from '@/shared/ui/kinds'
import RvButton from '@/shared/ui/RvButton.vue'
import RvDialog from '@/shared/ui/RvDialog.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvFilePicker from '@/shared/ui/RvFilePicker.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvTooltip from '@/shared/ui/RvTooltip.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'
import RvTextarea from '@/shared/ui/RvTextarea.vue'

/**
 * One list, read in either of the two flows the product has (ADR 0029).
 *
 * `library` is the curating flow: the name, the entries, the sources and the
 * list's own existence are the subject, and every switch here writes the
 * server. `compose` is the route being built: the same table is only read, and
 * the one act that route owns is the footer — whether the list is in this route
 * at all. Both flows can refresh existing source observations; only the
 * library edits list configuration.
 */
const props = defineProps<{
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
}>()

const emit = defineEmits<{
  close: []
  /** compose: the list's membership in the route this card was opened from. */
  include: [add: boolean]
  created: [detail: ListDetail]
  updated: [detail: ListDetail]
  /**
   * library: the operator asked for this list to stop existing. The flow that
   * owns the library confirms it and reports the refusal, so one confirmation
   * and one refusal serve both ways in.
   */
  remove: [detail: ListDetail]
}>()

const { t, tc, tor } = useLocale()

const composing = computed(() => props.mode === 'compose')
const curating = computed(() => props.mode === 'library')

// One card, two shapes: an existing list shows its single contents table and
// its sources; the creation form asks for a name and the first domains.
const active = computed(() => props.list !== null || props.creating === true)

// --- creation form -------------------------------------------------------
const titleDraft = ref('')
const titleError = ref('')
const domainsDraft = ref('')
const domainsError = ref('')
const saving = ref(false)

// --- existing list: contents ---------------------------------------------
// The batch endpoint takes at most this many values in one request, so a long
// import is sent in several rather than refused.
const batchLimit = 1024

const contents = ref<ListContents | null>(null)
const contentsState = ref<'idle' | 'loading' | 'ready' | 'failed'>('idle')
const filterInput = ref<HTMLInputElement | null>(null)
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
    const verdict: DomainVerdict = enabled
      ? 'auto'
      : row?.origin === 'manual'
        ? 'auto'
        : 'exclude'
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

// USlideover moves focus as soon as the sheet mounts. During the first source
// read the filter is disabled, so move focus once it is ready only when
// the keyboard is still on the sheet's own fallback control.
watch(contentsState, async (state) => {
  if (state !== 'ready' || props.list === null) return
  const request = contentsRequest
  const focused = document.activeElement
  await nextTick()
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()))
  const input = filterInput.value
  if (input === null || request !== contentsRequest) return
  const panel = input.closest('[role=dialog]')
  if (document.activeElement !== focused || !panel?.contains(focused)) return
  input.focus()
})

async function openContents(listID: string): Promise<void> {
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

// A card that shows a list before its sources were ever read shows almost
// nothing, and the operator has no way to know that. So the card reads them
// itself the first time it is opened on unread contents — in either flow,
// because reading a source changes no route and no list.
function shouldObserve(): boolean {
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
function applyContents(request: number, next: ListContents): void {
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
function reconcileMutationContents(next: ListContents): void {
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
      entryMutations.isPending(row.value) && !authoritativeRows.has(row.value),
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

function onRefreshSources(): Promise<void> {
  return runRefresh(false)
}

// The composing flow has no source-editing controls, but an automatic read is
// still recoverable in place. Its retry is the same read as the library button;
// it never changes route membership or opens the library editor.
function onRetryRefresh(): Promise<void> {
  return runRefresh(composing.value)
}

// One read of the sources, whether the card asked for it or the operator did.
// A failed read never removes the rows already on the table.
async function runRefresh(automatic: boolean): Promise<void> {
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

function onToggleSource(sourceID: string, enabled: boolean): void {
  if (props.list === null || toggleBlocked.value) return
  sourceError.value = ''
  sourceMutations.mutate(sourceID, enabled)
}

async function onRemoveSource(sourceID: string): Promise<void> {
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
function onToggleRow(row: ListContentsRow, enabled: boolean): void {
  if (props.list === null || toggleBlocked.value) return
  entryMutations.mutate(row.value, enabled)
}

// The creation form still writes a custom list, which the catalog defines by
// domains alone, so it keeps the narrower grammar and says so.
function parsedDomains(
  raw: string,
  assign: (message: string) => void,
): string[] | null {
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
function firstRejectedLine(raw: string): number {
  const lines = raw.split(/\r?\n/)
  for (let index = 0; index < lines.length; index += 1) {
    if (parseDestinationList(lines[index] ?? '').skipped > 0) return index + 1
  }
  return 1
}

async function includeValues(listID: string, values: string[]): Promise<void> {
  const request = contentsRequest
  let latest: ListContents | null = null
  for (let index = 0; index < values.length; index += batchLimit) {
    latest = await setListValues(
      listID,
      values.slice(index, index + batchLimit),
      'include',
    )
  }
  if (latest !== null) applyContents(request, latest)
}

async function onAddValues(): Promise<void> {
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
  if (parsed.values.length > batchLimit) {
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
function onAddValuesOpen(open: boolean): void {
  if (!open && interactionBusy.value) return
  addValuesOpen.value = open
  if (!open) addError.value = ''
}

function onSourcesOpenChange(open: boolean): void {
  if (!open && interactionBusy.value) return
  sourcesOpen.value = open
}

// A routes file an operator already has is a set of destinations, so it is read
// here rather than retyped. The file never leaves the browser: what is sent is
// the destinations it named.
async function onImportFile(file: File): Promise<void> {
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

async function onAddFeed(): Promise<void> {
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

async function onRenameCustom(): Promise<void> {
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
    emit('updated', asListDetail(record))
  } catch {
    titleError.value = t('listCard.save.failed')
  } finally {
    saving.value = false
  }
}

async function onCreate(): Promise<void> {
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
    emit('created', asListDetail(record))
    requestClose()
  } catch {
    domainsError.value = t('listCard.save.failed')
  } finally {
    saving.value = false
  }
}

function onRemove(): void {
  if (props.list !== null && !interactionBusy.value) emit('remove', props.list)
}

// A row is named by what it is and where it came from — the source's own name,
// the catalog, or the operator. The value already says whether it is a domain,
// an address or a network, so the caption does not repeat it.
function originLabel(row: ListContentsRow): string {
  if (row.missing) return t('listCard.origin.missing')
  if (row.origin === 'catalog') return t('listCard.origin.catalog')
  if (row.origin === 'manual') return t('listCard.origin.manual')
  return row.origin
}

function sourceLabel(id: string, type: string): string {
  if (id.startsWith('feed-')) return t('listCard.source.custom')
  return tor(`listDetail.source.${type}`, type)
}

function requestClose(): void {
  if (dismissalBlocked.value) return
  contentsRequest += 1
  sourcesOpen.value = false
  emit('close')
}

function onOpenChange(open: boolean): void {
  if (!open && !dismissalBlocked.value) requestClose()
}
</script>

<template>
  <RvDialog
    adaptive
    :close-label="t('action.close')"
    :dismissible="!dismissalBlocked"
    :fill="list !== null && creating !== true"
    :open="active"
    :title="list === null ? t('listCard.new') : list.title"
    @update:open="onOpenChange"
  >
    <template v-if="composing && list !== null" #actions>
      <RvTooltip :text="t('listCard.openLibrary')">
        <a
          class="list-card__library-link"
          :aria-label="t('listCard.openLibrary')"
          :href="libraryHref"
          rel="noopener"
          target="_blank"
          ><RvIcon name="external"
        /></a>
      </RvTooltip>
    </template>
    <!-- Creation: a name and the first domains. -->
    <form v-if="creating" class="list-card__section" @submit.prevent="onCreate">
      <RvField
        :error="titleError"
        input-id="list-title"
        :label="t('listCard.title.field')"
      >
        <template #default="{ describedBy, invalid }">
          <RvTextInput
            v-model="titleDraft"
            :described-by="describedBy"
            :disabled="saving"
            input-id="list-title"
            :invalid="invalid"
            maxlength="120"
          />
        </template>
      </RvField>
      <RvField
        :error="domainsError"
        :hint="t('listCard.new.domains.hint')"
        input-id="list-domains"
        :label="t('listCard.new.domains.field')"
      >
        <template #default="{ describedBy, invalid }">
          <RvTextarea
            v-model="domainsDraft"
            :described-by="describedBy"
            :disabled="saving"
            input-id="list-domains"
            :invalid="invalid"
            :rows="6"
          />
        </template>
      </RvField>
      <div class="list-card__actions">
        <RvButton :disabled="saving" variant="quiet" @click="requestClose">
          {{ t('action.cancel') }}
        </RvButton>
        <RvButton
          :disabled="saving"
          :loading="saving"
          type="submit"
          variant="primary"
        >
          {{ saving ? t('listCard.saving') : t('listCard.create') }}
        </RvButton>
      </div>
    </form>

    <!-- One element owns the sheet's height, so the table below fills it and
         the card ends where the panel ends. -->
    <div v-else-if="list !== null" class="list-card__body">
      <!-- A custom list's name is the operator's and stays editable where the
           list itself is the subject. -->
      <section
        v-if="curating && list.custom === true"
        aria-labelledby="list-card-name"
        class="list-card__section"
      >
        <h3 id="list-card-name" class="list-card__visually-hidden">
          {{ t('listCard.title.field') }}
        </h3>
        <div class="list-card__rename">
          <RvField
            :error="titleError"
            input-id="list-title"
            :label="t('listCard.title.field')"
          >
            <template #default="{ describedBy, invalid }">
              <RvTextInput
                v-model="titleDraft"
                :described-by="describedBy"
                :disabled="saving"
                input-id="list-title"
                :invalid="invalid"
                maxlength="120"
              />
            </template>
          </RvField>
          <RvButton
            :disabled="saving || titleDraft.trim() === list.title"
            :loading="saving"
            variant="secondary"
            @click="onRenameCustom"
          >
            {{ t('listCard.title.save') }}
          </RvButton>
        </div>
      </section>

      <!-- The one contents table: domains, addresses and networks. The card
           opens on it, because it is what the card is for. -->
      <section
        aria-labelledby="list-card-contents"
        class="list-card__section list-card__section--base list-card__section--fill"
        :aria-busy="contentsState === 'loading'"
      >
        <div class="list-card__heading">
          <h3 id="list-card-contents">{{ t('listCard.domains') }}</h3>
          <RvInfoTip
            :label="t('listCard.domains.info')"
            :text="t('listCard.domains.intro')"
          />
          <strong class="list-card__count">
            {{
              contents === null
                ? '—'
                : tc('listCard.domains.count', enabledCount)
            }}
          </strong>
        </div>
        <div class="list-card__commands">
          <!-- Reading the sources is the frequent act; editing which sources
               there are is the rare one, so the frequent one is the control
               and the rare one opens a panel. -->
          <RvTooltip :text="t('listCard.refresh')">
            <RvButton
              class="list-card__refresh-button"
              :aria-label="`${t('listCard.refresh')}: ${t('listCard.sources.open', { count: sourceCount })}`"
              :disabled="interactionBusy || contentsState !== 'ready'"
              size="compact"
              type="button"
              variant="secondary"
              @click="onRefreshSources"
            >
              <RvIcon
                name="refresh"
                :class="{ 'list-card__refresh-icon--busy': refreshing }"
              />
              <span>{{
                t('listCard.sources.open', { count: sourceCount })
              }}</span>
            </RvButton>
          </RvTooltip>
          <RvTooltip v-if="curating" :text="t('listCard.sources.configure')">
            <RvButton
              class="list-card__sources-button"
              :aria-label="t('listCard.sources.configure')"
              :disabled="interactionBusy"
              size="compact"
              type="button"
              variant="secondary"
              @click="sourcesOpen = true"
              ><RvIcon name="settings"
            /></RvButton>
          </RvTooltip>

          <!-- Source reads keep one compact, reserved status row. The retry
               control is present but invisible until an error so the filter
               and the known rows do not move when the answer changes. -->
          <div
            class="list-card__refresh-status"
            :role="
              refreshError !== '' || contentsState === 'failed'
                ? 'alert'
                : 'status'
            "
          >
            <RvStatus
              v-if="contentsState === 'loading'"
              class="list-card__refresh-indicator"
              :label="t('listCard.loading')"
              tone="busy"
            />
            <RvStatus
              v-else-if="contentsState === 'failed'"
              class="list-card__refresh-indicator"
              :label="t('listCard.failed.body')"
              tone="failed"
            />
            <RvStatus
              v-else-if="refreshing"
              class="list-card__refresh-indicator"
              :label="
                t(observing ? 'listCard.observing' : 'listCard.refresh.busy')
              "
              tone="busy"
            />
            <RvStatus
              v-else-if="refreshError !== ''"
              class="list-card__refresh-indicator"
              :label="refreshError"
              tone="failed"
            />
            <RvStatus
              v-else-if="sources.length === 0"
              class="list-card__refresh-indicator"
              :label="t('listCard.refresh.none')"
              tone="waiting"
            />
            <RvTooltip
              v-else-if="contents?.observed === true"
              :text="t('listCard.refresh.ready')"
            >
              <span
                class="list-card__ready"
                role="img"
                :aria-label="t('listCard.refresh.ready')"
                tabindex="0"
                ><RvIcon name="check"
              /></span>
            </RvTooltip>
            <RvStatus
              v-else
              class="list-card__refresh-indicator"
              :label="t('listCard.refresh.waiting')"
              tone="waiting"
            />
            <RvButton
              class="list-card__refresh-retry"
              :aria-hidden="
                refreshError === '' && contentsState !== 'failed'
                  ? 'true'
                  : undefined
              "
              :disabled="
                (refreshError === '' && contentsState !== 'failed') ||
                interactionBusy
              "
              size="compact"
              type="button"
              @click="
                contentsState === 'failed' && list !== null
                  ? openContents(list.id)
                  : onRetryRefresh()
              "
            >
              {{ t('action.retry') }}
            </RvButton>
          </div>
        </div>

        <div class="list-card__toolbar">
          <label class="list-card__filter">
            <RvIcon name="search" />
            <span class="list-card__visually-hidden">
              {{ t('listCard.filter') }}
            </span>
            <input
              ref="filterInput"
              v-model="filter"
              :disabled="contentsState !== 'ready'"
              class="list-card__filter-input"
              :placeholder="t('listCard.filter')"
              type="search"
            />
          </label>
          <RvButton
            v-if="curating"
            :disabled="interactionBusy"
            type="button"
            variant="secondary"
            @click="addValuesOpen = true"
          >
            <RvIcon name="plus" />
            {{ t('listCard.domains.add') }}
          </RvButton>
        </div>

        <!-- Composing reads the list here; the one act this route owns is
               the footer. The per-list override a composition can carry
               replaces the catalog's domain seeds and nothing else, so a switch
               on this row could not take an observed rule out of this route —
               and it must not pretend to. Excluding one value from one route is
               a feature of its own, with its own model. -->
        <ul
          class="list-card__rows"
          :aria-label="composing ? t('listCard.domains') : undefined"
          :tabindex="composing ? 0 : undefined"
        >
          <template v-if="contentsState === 'loading'">
            <li
              v-for="index in 8"
              :key="index"
              class="list-card__loading-row"
              aria-hidden="true"
            />
          </template>
          <li
            v-for="row in visibleRows"
            :key="row.value"
            :class="{ 'list-card__row--disabled': !row.enabled }"
          >
            <label v-if="curating" class="list-card__switch">
              <input
                :checked="row.enabled"
                :disabled="toggleBlocked"
                type="checkbox"
                @change="
                  onToggleRow(row, ($event.target as HTMLInputElement).checked)
                "
              />
              <span class="list-card__row-copy">
                <span class="list-card__value">{{ row.value }}</span>
                <small>{{ originLabel(row) }}</small>
              </span>
            </label>
            <span v-else class="list-card__row-copy list-card__entry">
              <span class="list-card__value">{{ row.value }}</span>
              <small>{{ originLabel(row) }}</small>
              <small v-if="!row.enabled" class="list-card__row-state">
                {{ t('listCard.domains.disabledInLibrary') }}
              </small>
            </span>
            <template v-if="curating">
              <small
                v-if="entryMutations.isPending(row.value)"
                class="list-card__row-state"
                role="status"
              >
                {{ t('listCard.mutation.pending') }}
              </small>
              <span
                v-else-if="entryMutations.getState(row.value).error !== null"
                class="list-card__row-recovery"
                role="alert"
              >
                <small class="list-card__row-state">
                  {{ t('listCard.mutation.failed') }}
                </small>
                <button
                  :aria-label="
                    t('listCard.mutation.retry.aria', {
                      entry: row.value,
                    })
                  "
                  class="list-card__row-action"
                  type="button"
                  @click="entryMutations.retry(row.value)"
                >
                  {{ t('listCard.mutation.retry') }}
                </button>
              </span>
            </template>
          </li>
        </ul>
        <p
          v-if="contentsState === 'ready' && rows.length === 0"
          class="list-card__muted"
          role="status"
        >
          {{ t('listCard.domains.empty') }}
        </p>
        <p
          v-else-if="contentsState === 'ready' && visibleRows.length === 0"
          class="list-card__muted"
          role="status"
        >
          {{ t('listCard.filter.empty') }}
        </p>

        <p v-if="importStatus !== ''" class="list-card__muted" role="status">
          {{ importStatus }}
        </p>
        <p v-if="refreshSkipped > 0" class="list-card__muted" role="status">
          {{ tc('listCard.refresh.skipped', refreshSkipped) }}
        </p>
        <!-- Composing leaves nothing under the table: the sheet's height is the
             table's, and the footer sits directly under its last row. -->
        <p v-if="curating" class="list-card__aside">
          <button
            class="list-card__link list-card__link--grave"
            :disabled="interactionBusy"
            type="button"
            @click="onRemove"
          >
            <RvIcon name="trash" />
            {{ t('listCard.remove') }}
          </button>
        </p>
      </section>
    </div>

    <template v-if="composing && !creating" #footer>
      <p class="list-card__membership">
        <RvStatus
          :label="membershipLabel"
          :tone="included === true ? 'ready' : 'waiting'"
        />
        <small v-if="pending">{{ t('listCard.pending') }}</small>
      </p>
      <RvButton
        :disabled="interactionBusy"
        variant="primary"
        @click="emit('include', included !== true)"
      >
        {{ included === true ? t('listDetail.remove') : t('listDetail.add') }}
      </RvButton>
    </template>
  </RvDialog>

  <!-- Adding is one bounded act taken over the table, not a form growing out of
       the bottom of it: the panel arrives where the pointer already is, and the
       table it adds to stays visible behind it. -->
  <RvDialog
    :close-label="t('action.close')"
    :dismissible="!interactionBusy"
    :open="curating && addValuesOpen && list !== null"
    :title="t('listCard.domains.add')"
    variant="panel"
    @update:open="onAddValuesOpen"
  >
    <form
      id="list-add-entries-form"
      class="list-card__entries"
      @submit.prevent="onAddValues"
    >
      <RvField
        :error="addError"
        :hint="t('listCard.domains.hint')"
        input-id="list-add-entries"
        :label="t('listCard.domains.field')"
      >
        <template #default="{ describedBy, invalid }">
          <RvTextarea
            v-model="addValuesDraft"
            :described-by="describedBy"
            :disabled="interactionBusy"
            input-id="list-add-entries"
            :invalid="invalid"
            :rows="6"
          />
        </template>
      </RvField>
      <div class="list-card__entries-import">
        <RvFilePicker
          accept=".txt,.bat,.lst,.json,.csv,text/plain"
          :action-label="t('listCard.import')"
          :disabled="interactionBusy"
          :empty-label="t('listCard.import.none')"
          :hint="t('listCard.import.hint')"
          input-id="list-card-import"
          :label="t('listCard.import')"
          @select="onImportFile"
        />
      </div>
    </form>
    <template #footer>
      <RvButton
        :disabled="interactionBusy"
        variant="quiet"
        @click="onAddValuesOpen(false)"
      >
        {{ t('action.cancel') }}
      </RvButton>
      <RvButton
        :disabled="interactionBusy"
        :loading="saving"
        form="list-add-entries-form"
        type="submit"
        variant="primary"
      >
        {{ t('listCard.domains.submit') }}
      </RvButton>
    </template>
  </RvDialog>

  <!-- Which feeds this list reads is a set of managed objects, not a preamble
       to the table: it is opened when it is the subject and closed the rest of
       the time. -->
  <RvDialog
    :close-label="t('action.close')"
    :dismissible="!interactionBusy"
    :open="curating && sourcesOpen && list !== null"
    :title="t('listCard.sources')"
    variant="panel"
    @update:open="onSourcesOpenChange"
  >
    <section class="list-card__section">
      <p v-if="sourceError !== ''" class="list-card__error" role="alert">
        {{ sourceError }}
      </p>

      <ul v-if="sources.length > 0" class="list-card__rows">
        <li v-for="source in effectiveSources" :key="source.id">
          <label class="list-card__switch">
            <input
              :checked="source.enabled"
              :disabled="toggleBlocked"
              type="checkbox"
              @change="
                onToggleSource(
                  source.id,
                  ($event.target as HTMLInputElement).checked,
                )
              "
            />
            <span class="list-card__row-copy">
              <strong>
                {{
                  source.custom && source.url !== '' ? source.url : source.id
                }}
              </strong>
              <small>{{ sourceLabel(source.id, source.type) }}</small>
            </span>
          </label>
          <template v-if="sourceMutations.isPending(source.id)">
            <small class="list-card__row-state" role="status">
              {{ t('listCard.mutation.pending') }}
            </small>
          </template>
          <span
            v-else-if="sourceMutations.getState(source.id).error !== null"
            class="list-card__row-recovery"
            role="alert"
          >
            <small class="list-card__row-state">
              {{ t('listCard.mutation.failed') }}
            </small>
            <button
              :aria-label="
                t('listCard.mutation.retry.aria', { entry: source.id })
              "
              class="list-card__row-action"
              type="button"
              @click="sourceMutations.retry(source.id)"
            >
              {{ t('listCard.mutation.retry') }}
            </button>
          </span>
          <button
            v-if="source.custom"
            :aria-label="t('listCard.feed.remove.aria', { source: source.url })"
            class="list-card__row-action"
            :disabled="interactionBusy"
            type="button"
            @click="onRemoveSource(source.id)"
          >
            {{ t('listCard.feed.remove') }}
          </button>
        </li>
      </ul>
      <p v-else class="list-card__muted">
        {{ t('listCard.sources.none') }}
      </p>

      <button
        v-if="!feedOpen"
        class="list-card__add"
        :disabled="interactionBusy"
        type="button"
        @click="feedOpen = true"
      >
        <RvIcon name="plus" />
        {{ t('listCard.feed.add') }}
      </button>
      <form v-else class="list-card__inline-form" @submit.prevent="onAddFeed">
        <RvField
          :error="feedError"
          :hint="t('listCard.feed.hint')"
          input-id="list-feed-url"
          :label="t('listCard.feed.url')"
        >
          <template #default="{ describedBy, invalid }">
            <RvTextInput
              v-model="feedURL"
              :described-by="describedBy"
              :disabled="interactionBusy"
              input-id="list-feed-url"
              :invalid="invalid"
              placeholder="https://example.com/list.txt"
            />
          </template>
        </RvField>
        <RvField input-id="list-feed-format" :label="t('listCard.feed.format')">
          <template #default="{ describedBy }">
            <RvSelect
              v-model="feedFormat"
              :described-by="describedBy"
              :disabled="interactionBusy"
              input-id="list-feed-format"
              :options="feedFormats"
              :placeholder="t('listCard.feed.format')"
            />
          </template>
        </RvField>
        <div class="list-card__actions">
          <RvButton
            :disabled="interactionBusy"
            size="compact"
            variant="quiet"
            @click="feedOpen = false"
          >
            {{ t('action.cancel') }}
          </RvButton>
          <RvButton
            :disabled="interactionBusy"
            :loading="saving"
            size="compact"
            type="submit"
            variant="secondary"
          >
            {{ t('listCard.feed.submit') }}
          </RvButton>
        </div>
      </form>
    </section>
  </RvDialog>
</template>

<style scoped>
/* The sheet's body, as one column: the sections above keep their height and the
   contents section takes everything that is left. */
.list-card__body {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow-y: auto;
}

.list-card__section {
  display: grid;
  gap: var(--rv-space-4);
  padding: var(--rv-space-5) var(--rv-space-6);
}

.list-card__section + .list-card__section {
  border-top: var(--rv-border-hair) solid var(--rv-color-rule-strong);
}

.list-card__section--base {
  background: var(--rv-color-surface);
}

/* A column rather than a grid, because exactly one of its children — the
   table — is allowed to take the height the others do not need. */
.list-card__section--fill {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: var(--rv-space-4);
  min-height: min-content;
}

.list-card__heading {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2) var(--rv-space-3);
  align-items: center;
}

.list-card__heading h3 {
  font-size: var(--rv-text-interface);
}

.list-card__count {
  margin-left: auto;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

/* A fact the composing card states but does not offer to change: the same
   words as the control beside the table in the other flow, without the box
   that would promise something to press. */
.list-card__fact {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-dense);
}

/* A named action that is not the point of the screen: it reads as a control
   rather than a link, and it says what it opens and how much is in there. */
.list-card__quiet {
  display: inline-flex;
  gap: var(--rv-space-2);
  align-items: center;
  min-height: var(--rv-control-compact);
  padding: 0 var(--rv-space-2);
  color: var(--rv-color-ink-muted);
  font: inherit;
  font-size: var(--rv-text-dense);
  background: transparent;
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.list-card__quiet:hover {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-hover);
}

.list-card__quiet:disabled {
  color: var(--rv-color-ink-tertiary);
  background: transparent;
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.list-card__muted {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.list-card__error {
  color: var(--rv-color-status-failed);
  font-size: var(--rv-text-dense);
}

/* Source reads are a compact fact beside the table. Keeping the retry slot in
   the row even while it is hidden means loading, failure and recovery share
   the same geometry; the row itself can still grow for translated or zoomed
   copy instead of clipping it. */
.list-card__refresh-status {
  flex: 1 1 var(--rv-composer-field-width);
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: var(--rv-space-3);
  align-items: center;
  min-width: 0;

  /* A compact status and its Retry control share the same minimum height.
     Longer failure explanations may wrap when recovery needs more context. */
  min-height: var(--rv-control-compact);
}

.list-card__refresh-indicator {
  flex: 1 1 auto;
  min-width: 0;
}

.list-card__refresh-retry {
  flex: none;
}

.list-card__refresh-retry[aria-hidden='true'] {
  visibility: hidden;
}

.list-card__aside {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  align-items: center;
}

.list-card__link {
  display: inline-flex;
  gap: var(--rv-space-2);
  align-items: center;
  min-height: var(--rv-control-compact);
  padding: 0 var(--rv-space-2);
  color: var(--rv-color-accent-ink);
  font: inherit;
  font-size: var(--rv-text-dense);
  text-decoration: none;
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.list-card__link:hover {
  background: var(--rv-color-accent-quiet);
}

.list-card__link--grave {
  color: var(--rv-color-status-failed);
}

.list-card__link--grave:hover {
  background: var(--rv-color-surface-hover);
}

.list-card__link:disabled {
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.list-card__rename {
  display: flex;
  gap: var(--rv-space-3);
  align-items: flex-end;
}

.list-card__rename > :first-child {
  flex: 1;
}

.list-card__visually-hidden {
  position: absolute;
  width: 0.0625rem;
  height: 0.0625rem;
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}

/* The filter and the way in share one line above the table: the control that
   adds a row belongs where the rows are, not below everything they say. */
.list-card__toolbar {
  display: flex;
  gap: var(--rv-space-3);
  align-items: center;
}

.list-card__filter {
  display: flex;
  flex: 1;
  gap: var(--rv-space-2);
  align-items: center;
  min-width: 0;
  min-height: var(--rv-control-touch);
  padding: 0 var(--rv-space-3);
  color: var(--rv-color-ink-tertiary);
  background: var(--rv-color-canvas);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-sm);
}

.list-card__filter:focus-within {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-border-hair);
}

.list-card__filter-input {
  flex: 1;
  min-width: 0;

  /* The wrapper owns the height; a minimum here would add the wrapper's
     border on top of it and leave the field two pixels taller than the
     controls beside it. */
  align-self: stretch;
  padding: 0;
  color: var(--rv-color-ink);
  font: inherit;
  background: transparent;
  border: 0;
  outline: 0;
}

.list-card__filter-input::placeholder {
  color: var(--rv-color-ink-tertiary);
}

/* The table keeps its own scroll so the heading above it and the actions below
   it stay in place. In a panel it is capped; in the sheet it claims the height
   the sheet actually has, which is what keeps the footer off an empty band. */
.list-card__rows {
  display: grid;
  grid-auto-rows: max-content;
  align-content: start;
  max-height: var(--rv-picker-height);
  overflow-y: auto;
  overscroll-behavior: contain;
  margin: 0;
  padding: 0;
  list-style: none;
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.list-card__section--fill .list-card__rows {
  flex: 1;
  max-height: none;

  /* Keep a usable table when enlarged controls consume the sheet. Size
     containment excludes the entire list from the section's intrinsic height;
     the body can then scroll its controls without replacing the table scroll. */
  min-height: calc(var(--rv-row-default) * 2);
  contain: size;
}

.list-card__loading-row::before {
  content: '';
  display: block;
  width: 60%;
  height: var(--rv-space-4);
  margin-block: var(--rv-space-3);
  background: var(--rv-color-surface-muted);
  border-radius: var(--rv-radius-sm);
}

.list-card__rows li {
  display: flex;
  gap: var(--rv-space-4);
  align-items: center;
  justify-content: space-between;
  min-height: var(--rv-row-default);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.list-card__switch {
  display: flex;
  flex: 1;
  gap: var(--rv-space-3);
  align-items: center;
  min-width: 0;
  min-height: var(--rv-control-touch);
  padding: var(--rv-space-2) 0;
  cursor: pointer;
}

.list-card__switch input {
  flex: none;
  width: var(--rv-control-choice);
  height: var(--rv-control-choice);
  margin: 0;
  accent-color: var(--rv-color-accent);
}

.list-card__switch input:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-border-hair);
}

.list-card__row-copy {
  display: grid;
  gap: var(--rv-space-1);
  min-width: 0;
}

/* A composing row is read, not pressed, so it takes the switch's box without
   the switch and the two flows still read as one table. */
.list-card__entry {
  flex: 1;
  align-content: center;
  min-height: var(--rv-control-touch);
  padding: var(--rv-space-2) 0;
}

.list-card__row-copy strong {
  overflow-wrap: anywhere;
}

.list-card__row-copy small {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}

.list-card__row-recovery {
  display: inline-flex;
  flex: none;
  flex-wrap: wrap;
  gap: var(--rv-space-1) var(--rv-space-2);
  align-items: center;
}

.list-card__row-recovery .list-card__row-state {
  color: var(--rv-color-status-failed);
}

.list-card__value {
  font-family: var(--rv-font-mono);
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}

.list-card__row-action {
  flex: none;
  min-height: var(--rv-control-compact);
  padding: 0 var(--rv-space-2);
  color: var(--rv-color-ink-muted);
  font: inherit;
  font-size: var(--rv-text-dense);
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.list-card__row-action:hover {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-hover);
}

.list-card__add {
  display: inline-flex;
  gap: var(--rv-space-2);
  align-items: center;
  justify-self: start;
  min-height: var(--rv-control-compact);
  padding: 0 var(--rv-space-2);
  color: var(--rv-color-accent-ink);
  font: inherit;
  font-weight: 600;
  font-size: var(--rv-text-dense);
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.list-card__add:hover {
  background: var(--rv-color-accent-quiet);
}

.list-card__add:disabled {
  background: transparent;
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

/* The file control is driven by the button beside it. It stays in the document
   so the browser can open the picker, and out of the tab order so the operator
   meets one control rather than two. */
.list-card__file {
  position: absolute;
  width: 0.0625rem;
  height: 0.0625rem;
  overflow: hidden;
  clip-path: inset(50%);
}

.list-card__add:focus-visible,
.list-card__link:focus-visible,
.list-card__row-action:focus-visible,
.list-card__quiet:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-focus-offset);
}

.list-card__entries {
  display: grid;
  gap: var(--rv-space-4);
  padding: var(--rv-space-5) var(--rv-space-6);
}

/* Reading a file the operator already has is the same act as typing into the
   field above, so it stands beside it rather than under a heading of its own. */
.list-card__entries-import {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2) var(--rv-space-3);
  align-items: center;
}

.list-card__inline-form {
  display: grid;
  gap: var(--rv-space-3);
  padding: var(--rv-space-4);
  background: var(--rv-color-surface-muted);
  border-radius: var(--rv-radius-md);
}

.list-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  justify-content: flex-end;
}

/* One line: where the list stands in this route, and — while the route is an
   unsaved draft — that the answer is waiting on a save. */
.list-card__membership {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  align-items: baseline;
  min-width: 0;
}

.list-card__membership small {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}

/* Disabled library values remain fully opaque so their value, origin and
   state each keep AA contrast. Text, rather than faded containment, carries
   the distinction for every theme. */
.list-card__row--disabled {
  color: var(--rv-color-ink-muted);
}

.list-card__row--disabled .list-card__value,
.list-card__row--disabled .list-card__row-copy small {
  color: var(--rv-color-ink-muted);
}

.list-card__row--disabled .list-card__row-state {
  color: var(--rv-color-ink);
  font-weight: 600;
}

@container dialog (width <= 36rem) {
  .list-card__section,
  .list-card__entries {
    padding-right: var(--rv-space-4);
    padding-left: var(--rv-space-4);
  }

  /* Two controls that no longer fit one line take two rather than shrinking
     the filter to nothing. */
  .list-card__toolbar {
    flex-wrap: wrap;
  }

  .list-card__rename {
    align-items: stretch;
    flex-direction: column;
  }

  .list-card__actions > * {
    flex: 1 1 auto;
  }
}

.list-card__commands {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--rv-space-2) var(--rv-space-3);
}

.list-card__refresh-button {
  flex: none;
}

.list-card__sources-button {
  flex: none;
  width: var(--rv-control-compact);
  padding: 0;
}

.list-card__ready {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  justify-self: start;
  width: var(--rv-control-compact);
  height: var(--rv-control-compact);
  color: var(--rv-color-status-ready);
}

.list-card__library-link {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: var(--rv-control-touch);
  height: var(--rv-control-touch);
  color: var(--rv-color-ink-muted);
  border-radius: var(--rv-radius-sm);
}

.list-card__library-link:hover {
  background: var(--rv-color-surface-hover);
}

.list-card__refresh-icon--busy {
  animation: list-card-refresh var(--rv-motion-working) linear infinite;
}

@keyframes list-card-refresh {
  to {
    transform: rotate(360deg);
  }
}

@media (prefers-reduced-motion: reduce) {
  .list-card__refresh-icon--busy {
    animation: none;
  }
}
</style>
