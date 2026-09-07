import * as v from 'valibot'
import { serviceChanges } from '@/shared/lib/serviceChanges'

import {
  acknowledged,
  count,
  decode,
  type Decoder,
  fields,
  getJSON,
  optionalFlag,
  optionalText,
  postJSON,
  postNoContent,
  RoutevaneAPIError,
  text,
  texts,
} from './http'

export type ServiceDomain = {
  value: string
  includeSubdomains: boolean
}

export type ServiceSource = {
  id: string
  type: 'dns' | 'http'
}

export type ServiceDetail = {
  id: string
  title: string
  // Every catalog grouping that names this service. A service belongs to as
  // many as fit it, so this is a set rather than one label.
  categories: string[]
  // Optional for compatibility with a service binary built before catalog
  // details were exposed. loadCatalog normalizes both fields for current data.
  domains?: ServiceDomain[]
  sources?: ServiceSource[]
  sourceCount?: number
  // An operator-defined service. Its domains belong to the service itself and
  // are editable, unlike a shipped catalog entry.
  custom?: boolean
}

export type ServiceSourcePreview = {
  id: string
  type: 'dns' | 'http'
  status: 'ready' | 'failed'
  errorCode?: string
  domains: string[]
  domainCount: number
  addressCount: number
  prefixCount: number
  skippedCount: number
}

export type ServicePreview = {
  serviceID: string
  sources: ServiceSourcePreview[]
  domains: string[]
  domainCount: number
  addressCount: number
  prefixCount: number
  skippedCount: number
}

// A grouping of services. It owns no data of its own: a list that references it
// follows whatever the category currently carries.
//
// `custom` says who owns it (ADR 0028). A catalog category keeps the title the
// catalog gave it and cannot be removed; one the operator created can be
// renamed and removed. The server states it on every category, so the surface
// never has to guess which controls a category may be offered.
export type CategoryDetail = {
  id: string
  title: string
  services: string[]
  custom: boolean
}

// What a category edit states. An absent field and an empty one are different
// requests: omitting `services` leaves the membership alone, while an empty
// array clears it.
export type CategoryEdit = {
  title?: string
  services?: string[]
}

// A list named by a refusal — its identity and the words the operator gave it.
export type ListReference = {
  id: string
  title: string
}

// A target describes itself: what the operator calls it and what kind of file it
// receives. The screen never keeps its own list of device names or file types,
// so a target added to the catalog is named correctly without a UI change.
//
// The catalog states its own words first. An English rendition is optional per
// entry, so a device the catalog names only once still reads correctly in both
// locales rather than being hidden or half-translated.
export type TargetOption = {
  id: string
  title: string
  titleEn: string
  kind: 'router' | 'app'
  profileKey: string
  rendererID: string
  fileExtension: string
  manualInstallationHint: string
  manualInstallationHintEn: string
}

// One rule for every surface that prints a device or format name: the English
// rendition when the operator reads English and the catalog supplied one, the
// catalog's own words otherwise. The fallback carries a stored title for an
// output whose target has since left the catalog.
export function localizedTargetTitle(
  target: Pick<TargetOption, 'title' | 'titleEn'> | null | undefined,
  locale: string,
  fallback = '',
): string {
  if (target === null || target === undefined) return fallback
  if (locale === 'en' && target.titleEn !== '') return target.titleEn
  return target.title === '' ? fallback : target.title
}

export function localizedTargetHint(
  target:
    | Pick<TargetOption, 'manualInstallationHint' | 'manualInstallationHintEn'>
    | null
    | undefined,
  locale: string,
): string {
  if (target === null || target === undefined) return ''
  if (locale === 'en' && target.manualInstallationHintEn !== '')
    return target.manualInstallationHintEn
  return target.manualInstallationHint
}

export type Catalog = {
  services: string[]
  serviceDetails: ServiceDetail[]
  categories: CategoryDetail[]
  targets: TargetOption[]
  // Older binaries may omit the field; current servers always state the
  // complete library-wide order beside the service collection.
  defaultPriority?: string[]
}

export async function loadCatalog(): Promise<Catalog> {
  const [services, targets] = await Promise.all([
    getJSON('/v1/lists', parseServices),
    getJSON('/v1/targets', parseTargets),
  ])
  return { ...services, targets }
}

