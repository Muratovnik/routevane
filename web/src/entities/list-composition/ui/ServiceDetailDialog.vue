<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import {
  addServiceSource,
  asServiceDetail,
  createCustomService,
  loadServiceContents,
  refreshService,
  removeServiceSource,
  setServiceSourceEnabled,
  setServiceValues,
  updateCustomService,
  type DomainVerdict,
  type ServiceContents,
  type ServiceContentsRow,
  type ServiceDetail,
} from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'
import {
  normalizeDomain,
  parseDestinationList,
} from '@/shared/lib/destinationList'
import { libraryPageHash } from '@/shared/lib/libraryHash'
import type { ChoiceOption } from '@/shared/ui/kinds'
import RvButton from '@/shared/ui/RvButton.vue'
import RvDialog from '@/shared/ui/RvDialog.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvFilePicker from '@/shared/ui/RvFilePicker.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
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
 * at all. Nothing on the composing card reaches the list or another route,
 * which is why no caption has to explain the reach of an edit.
 */
const props = defineProps<{
  mode: 'compose' | 'library'
  creating?: boolean
  disabled?: boolean
  included?: boolean
  /** The route the footer controls. Blank while a draft has no name yet. */
  listName?: string
  /**
   * Whether that route is still an unsaved draft. Membership then waits for a
   * save, and the footer names no route: the composer proposes the name from
   * the lists picked, so naming it here would read as naming this list.
   */
  pending?: boolean
  service: ServiceDetail | null
}>()

const emit = defineEmits<{
  close: []
  /** compose: the list's membership in the route this card was opened from. */
  include: [add: boolean]
  created: [detail: ServiceDetail]
  updated: [detail: ServiceDetail]
  /**
   * library: the operator asked for this list to stop existing. The flow that
   * owns the library confirms it and reports the refusal, so one confirmation
   * and one refusal serve both ways in.
   */
  remove: [detail: ServiceDetail]
}>()

const { t, tc, tor } = useLocale()

const composing = computed(() => props.mode === 'compose')
const curating = computed(() => props.mode === 'library')

// One card, two shapes: an existing list shows its single contents table and
// its sources; the creation form asks for a name and the first domains.
const active = computed(() => props.service !== null || props.creating === true)

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

const contents = ref<ServiceContents | null>(null)
const contentsState = ref<'idle' | 'loading' | 'ready' | 'failed'>('idle')
const refreshing = ref(false)
const observing = ref(false)
const actionError = ref('')
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

const rows = computed<ServiceContentsRow[]>(() => contents.value?.rows ?? [])

// What the list currently offers. Both flows count the same thing: a route
// takes the list as the library holds it.
const enabledCount = computed(
  () => rows.value.filter((row) => row.enabled).length,
)

// A list can stand for hundreds of destinations. The field over the table
// narrows what is drawn by what a row says — its value and where it came from.
const visibleRows = computed<ServiceContentsRow[]>(() => {
  const query = filter.value.trim().toLowerCase()
  if (query === '') return rows.value
  return rows.value.filter(
    (row) =>
      row.value.toLowerCase().includes(query) ||
      originLabel(row).toLowerCase().includes(query),
  )
})

const sources = computed(() => contents.value?.sources ?? [])
// Before the contents land, the catalog entry already knows how many sources
// the list has, so the control that opens them never starts at nothing.
const sourceCount = computed(() =>
  contents.value === null
    ? (props.service?.sourceCount ?? props.service?.sources?.length ?? 0)
    : sources.value.length,
)

const feedFormats = computed<ChoiceOption[]>(() => [
  { label: t('serviceCard.feed.format.text'), value: 'text' },
  { label: t('serviceCard.feed.format.domainList'), value: 'domain-list' },
  { label: t('serviceCard.feed.format.json'), value: 'json' },
])

// Writes are serialized inside this card. A list that is still being read or
// refreshed cannot be removed, and a second switch cannot race the first one.
const interactionBusy = computed(
  () =>
    props.disabled === true ||
    saving.value ||
    refreshing.value ||
    contentsState.value === 'loading',
)

// Where this list is curated. The draft behind the card is unsaved, so the
// other flow opens in a tab of its own rather than over it.
const libraryHref = computed(() => {
  const service = props.service
  if (service === null) return '/library'
  return `/library${libraryPageHash({
    category: service.categories[0] ?? '',
    list: service.id,
  })}`
})