// saveDefaultPriority sends one complete service permutation and reads the
// server's normalized order back. Validation of membership and transactionality
// belongs to the application boundary, not this shared transport helper.
export async function saveDefaultPriority(
  priority: string[],
): Promise<string[]> {
  const saved = await postJSON(
    '/v1/lists/priority',
    { default_priority: priority },
    parseDefaultPriority,
  )
  invalidateCatalogCache()
  return saved
}

// Alias kept for callers that name the operation after the HTTP verb.
export const setDefaultPriority = saveDefaultPriority

// The catalog changes only when this tab writes a custom service or the
// process restarts, so one session-scoped copy saves a network round trip on
// every list open. Writes below invalidate it; a failure is never cached.
let cachedCatalog: Catalog | null = null

export async function loadCatalogCached(): Promise<Catalog> {
  if (cachedCatalog !== null) return cachedCatalog
  const catalog = await loadCatalog()
  cachedCatalog = catalog
  return catalog
}

export function invalidateCatalogCache(): void {
  cachedCatalog = null
}

export type CustomServiceRecord = {
  id: string
  title: string
  domains: string[]
}

// asServiceDetail is the catalog shape of a just-written custom service, so a
// screen can extend its loaded catalog without re-reading the collection.
export function asServiceDetail(record: CustomServiceRecord): ServiceDetail {
  return {
    id: record.id,
    title: record.title,
    categories: [],
    domains: record.domains.map((value) => ({
      value,
      includeSubdomains: true,
    })),
    sources: [],
    sourceCount: 0,
    custom: true,
  }
}

export async function createCustomService(
  title: string,
  domains: string[],
): Promise<CustomServiceRecord> {
  const record = await postJSON(
    '/v1/lists',
    { title, domains },
    parseCustomServiceEnvelope,
  )
  invalidateCatalogCache()
  return record
}

export async function updateCustomService(
  serviceID: string,
  title: string,
  domains: string[],
): Promise<CustomServiceRecord> {
  const record = await postJSON(
    `/v1/lists/${encodeURIComponent(serviceID)}/update`,
    { title, domains },
    parseCustomServiceEnvelope,
  )
  invalidateCatalogCache()
  await serviceChanges.trigger({ serviceID, observed: false })
  return record
}

/**
 * Categories the operator owns over the catalog seed (ADR 0028).
 *
 * Every reader of membership sees the merged result, so a write here reaches
 * every list that names the category on its next rebuild. That is the whole
 * point of a category, and it is why each of these invalidates the cached
 * catalog: the next screen to ask must not be handed the membership from
 * before the edit.
 */
export async function createCategory(
  title: string,
  services?: string[],
): Promise<CategoryDetail> {
  const category = await postJSON(
    '/v1/categories',
    services === undefined ? { title } : { lists: services, title },
    parseCategoryEnvelope,
  )
  invalidateCatalogCache()
  return category
}

// `services` is the whole membership the operator wants, not a delta: the
// server works out the overlay against the catalog seed itself. The body
// therefore carries only what the caller actually stated, because a field left
// out means "leave this alone" and an empty array means "make it empty".
export async function updateCategory(
  categoryID: string,
  edit: CategoryEdit,
): Promise<CategoryDetail> {
  const body: Record<string, unknown> = {}
  if (edit.title !== undefined) body.title = edit.title
  if (edit.services !== undefined) body.lists = edit.services
  const category = await postJSON(
    `/v1/categories/${encodeURIComponent(categoryID)}/update`,
    body,
    parseCategoryEnvelope,
  )
  invalidateCatalogCache()
  return category
}

// What becomes of the lists a deleted category held. The two answers are the
// whole question the operator is asked (ADR 0029), so the request states one of
// them rather than letting either be the silent default.
export type CategoryLists = 'detach' | 'delete'

// Answers with nothing but its status: the category is gone, or it is refused
// and nothing changed. A route still naming it is one of those refusals, and
// `categoryInUse` reads which routes they are.
export async function removeCategory(
  categoryID: string,
  lists: CategoryLists,
): Promise<void> {
  await postNoContent(
    `/v1/categories/${encodeURIComponent(categoryID)}/remove`,
    { lists },
  )
  invalidateCatalogCache()
}