// The footer names the route it is talking about, unless that route is a draft
// with no stored name to use.
const membershipLabel = computed(() => {
  const name = (props.listName ?? '').trim()
  if (name === '' || props.pending === true) {
    return props.included === true
      ? t('serviceCard.membership.in.unnamed')
      : t('serviceCard.membership.out.unnamed')
  }
  return props.included === true
    ? t('serviceCard.membership.in', { name })
    : t('serviceCard.membership.out', { name })
})

watch(
  [() => props.service, () => props.creating],
  ([service, creating]) => {
    contentsRequest += 1
    titleDraft.value = service?.title ?? ''
    titleError.value = ''
    domainsDraft.value = ''
    domainsError.value = ''
    actionError.value = ''
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
    if (service !== null && creating !== true) void openContents(service.id)
  },
  { immediate: true },
)

async function openContents(serviceID: string): Promise<void> {
  const request = ++contentsRequest
  contentsState.value = 'loading'
  try {
    const loaded = await loadServiceContents(serviceID)
    if (request !== contentsRequest) return
    contents.value = loaded
    contentsState.value = 'ready'
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
    props.service !== null &&
    loaded !== null &&
    !loaded.observed &&
    loaded.sources.some((source) => source.enabled)
  )
}

// applyContents lands a mutation's answer, unless the card moved on.
function applyContents(request: number, next: ServiceContents): void {
  if (request !== contentsRequest) return
  contents.value = next
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
  const service = props.service
  if (service === null || refreshing.value) return
  const request = contentsRequest
  refreshing.value = true
  observing.value = automatic
  refreshError.value = ''
  refreshSkipped.value = 0
  try {
    const result = await refreshService(service.id)
    const loaded = await loadServiceContents(service.id)
    applyContents(request, loaded)
    if (request === contentsRequest)
      refreshSkipped.value = result.skippedEntries
  } catch {
    if (request === contentsRequest)
      refreshError.value = t('serviceCard.refresh.failed.compact')
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

async function onToggleSource(
  sourceID: string,
  enabled: boolean,
): Promise<void> {
  const service = props.service
  if (service === null || interactionBusy.value) return
  const request = contentsRequest
  sourceError.value = ''
  saving.value = true
  try {
    applyContents(
      request,
      await setServiceSourceEnabled(service.id, sourceID, enabled),
    )
  } catch {
    if (request === contentsRequest)
      sourceError.value = t('serviceCard.action.failed')
  } finally {
    if (request === contentsRequest) saving.value = false
  }
}

async function onRemoveSource(sourceID: string): Promise<void> {
  const service = props.service
  if (service === null || interactionBusy.value) return
  const request = contentsRequest
  sourceError.value = ''
  saving.value = true
  try {
    applyContents(request, await removeServiceSource(service.id, sourceID))
  } catch {
    if (request === contentsRequest)
      sourceError.value = t('serviceCard.action.failed')
  } finally {
    if (request === contentsRequest) saving.value = false
  }
}

// A row's switch belongs to the library alone, and speaks the verdict
// language: switching an offered row off records an exclude; switching it back
// on removes the standing verdict; a row the operator added and then switched
// off is simply taken back.
async function onToggleRow(row: ServiceContentsRow): Promise<void> {
  const service = props.service
  if (service === null || interactionBusy.value) return
  let verdict: DomainVerdict = 'auto'
  if (row.enabled) {
    verdict = row.origin === 'manual' ? 'auto' : 'exclude'
  }
  const request = contentsRequest
  actionError.value = ''
  saving.value = true
  try {
    applyContents(
      request,
      await setServiceValues(service.id, [row.value], verdict),
    )
  } catch {
    if (request === contentsRequest)
      actionError.value = t('serviceCard.action.failed')
  } finally {
    if (request === contentsRequest) saving.value = false
  }
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
      assign(t('serviceCard.new.domains.invalid', { line: index + 1 }))
      return null
    }
    result.push(domain)
  }
  const unique = [...new Set(result)].sort()
  if (unique.length > 64) {
    assign(t('serviceCard.new.domains.limit'))
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

async function includeValues(
  serviceID: string,
  values: string[],
): Promise<void> {
  const request = contentsRequest
  let latest: ServiceContents | null = null
  for (let index = 0; index < values.length; index += batchLimit) {
    latest = await setServiceValues(
      serviceID,
      values.slice(index, index + batchLimit),
      'include',
    )
  }
  if (latest !== null) applyContents(request, latest)
}

async function onAddValues(): Promise<void> {
  const service = props.service
  if (service === null || saving.value) return
  const parsed = parseDestinationList(addValuesDraft.value)
  if (parsed.skipped > 0) {
    addError.value = t('serviceCard.domains.invalid', {
      line: firstRejectedLine(addValuesDraft.value),
    })
    return
  }
  if (parsed.values.length === 0) {
    addError.value = t('serviceCard.domains.required')
    return
  }
  // A hand-typed batch past the endpoint's bound is a paste accident; the file
  // import is the path built for a set that large.
  if (parsed.values.length > batchLimit) {
    addError.value = t('serviceCard.domains.limit')
    return
  }
  addError.value = ''
  importStatus.value = ''
  saving.value = true
  try {
    await includeValues(service.id, parsed.values)
    addValuesDraft.value = ''
    addError.value = ''
    addValuesOpen.value = false
  } catch {
    addError.value = t('serviceCard.action.failed')
  } finally {
    saving.value = false
  }
}

// Closing the panel drops the complaint, never the typing: an operator who
// closes it to read the table behind finds the draft where they left it.
function onAddValuesOpen(open: boolean): void {
  addValuesOpen.value = open
  if (!open) addError.value = ''
}

// A routes file an operator already has is a set of destinations, so it is read
// here rather than retyped. The file never leaves the browser: what is sent is
// the destinations it named.
async function onImportFile(file: File): Promise<void> {
  const service = props.service
  if (service === null || saving.value) return
  addError.value = ''
  importStatus.value = ''
  saving.value = true
  try {
    const parsed = parseDestinationList(await file.text())
    if (parsed.values.length === 0) {
      addError.value = t('serviceCard.import.empty')
      return
    }
    await includeValues(service.id, parsed.values)
    // What the file could not be read for is reported on the card, beside the
    // rows the file did land in.
    if (parsed.skipped > 0)
      importStatus.value = tc('serviceCard.import.skipped', parsed.skipped)
    addValuesOpen.value = false
  } catch {
    addError.value = t('serviceCard.action.failed')
  } finally {
    saving.value = false
  }
}

async function onAddFeed(): Promise<void> {
  const service = props.service
  if (service === null || saving.value) return
  if (feedURL.value.trim() === '') {
    feedError.value = t('serviceCard.feed.url.required')
    return
  }
  const request = contentsRequest
  saving.value = true
  feedError.value = ''
  try {
    await addServiceSource(service.id, feedURL.value.trim(), feedFormat.value)
    const loaded = await loadServiceContents(service.id)
    applyContents(request, loaded)
    feedURL.value = ''
    feedOpen.value = false
  } catch {
    feedError.value = t('serviceCard.feed.failed')
  } finally {
    saving.value = false
  }
}

async function onRenameCustom(): Promise<void> {
  const service = props.service
  if (service === null || service.custom !== true || saving.value) return
  const title = titleDraft.value.trim()
  if (title === '') {
    titleError.value = t('serviceCard.title.invalid')
    return
  }
  saving.value = true
  titleError.value = ''
  try {
    const record = await updateCustomService(
      service.id,
      title,
      (service.domains ?? []).map((domain) => domain.value),
    )
    emit('updated', asServiceDetail(record))
  } catch {
    titleError.value = t('serviceCard.save.failed')
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
    domainsError.value = t('serviceCard.domains.required')
    return
  }
  const title = titleDraft.value.trim()
  if (title === '') {
    titleError.value = t('serviceCard.title.invalid')
    return
  }
  titleError.value = ''
  saving.value = true
  try {
    const record = await createCustomService(title, domains)
    emit('created', asServiceDetail(record))
    requestClose()
  } catch {
    domainsError.value = t('serviceCard.save.failed')
  } finally {
    saving.value = false
  }
}

function onRemove(): void {
  if (props.service !== null && !interactionBusy.value)
    emit('remove', props.service)
}

// A row is named by what it is and where it came from — the source's own name,
// the catalog, or the operator. The value already says whether it is a domain,
// an address or a network, so the caption does not repeat it.
function originLabel(row: ServiceContentsRow): string {
  if (row.missing) return t('serviceCard.origin.missing')
  if (row.origin === 'catalog') return t('serviceCard.origin.catalog')
  if (row.origin === 'manual') return t('serviceCard.origin.manual')
  return row.origin
}

function sourceLabel(id: string, type: string): string {
  if (id.startsWith('feed-')) return t('serviceCard.source.custom')
  return tor(`serviceDetail.source.${type}`, type)
}

function requestClose(): void {
  contentsRequest += 1
  sourcesOpen.value = false
  emit('close')
}

function onOpenChange(open: boolean): void {
  if (!open) requestClose()
}
</script>

<template>
  <RvDialog
    :close-label="t('action.close')"
    :dismissible="!saving"
    :fill="service !== null && creating !== true"
    :open="active"
    :title="service === null ? t('serviceCard.new') : service.title"
    @update:open="onOpenChange"
  >
    <!-- Creation: a name and the first domains. -->
    <form
      v-if="creating"
      class="service-card__section"
      @submit.prevent="onCreate"
    >
      <RvField
        :error="titleError"
        input-id="service-title"
        :label="t('serviceCard.title.field')"
      >
        <template #default="{ describedBy, invalid }">
          <RvTextInput
            v-model="titleDraft"
            :described-by="describedBy"
            :disabled="saving"
            input-id="service-title"
            :invalid="invalid"
            maxlength="120"
          />
        </template>
      </RvField>
      <RvField
        :error="domainsError"
        :hint="t('serviceCard.new.domains.hint')"
        input-id="service-domains"
        :label="t('serviceCard.new.domains.field')"
      >
        <template #default="{ describedBy, invalid }">
          <RvTextarea
            v-model="domainsDraft"
            :described-by="describedBy"
            :disabled="saving"
            input-id="service-domains"
            :invalid="invalid"
            :rows="6"
          />
        </template>
      </RvField>
      <div class="service-card__actions">
        <RvButton :disabled="saving" variant="quiet" @click="requestClose">
          {{ t('action.cancel') }}
        </RvButton>
        <RvButton
          :disabled="saving"
          :loading="saving"
          type="submit"
          variant="primary"
        >
          {{ saving ? t('serviceCard.saving') : t('serviceCard.create') }}
        </RvButton>
      </div>
    </form>

    <!-- One element owns the sheet's height, so the table below fills it and
         the card ends where the panel ends. -->
    <div v-else-if="service !== null" class="service-card__body">
      <!-- A custom list's name is the operator's and stays editable where the
           list itself is the subject. -->
      <section
        v-if="curating && service.custom === true"
        aria-labelledby="service-card-name"
        class="service-card__section"
      >
        <h3 id="service-card-name" class="service-card__visually-hidden">
          {{ t('serviceCard.title.field') }}
        </h3>
        <div class="service-card__rename">
          <RvField
            :error="titleError"
            input-id="service-title"
            :label="t('serviceCard.title.field')"
          >
            <template #default="{ describedBy, invalid }">
              <RvTextInput
                v-model="titleDraft"
                :described-by="describedBy"
                :disabled="saving"
                input-id="service-title"
                :invalid="invalid"
                maxlength="120"
              />
            </template>
          </RvField>
          <RvButton
            :disabled="saving || titleDraft.trim() === service.title"
            :loading="saving"
            variant="secondary"
            @click="onRenameCustom"
          >
            {{ t('serviceCard.title.save') }}
          </RvButton>
        </div>
      </section>

      <!-- The one contents table: domains, addresses and networks. The card
           opens on it, because it is what the card is for. -->
      <section
        aria-labelledby="service-card-contents"
        class="service-card__section service-card__section--base service-card__section--fill"
      >
        <div class="service-card__heading">
          <h3 id="service-card-contents">{{ t('serviceCard.domains') }}</h3>
          <RvInfoTip
            :label="t('serviceCard.domains.info')"
            :text="t('serviceCard.domains.intro')"
          />
          <!-- Reading the sources is the frequent act; editing which sources
               there are is the rare one, so the frequent one is the control
               and the rare one opens a panel. -->
          <template v-if="curating">
            <RvButton
              :disabled="refreshing || contentsState !== 'ready'"
              :loading="refreshing"
              size="compact"
              type="button"
              variant="quiet"
              @click="onRefreshSources"
            >
              <RvIcon name="refresh" />
              {{
                refreshing
                  ? t('serviceCard.refresh.busy')
                  : t('serviceCard.refresh')
              }}
            </RvButton>
            <RvButton
              :disabled="interactionBusy"
              size="compact"
              type="button"
              variant="quiet"
              @click="sourcesOpen = true"
            >
              <RvIcon name="settings" />
              {{ t('serviceCard.sources.open', { count: sourceCount }) }}
            </RvButton>
          </template>
          <template v-else>
            <small class="service-card__fact">
              {{ t('serviceCard.sources.open', { count: sourceCount }) }}
            </small>
            <!-- The way to the other flow, in a tab of its own, so the unsaved
                 draft this card was opened from survives being left. -->
            <a
              class="service-card__link"
              :href="libraryHref"
              rel="noopener"
              target="_blank"
            >
              <RvIcon name="external" />
              {{ t('serviceCard.openLibrary') }}
            </a>
          </template>
          <strong v-if="contents !== null" class="service-card__count">
            {{ tc('serviceCard.domains.count', enabledCount) }}
          </strong>
        </div>

        <RvStateNotice
          v-if="contentsState === 'loading'"
          live
          :title="t('serviceCard.loading')"
          tone="busy"
        />
        <RvStateNotice
          v-else-if="contentsState === 'failed'"
          :body="t('serviceCard.failed.body')"
          live
          :title="t('serviceCard.failed')"
          tone="failed"
        >
          <template #action>
            <RvButton
              type="button"
              @click="service !== null && openContents(service.id)"
            >
              {{ t('action.retry') }}
            </RvButton>
          </template>
        </RvStateNotice>

        <template v-else-if="contents !== null">
          <p v-if="actionError !== ''" class="service-card__error" role="alert">
            {{ actionError }}
          </p>

          <!-- Source reads keep one compact, reserved status row. The retry
               control is present but invisible until an error so the filter
               and the known rows do not move when the answer changes. -->
          <div
            class="service-card__refresh-status"
            :role="refreshError !== '' ? 'alert' : 'status'"
          >
            <RvStatus
              v-if="observing"
              class="service-card__refresh-indicator"
              :label="t('serviceCard.observing')"
              tone="busy"
            />
            <RvStatus
              v-else-if="refreshing"
              class="service-card__refresh-indicator"
              :label="t('serviceCard.refresh.busy')"
              tone="busy"
            />
            <RvStatus
              v-else-if="refreshError !== ''"
              class="service-card__refresh-indicator"
              :label="refreshError"
              tone="failed"
            />
            <RvStatus
              v-else-if="sources.length === 0"
              class="service-card__refresh-indicator"
              :label="t('serviceCard.refresh.none')"
              tone="waiting"
            />
            <RvStatus
              v-else-if="contents?.observed === true"
              class="service-card__refresh-indicator"
              :label="t('serviceCard.refresh.ready')"
              tone="ready"
            />
            <RvStatus
              v-else
              class="service-card__refresh-indicator"
              :label="t('serviceCard.refresh.waiting')"
              tone="waiting"
            />
            <RvButton
              class="service-card__refresh-retry"
              :aria-hidden="refreshError === '' ? 'true' : undefined"
              :disabled="refreshError === '' || refreshing"
              size="compact"
              type="button"
              @click="onRetryRefresh"
            >
              {{ t('action.retry') }}
            </RvButton>
          </div>

          <div class="service-card__toolbar">
            <label class="service-card__filter">
              <RvIcon name="search" />
              <span class="service-card__visually-hidden">
                {{ t('serviceCard.filter') }}
              </span>
              <input
                v-model="filter"
                class="service-card__filter-input"
                :placeholder="t('serviceCard.filter')"
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
              {{ t('serviceCard.domains.add') }}
            </RvButton>
          </div>

          <!-- Composing reads the list here; the one act this route owns is
               the footer. The per-list override a composition can carry
               replaces the catalog's domain seeds and nothing else, so a switch
               on this row could not take an observed rule out of this route —
               and it must not pretend to. Excluding one value from one route is
               a feature of its own, with its own model. -->
          <ul class="service-card__rows">
            <li v-for="row in visibleRows" :key="row.value">
              <label v-if="curating" class="service-card__switch">
                <input
                  :checked="row.enabled"
                  :disabled="interactionBusy"
                  type="checkbox"
                  @change="onToggleRow(row)"
                />
                <span class="service-card__row-copy">
                  <span class="service-card__value">{{ row.value }}</span>
                  <small>{{ originLabel(row) }}</small>
                </span>
              </label>
              <span v-else class="service-card__row-copy service-card__entry">
                <span class="service-card__value">{{ row.value }}</span>
                <small>{{ originLabel(row) }}</small>
              </span>
            </li>
          </ul>
          <p v-if="rows.length === 0" class="service-card__muted" role="status">
            {{ t('serviceCard.domains.empty') }}
          </p>
          <p
            v-else-if="visibleRows.length === 0"
            class="service-card__muted"
            role="status"
          >
            {{ t('serviceCard.filter.empty') }}
          </p>

          <p
            v-if="importStatus !== ''"
            class="service-card__muted"
            role="status"
          >
            {{ importStatus }}
          </p>
          <p
            v-if="refreshSkipped > 0"
            class="service-card__muted"
            role="status"
          >
            {{ tc('serviceCard.refresh.skipped', refreshSkipped) }}
          </p>
        </template>

        <!-- Composing leaves nothing under the table: the sheet's height is the
             table's, and the footer sits directly under its last row. -->
        <p v-if="curating" class="service-card__aside">
          <button
            class="service-card__link service-card__link--grave"
            :disabled="interactionBusy"
            type="button"
            @click="onRemove"
          >
            <RvIcon name="trash" />
            {{ t('serviceCard.remove') }}
          </button>
        </p>
      </section>
    </div>

    <template v-if="composing && !creating" #footer>
      <p class="service-card__membership">
        <RvStatus
          :label="membershipLabel"
          :tone="included === true ? 'ready' : 'waiting'"
        />
        <small v-if="pending">{{ t('serviceCard.pending') }}</small>
      </p>
      <RvButton
        :disabled="disabled"
        variant="primary"
        @click="emit('include', included !== true)"
      >
        {{
          included === true ? t('serviceDetail.remove') : t('serviceDetail.add')
        }}
      </RvButton>
    </template>
  </RvDialog>

  <!-- Adding is one bounded act taken over the table, not a form growing out of
       the bottom of it: the panel arrives where the pointer already is, and the
       table it adds to stays visible behind it. -->
  <RvDialog
    :close-label="t('action.close')"
    :dismissible="!saving"
    :open="curating && addValuesOpen && service !== null"
    :title="t('serviceCard.domains.add')"
    variant="panel"
    @update:open="onAddValuesOpen"
  >
    <form
      id="service-add-entries-form"
      class="service-card__entries"
      @submit.prevent="onAddValues"
    >
      <RvField
        :error="addError"
        :hint="t('serviceCard.domains.hint')"
        input-id="service-add-entries"
        :label="t('serviceCard.domains.field')"
      >
        <template #default="{ describedBy, invalid }">
          <RvTextarea
            v-model="addValuesDraft"
            :described-by="describedBy"
            :disabled="saving"
            input-id="service-add-entries"
            :invalid="invalid"
            :rows="6"
          />
        </template>
      </RvField>
      <div class="service-card__entries-import">
        <RvFilePicker
          accept=".txt,.bat,.lst,.json,.csv,text/plain"
          :action-label="t('serviceCard.import')"
          :disabled="saving"
          :empty-label="t('serviceCard.import.none')"
          :hint="t('serviceCard.import.hint')"
          input-id="service-card-import"
          :label="t('serviceCard.import')"
          @select="onImportFile"
        />
      </div>
    </form>
    <template #footer>
      <RvButton
        :disabled="saving"
        variant="quiet"
        @click="onAddValuesOpen(false)"
      >
        {{ t('action.cancel') }}
      </RvButton>
      <RvButton
        :disabled="saving"
        :loading="saving"
        form="service-add-entries-form"
        type="submit"
        variant="primary"
      >
        {{ t('serviceCard.domains.submit') }}
      </RvButton>
    </template>
  </RvDialog>

  <!-- Which feeds this list reads is a set of managed objects, not a preamble
       to the table: it is opened when it is the subject and closed the rest of
       the time. -->
  <RvDialog
    :close-label="t('action.close')"
    :dismissible="!saving"
    :open="curating && sourcesOpen && service !== null"
    :title="t('serviceCard.sources')"
    variant="panel"
    @update:open="sourcesOpen = $event"
  >
    <section class="service-card__section">
      <p v-if="sourceError !== ''" class="service-card__error" role="alert">
        {{ sourceError }}
      </p>

      <ul v-if="sources.length > 0" class="service-card__rows">
        <li v-for="source in sources" :key="source.id">
          <label class="service-card__switch">
            <input
              :checked="source.enabled"
              :disabled="interactionBusy"
              type="checkbox"
              @change="
                onToggleSource(
                  source.id,
                  ($event.target as HTMLInputElement).checked,
                )
              "
            />
            <span class="service-card__row-copy">
              <strong>
                {{
                  source.custom && source.url !== '' ? source.url : source.id
                }}
              </strong>
              <small>{{ sourceLabel(source.id, source.type) }}</small>
            </span>
          </label>
          <button
            v-if="source.custom"
            :aria-label="
              t('serviceCard.feed.remove.aria', { source: source.url })
            "
            class="service-card__row-action"
            :disabled="interactionBusy"
            type="button"
            @click="onRemoveSource(source.id)"
          >
            {{ t('serviceCard.feed.remove') }}
          </button>
        </li>
      </ul>
      <p v-else class="service-card__muted">
        {{ t('serviceCard.sources.none') }}
      </p>

      <button
        v-if="!feedOpen"
        class="service-card__add"
        :disabled="interactionBusy"
        type="button"
        @click="feedOpen = true"
      >
        <RvIcon name="plus" />
        {{ t('serviceCard.feed.add') }}
      </button>
      <form
        v-else
        class="service-card__inline-form"
        @submit.prevent="onAddFeed"
      >
        <RvField
          :error="feedError"
          :hint="t('serviceCard.feed.hint')"
          input-id="service-feed-url"
          :label="t('serviceCard.feed.url')"
        >
          <template #default="{ describedBy, invalid }">
            <RvTextInput
              v-model="feedURL"
              :described-by="describedBy"
              :disabled="saving"
              input-id="service-feed-url"
              :invalid="invalid"
              placeholder="https://example.com/list.txt"
            />
          </template>
        </RvField>
        <RvField
          input-id="service-feed-format"
          :label="t('serviceCard.feed.format')"
        >
          <template #default="{ describedBy }">
            <RvSelect
              v-model="feedFormat"
              :described-by="describedBy"
              :disabled="saving"
              input-id="service-feed-format"
              :options="feedFormats"
              :placeholder="t('serviceCard.feed.format')"
            />
          </template>
        </RvField>
        <div class="service-card__actions">
          <RvButton
            :disabled="saving"
            size="compact"
            variant="quiet"
            @click="feedOpen = false"
          >
            {{ t('action.cancel') }}
          </RvButton>
          <RvButton
            :disabled="saving"
            :loading="saving"
            size="compact"
            type="submit"
            variant="secondary"
          >
            {{ t('serviceCard.feed.submit') }}
          </RvButton>
        </div>
      </form>
    </section>
  </RvDialog>
</template>

<style scoped>
/* The sheet's body, as one column: the sections above keep their height and the
   contents section takes everything that is left. */
.service-card__body {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow-y: auto;
}

.service-card__section {
  display: grid;
  gap: var(--rv-space-4);
  padding: var(--rv-space-5) var(--rv-space-6);
}

.service-card__section + .service-card__section {
  border-top: var(--rv-border-hair) solid var(--rv-color-rule-strong);
}

.service-card__section--base {
  background: var(--rv-color-surface);
}

/* A column rather than a grid, because exactly one of its children — the
   table — is allowed to take the height the others do not need. */
.service-card__section--fill {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: var(--rv-space-4);
  min-height: min-content;
}

.service-card__heading {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2) var(--rv-space-3);
  align-items: center;
}

.service-card__heading h3 {
  font-size: var(--rv-text-interface);
}

.service-card__count {
  margin-left: auto;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

/* A fact the composing card states but does not offer to change: the same
   words as the control beside the table in the other flow, without the box
   that would promise something to press. */
.service-card__fact {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-dense);
}

/* A named action that is not the point of the screen: it reads as a control
   rather than a link, and it says what it opens and how much is in there. */
.service-card__quiet {
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

.service-card__quiet:hover {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-hover);
}

.service-card__quiet:disabled {
  color: var(--rv-color-ink-tertiary);
  background: transparent;
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.service-card__muted {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.service-card__error {
  color: var(--rv-color-status-failed);
  font-size: var(--rv-text-dense);
}

/* Source reads are a compact fact beside the table. Keeping the retry slot in
   the row even while it is hidden means loading, failure and recovery share
   the same geometry; the row itself can still grow for translated or zoomed
   copy instead of clipping it. */
.service-card__refresh-status {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: var(--rv-space-3);
  align-items: center;
  min-width: 0;
  min-height: var(--rv-control-touch);
}

.service-card__refresh-indicator {
  flex: 1 1 auto;
  min-width: 0;
}

.service-card__refresh-retry {
  flex: none;
}

.service-card__refresh-retry[aria-hidden='true'] {
  visibility: hidden;
}

.service-card__aside {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  align-items: center;
}

.service-card__link {
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

.service-card__link:hover {
  background: var(--rv-color-accent-quiet);
}

.service-card__link--grave {
  color: var(--rv-color-status-failed);
}

.service-card__link--grave:hover {
  background: var(--rv-color-surface-hover);
}

.service-card__link:disabled {
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.service-card__rename {
  display: flex;
  gap: var(--rv-space-3);
  align-items: flex-end;
}

.service-card__rename > :first-child {
  flex: 1;
}

.service-card__visually-hidden {
  position: absolute;
  width: 0.0625rem;
  height: 0.0625rem;
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}

/* The filter and the way in share one line above the table: the control that
   adds a row belongs where the rows are, not below everything they say. */
.service-card__toolbar {
  display: flex;
  gap: var(--rv-space-3);
  align-items: center;
}

.service-card__filter {
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

.service-card__filter:focus-within {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-border-hair);
}

.service-card__filter-input {
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

.service-card__filter-input::placeholder {
  color: var(--rv-color-ink-tertiary);
}

/* The table keeps its own scroll so the heading above it and the actions below
   it stay in place. In a panel it is capped; in the sheet it claims the height
   the sheet actually has, which is what keeps the footer off an empty band. */
.service-card__rows {
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

.service-card__section--fill .service-card__rows {
  flex: 1;
  max-height: none;

  /* Keep a usable table when enlarged controls consume the sheet. Size
     containment excludes the entire list from the section's intrinsic height;
     the body can then scroll its controls without replacing the table scroll. */
  min-height: calc(var(--rv-row-default) * 2);
  contain: size;
}

.service-card__rows li {
  display: flex;
  gap: var(--rv-space-4);
  align-items: center;
  justify-content: space-between;
  min-height: var(--rv-row-default);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.service-card__switch {
  display: flex;
  flex: 1;
  gap: var(--rv-space-3);
  align-items: center;
  min-width: 0;
  min-height: var(--rv-control-touch);
  padding: var(--rv-space-2) 0;
  cursor: pointer;
}

.service-card__switch input {
  flex: none;
  width: var(--rv-control-choice);
  height: var(--rv-control-choice);
  margin: 0;
  accent-color: var(--rv-color-accent);
}

.service-card__switch input:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-border-hair);
}

.service-card__row-copy {
  display: grid;
  gap: var(--rv-space-1);
  min-width: 0;
}

/* A composing row is read, not pressed, so it takes the switch's box without
   the switch and the two flows still read as one table. */
.service-card__entry {
  flex: 1;
  align-content: center;
  min-height: var(--rv-control-touch);
  padding: var(--rv-space-2) 0;
}

.service-card__row-copy strong {
  overflow-wrap: anywhere;
}

.service-card__row-copy small {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}

.service-card__value {
  font-family: var(--rv-font-mono);
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}

.service-card__row-action {
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

.service-card__row-action:hover {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-hover);
}

.service-card__add {
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

.service-card__add:hover {
  background: var(--rv-color-accent-quiet);
}

.service-card__add:disabled {
  background: transparent;
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

/* The file control is driven by the button beside it. It stays in the document
   so the browser can open the picker, and out of the tab order so the operator
   meets one control rather than two. */
.service-card__file {
  position: absolute;
  width: 0.0625rem;
  height: 0.0625rem;
  overflow: hidden;
  clip-path: inset(50%);
}

.service-card__add:focus-visible,
.service-card__link:focus-visible,
.service-card__row-action:focus-visible,
.service-card__quiet:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-focus-offset);
}

.service-card__entries {
  display: grid;
  gap: var(--rv-space-4);
  padding: var(--rv-space-5) var(--rv-space-6);
}

/* Reading a file the operator already has is the same act as typing into the
   field above, so it stands beside it rather than under a heading of its own. */
.service-card__entries-import {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2) var(--rv-space-3);
  align-items: center;
}

.service-card__inline-form {
  display: grid;
  gap: var(--rv-space-3);
  padding: var(--rv-space-4);
  background: var(--rv-color-surface-muted);
  border-radius: var(--rv-radius-md);
}

.service-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  justify-content: flex-end;
}

/* One line: where the list stands in this route, and — while the route is an
   unsaved draft — that the answer is waiting on a save. */
.service-card__membership {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  align-items: baseline;
  min-width: 0;
}

.service-card__membership small {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}

@media (width <= 36rem) {
  .service-card__section,
  .service-card__entries {
    padding-right: var(--rv-space-4);
    padding-left: var(--rv-space-4);
  }

  /* Two controls that no longer fit one line take two rather than shrinking
     the filter to nothing. */
  .service-card__toolbar {
    flex-wrap: wrap;
  }

  .service-card__rename {
    align-items: stretch;
    flex-direction: column;
  }

  .service-card__actions > * {
    flex: 1 1 auto;
  }
}
</style>