// A list the operator removed, catalog-seeded or their own. The same shape of
// refusal guards it: a route naming the list directly keeps it.
export async function removeService(serviceID: string): Promise<void> {
  await postNoContent(`/v1/lists/${encodeURIComponent(serviceID)}/remove`, {})
  invalidateCatalogCache()
}

// The refusals that carry objects rather than a message: a category or a list a
// route still names is kept, and the reply names those routes so the screen can
// say which ones stand in the way. Anything else — another status, another
// shape — is not that refusal and answers null.
export function categoryInUse(reason: unknown): ListReference[] | null {
  return refusedBy(reason, parseCategoryInUse)
}

export function serviceInUse(reason: unknown): ListReference[] | null {
  return refusedBy(reason, parseServiceInUse)
}

function refusedBy(
  reason: unknown,
  read: Decoder<ListReference[]>,
): ListReference[] | null {
  if (!(reason instanceof RoutevaneAPIError) || reason.status !== 409)
    return null
  return read(reason.payload)
}

// The service card's one table: every destination the service stands for — a
// domain, an address or a network — each with its origin and the operator's
// switch, plus the automatic sources with their switches. It is the same
// material the next build reads.
export type ServiceContentsKind = 'domain' | 'ip' | 'prefix'

export type ServiceContentsRow = {
  value: string
  kind: ServiceContentsKind
  origin: string
  enabled: boolean
  missing: boolean
}

export type ServiceContentsSource = {
  id: string
  type: 'dns' | 'http'
  custom: boolean
  enabled: boolean
  url: string
}

export type ServiceContents = {
  serviceID: string
  rows: ServiceContentsRow[]
  sources: ServiceContentsSource[]
  observed: boolean
}

export type DomainVerdict = 'include' | 'exclude' | 'auto'

export function loadServiceContents(
  serviceID: string,
): Promise<ServiceContents> {
  return getJSON(
    `/v1/lists/${encodeURIComponent(serviceID)}/contents`,
    parseServiceContents,
  )
}

export type ServiceRefresh = { skippedEntries: number }

export async function refreshService(
  serviceID: string,
): Promise<ServiceRefresh> {
  const result = await postJSON(
    `/v1/lists/${encodeURIComponent(serviceID)}/refresh`,
    {},
    parseServiceRefresh,
  )
  await serviceChanges.trigger({ serviceID, observed: true })
  return result
}

export async function setServiceSourceEnabled(
  serviceID: string,
  sourceID: string,
  enabled: boolean,
): Promise<ServiceContents> {
  const result = await postJSON(
    `/v1/lists/${encodeURIComponent(serviceID)}/sources/${encodeURIComponent(sourceID)}/update`,
    { enabled },
    parseServiceContents,
  )
  await serviceChanges.trigger({ serviceID, observed: false })
  return result
}

export async function addServiceSource(
  serviceID: string,
  url: string,
  format: string,
): Promise<void> {
  await postJSON(
    `/v1/lists/${encodeURIComponent(serviceID)}/sources`,
    { format, url },
    parseAcknowledgement,
  )
  await serviceChanges.trigger({ serviceID, observed: false })
}

export async function removeServiceSource(
  serviceID: string,
  sourceID: string,
): Promise<ServiceContents> {
  const result = await postJSON(
    `/v1/lists/${encodeURIComponent(serviceID)}/sources/${encodeURIComponent(sourceID)}/remove`,
    {},
    parseServiceContents,
  )
  await serviceChanges.trigger({ serviceID, observed: false })
  return result
}

// One verdict, many values: the endpoint takes a batch so a pasted list or an
// imported routes file is one decision rather than a request per line. The
// server refuses the whole batch when a single value is malformed, which is why
// the caller validates before it sends.
export async function setServiceValues(
  serviceID: string,
  values: string[],
  verdict: DomainVerdict,
): Promise<ServiceContents> {
  const result = await postJSON(
    `/v1/lists/${encodeURIComponent(serviceID)}/domains`,
    { values, verdict },
    parseServiceContents,
  )
  await serviceChanges.trigger({ serviceID, observed: false })
  return result
}

export async function previewService(
  serviceID: string,
): Promise<ServicePreview> {
  return postJSON(
    `/v1/lists/${encodeURIComponent(serviceID)}/preview`,
    {},
    parseServicePreview,
  )
}

// A source states which of the two kinds it is. Anything else is a source this
// build does not know how to read, not a source to guess at.
const sourceType = v.picklist(['dns', 'http'])

const contentsRowSchema = fields({
  value: text,
  kind: v.picklist(['domain', 'ip', 'prefix']),
  origin: optionalText,
  enabled: optionalFlag,
  missing: optionalFlag,
})

const contentsSourceSchema = fields({
  id: text,
  type: sourceType,
  custom: optionalFlag,
  enabled: optionalFlag,
  url: optionalText,
})

const serviceContentsSchema = v.pipe(
  fields({
    list_id: text,
    rows: v.array(contentsRowSchema),
    sources: v.array(contentsSourceSchema),
    observed: optionalFlag,
  }),
  v.transform((contents): ServiceContents => ({
    serviceID: contents.list_id,
    rows: contents.rows,
    sources: contents.sources,
    observed: contents.observed,
  })),
)

const customServiceSchema = v.pipe(
  fields({ list: fields({ id: text, title: text, domains: texts }) }),
  v.transform((envelope): CustomServiceRecord => envelope.list),
)

const sourcePreviewSchema = v.pipe(
  fields({
    id: text,
    type: sourceType,
    status: v.picklist(['ready', 'failed']),
    error_code: v.optional(text),
    domains: texts,
    domain_count: count,
    address_count: count,
    prefix_count: count,
    skipped_count: count,
  }),
  v.transform((source): ServiceSourcePreview => ({
    id: source.id,
    type: source.type,
    status: source.status,
    // A source that succeeded states no code, and an absent code is not an
    // empty one: nothing prints a blank reason.
    ...(source.error_code === undefined
      ? {}
      : { errorCode: source.error_code }),
    domains: source.domains,
    domainCount: source.domain_count,
    addressCount: source.address_count,
    prefixCount: source.prefix_count,
    skippedCount: source.skipped_count,
  })),
)

const servicePreviewSchema = v.pipe(
  fields({
    list_id: text,
    sources: v.array(sourcePreviewSchema),
    domains: texts,
    domain_count: count,
    address_count: count,
    prefix_count: count,
    skipped_count: count,
  }),
  v.transform((preview): ServicePreview => ({
    serviceID: preview.list_id,
    sources: preview.sources,
    domains: preview.domains,
    domainCount: preview.domain_count,
    addressCount: preview.address_count,
    prefixCount: preview.prefix_count,
    skippedCount: preview.skipped_count,
  })),
)

const serviceDomainSchema = v.pipe(
  fields({ value: text, include_subdomains: v.boolean() }),
  v.transform((domain): ServiceDomain => ({
    value: domain.value,
    includeSubdomains: domain.include_subdomains,
  })),
)

const serviceSourceSchema = fields({ id: text, type: sourceType })

// A service binary older than catalog details states neither its domains nor
// its sources. That is an older server, not a broken one, so the fields are
// absent rather than refused, and a stated count wins over a counted one
// because the server may know of sources this reply does not carry.
const serviceDetailSchema = v.pipe(
  fields({
    id: text,
    title: text,
    categories: texts,
    domains: v.optional(v.array(serviceDomainSchema), []),
    sources: v.optional(v.array(serviceSourceSchema), []),
    source_count: v.optional(count),
    custom: optionalFlag,
  }),
  v.transform((detail): ServiceDetail => ({
    id: detail.id,
    title: detail.title,
    categories: detail.categories,
    domains: detail.domains,
    sources: detail.sources,
    sourceCount: detail.source_count ?? detail.sources.length,
    ...(detail.custom ? { custom: true } : {}),
  })),
)

// `custom` is required rather than optional: it decides which controls a
// category is offered, and a build that did not state it would have the surface
// guessing at ownership instead of refusing a reply outside the contract.
const categoryFields = fields({
  id: text,
  title: text,
  lists: texts,
  custom: v.boolean(),
})

// The wire already says `lists`; the screen's own property is renamed by the
// slice that owns internal identifiers, so the value is carried across here.
const categorySchema = v.pipe(
  categoryFields,
  v.transform((category): CategoryDetail => ({
    id: category.id,
    title: category.title,
    services: category.lists,
    custom: category.custom,
  })),
)

const categoryEnvelopeSchema = v.pipe(
  fields({ category: categorySchema }),
  v.transform((envelope): CategoryDetail => envelope.category),
)

// The refusal keys the profiles that hold the object being deleted (ADR 0039).
function inUseSchema(error: string) {
  return v.pipe(
    fields({
      error: v.literal(error),
      profiles: v.array(fields({ id: text, title: text })),
    }),
    v.transform((refused): ListReference[] => refused.profiles),
  )
}

const servicesSchema = v.pipe(
  fields({
    lists: texts,
    list_details: v.array(serviceDetailSchema),
    categories: v.array(categorySchema),
    default_priority: v.optional(texts),
  }),
  v.check((catalog) => {
    if (catalog.default_priority === undefined) return true
    if (catalog.default_priority.length !== catalog.lists.length) return false
    const available = new Set(catalog.lists)
    return (
      available.size === catalog.lists.length &&
      new Set(catalog.default_priority).size === available.size &&
      catalog.default_priority.every((serviceID) => available.has(serviceID))
    )
  }),
  v.transform((catalog): Omit<Catalog, 'targets'> => ({
    services: catalog.lists,
    serviceDetails: catalog.list_details,
    categories: catalog.categories,
    ...(catalog.default_priority === undefined
      ? {}
      : { defaultPriority: catalog.default_priority }),
  })),
)

const defaultPrioritySchema = v.pipe(
  fields({ default_priority: texts }),
  v.transform((value): string[] => value.default_priority),
)

const targetSchema = v.pipe(
  fields({
    id: text,
    title: text,
    // Absent is the normal case, not a fault: an entry with no English
    // rendition reads in the catalog's own words in both locales.
    title_en: optionalText,
    kind: v.picklist(['router', 'app']),
    profile_key: text,
    renderer_id: text,
    file_extension: text,
    manual_installation_hint: text,
    manual_installation_hint_en: optionalText,
  }),
  v.transform((target): TargetOption => ({
    id: target.id,
    title: target.title,
    titleEn: target.title_en,
    kind: target.kind,
    profileKey: target.profile_key,
    rendererID: target.renderer_id,
    fileExtension: target.file_extension,
    manualInstallationHint: target.manual_installation_hint,
    manualInstallationHintEn: target.manual_installation_hint_en,
  })),
)

const targetsSchema = v.pipe(
  fields({ targets: v.array(targetSchema) }),
  v.transform((catalog): TargetOption[] => catalog.targets),
)

const parseServiceContents: Decoder<ServiceContents> = decode(
  serviceContentsSchema,
)
const parseCustomServiceEnvelope: Decoder<CustomServiceRecord> =
  decode(customServiceSchema)
const parseServicePreview: Decoder<ServicePreview> =
  decode(servicePreviewSchema)
const parseCategoryEnvelope: Decoder<CategoryDetail> = decode(
  categoryEnvelopeSchema,
)
const parseCategoryInUse: Decoder<ListReference[]> = decode(
  inUseSchema('category in use'),
)
const parseServiceInUse: Decoder<ListReference[]> = decode(
  inUseSchema('list in use'),
)
const parseServices: Decoder<Omit<Catalog, 'targets'>> = decode(servicesSchema)
const parseDefaultPriority: Decoder<string[]> = decode(defaultPrioritySchema)
const parseTargets: Decoder<TargetOption[]> = decode(targetsSchema)
const parseAcknowledgement: Decoder<true> = decode(acknowledged)

const parseServiceRefresh: Decoder<ServiceRefresh> = decode(
  v.pipe(
    fields({ refresh: fields({ skipped_entries: v.optional(count, 0) }) }),
    v.transform(({ refresh }) => ({ skippedEntries: refresh.skipped_entries })),
  ),
)
