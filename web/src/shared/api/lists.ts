import * as v from 'valibot'

import {
  count,
  decode,
  type Decoder,
  fields,
  getJSON,
  jsonObject,
  optionalFlag,
  optionalText,
  optionalTimestamp,
  postJSON,
  text,
  texts,
  timestamp,
} from './http'
import { type OutputCard, outputCardsSchema } from './outputs'

// What a list is made of, as the operator said it: the services it names, the
// categories it references, and what it took back out of them. Resolution
// happens on the server, so this stays the stored shape rather than the result.
export type ListComposition = {
  services: string[]
  categories: string[]
  exclusions: string[]
  serviceDomains: Record<string, string[]>
}

// A list is the unit the operator works with: a name and a composition, with no
// target of its own.
export type RouteList = ListComposition & {
  id: string
  name: string
  refreshInterval: RefreshInterval
  lastRefreshedAt: string
  lastRefreshFailed: boolean
  // When the list left the shelf, empty while it is on it. It is one fact
  // rather than a flag beside a date, so nothing can report an archived list
  // with no date or a date with no archival.
  archivedAt: string
  createdAt: string
  updatedAt: string
}

// An archived list is read from its date. The helper exists so no screen has to
// remember which of the two questions the field answers.
export function isArchived(list: { archivedAt: string }): boolean {
  return list.archivedAt !== ''
}

// One row of the library: a list and the outputs it feeds.
export type ListCard = RouteList & {
  // What the list publishes right now: named services plus every category's
  // members, minus exclusions, deduplicated by the server.
  resolved: string[]
  // References the catalog no longer supplies. The list keeps working on what
  // remains, and the screen says which reference is gone.
  missingCategories: string[]
  outputs: OutputCard[]
}

// How a list is refreshed right now, and where that rule came from. The server
// resolves it: a screen that combined the default with the list's own value
// would have to repeat the precedence rule and could disagree with the timer.
export type Schedule = {
  interval: RefreshInterval
  effective: RefreshInterval
  followsDefault: boolean
  lastRefreshedAt: string
  lastRefreshFailed: boolean
  nextRefreshAt: string
}

// The closed set of scheduling rules. An empty value is not "unset": it is the
// list deferring to the service-wide default, which is a different statement
// from naming the default's current value.
export type RefreshInterval = '' | 'off' | 'daily' | 'weekly'

export type ListDetail = {
  list: RouteList
  outputs: OutputCard[]
  schedule: Schedule
  // Resolution is the server's answer, not the screen's: a stale catalog copy
  // would disagree with what the next build actually plans.
  resolved: string[]
  missingCategories: string[]
}

export function createList(
  name: string,
  composition: ListComposition,
): Promise<RouteList> {
  return postJSON(
    '/v1/lists',
    {
      name,
      services: composition.services,
      categories: composition.categories,
      exclusions: composition.exclusions,
      service_domains: composition.serviceDomains,
    },
    parseListEnvelope,
  )
}

// What a composition would weigh, per format, before anything is stored. The
// answer belongs to the server: the same planner that will build the file
// decides how many rules a service contributes, and a screen that guessed would
// disagree with the build it is trying to predict.
export type ForecastService = {
  serviceID: string
  rules: number
}

export type TargetForecast = {
  targetID: string
  // Zero means the format states no bound, which is a different fact from a
  // bound that happens to be large.
  maximumRules: number
  // The total the build would produce. Per-service shares overlap where two
  // services name the same destination, so they can add up to more than this;
  // the total is stated rather than derived for exactly that reason.
  projectedRules: number
  fits: boolean
  // Every resolved service, including the ones that contribute nothing.
  perService: ForecastService[]
}

/**
 * previewComposition reads what a draft composition would produce. It stores
 * nothing and publishes nothing, so it is safe to call while the operator is
 * still choosing. An empty target list asks about every format the build knows,
 * and the answer arrives sorted by target identifier rather than in the order
 * asked, so callers look a format up by its id.
 *
 * The server refuses a composition that resolves to no service, one that names
 * something the catalog does not carry, and one nothing has ever observed.
 * Every refusal reaches the caller as a rejection, and callers read that as
 * "no forecast" rather than as a fit.
 */
export function previewComposition(
  composition: ListComposition,
  targets: string[] = [],
): Promise<TargetForecast[]> {
  return postJSON(
    '/v1/lists/preview',
    {
      services: composition.services,
      categories: composition.categories,
      exclusions: composition.exclusions,
      service_domains: composition.serviceDomains,
      ...(targets.length === 0 ? {} : { targets }),
    },
    parseForecasts,
  )
}

export function updateList(
  listID: string,
  name: string,
  composition: ListComposition,
): Promise<RouteList> {
  return postJSON(
    `/v1/lists/${listID}/update`,
    {
      name,
      services: composition.services,
      categories: composition.categories,
      exclusions: composition.exclusions,
      service_domains: composition.serviceDomains,
    },
    parseListEnvelope,
  )
}

// Archiving takes a list off the shelf; nothing it published is removed and its
// subscription keeps resolving. The reply is the list, so the caller reads the
// resulting state rather than assuming the verb it sent.
export function archiveList(listID: string): Promise<RouteList> {
  return postJSON(`/v1/lists/${listID}/archive`, {}, parseListEnvelope)
}

export function restoreList(listID: string): Promise<RouteList> {
  return postJSON(`/v1/lists/${listID}/restore`, {}, parseListEnvelope)
}

export async function refreshList(listID: string): Promise<void> {
  await postJSON(`/v1/lists/${listID}/refresh`, {}, parseRefresh)
}

export function loadList(listID: string): Promise<ListDetail> {
  return getJSON(`/v1/lists/${listID}`, parseListDetail)
}

export function loadLists(): Promise<ListCard[]> {
  return getJSON('/v1/lists', parseListCards)
}

export function saveListRefreshInterval(
  listID: string,
  interval: RefreshInterval,
): Promise<Schedule> {
  return postJSON(
    `/v1/lists/${listID}/schedule`,
    { refresh_interval: interval },
    parseScheduleEnvelope,
  )
}

// An unrecognised stored rule reads as off: a timer nobody implements must not
// be inferred from a value nobody wrote. A rule the reply does not state at all
// is the other answer -- the empty rule, which is the list deferring to the
// service-wide default rather than naming one.
const statedRule = v.optional(optionalText, '')

export const refreshRule = v.fallback(
  v.pipe(statedRule, v.picklist(['', 'off', 'daily', 'weekly'])),
  'off',
)

export function refreshInterval(value: unknown): RefreshInterval {
  return readRefreshInterval(value) ?? 'off'
}

// A service the list names carries the operator's own additions for that
// service. An absent map is an empty one; a map that is not a map of names is a
// reply outside the contract.
const serviceDomainsSchema = v.nullish(
  v.pipe(jsonObject, v.record(v.string(), texts)),
  {},
)

const listEntries = {
  id: text,
  name: text,
  services: texts,
  categories: texts,
  exclusions: texts,
  service_domains: serviceDomainsSchema,
  refresh_interval: refreshRule,
  last_refreshed_at: optionalTimestamp,
  last_refresh_failed: optionalFlag,
  archived_at: optionalTimestamp,
  created_at: timestamp,
  updated_at: timestamp,
}

const listShape = fields(listEntries)

function asRouteList(list: v.InferOutput<typeof listShape>): RouteList {
  return {
    id: list.id,
    name: list.name,
    services: list.services,
    categories: list.categories,
    exclusions: list.exclusions,
    serviceDomains: list.service_domains,
    refreshInterval: list.refresh_interval,
    lastRefreshedAt: list.last_refreshed_at,
    lastRefreshFailed: list.last_refresh_failed,
    archivedAt: list.archived_at,
    createdAt: list.created_at,
    updatedAt: list.updated_at,
  }
}

const listSchema = v.pipe(listShape, v.transform(asRouteList))

const listEnvelopeSchema = v.pipe(
  fields({ list: listSchema }),
  v.transform((envelope): RouteList => envelope.list),
)

const scheduleSchema = v.pipe(
  fields({
    interval: refreshRule,
    effective: refreshRule,
    follows_default: optionalFlag,
    last_refreshed_at: optionalTimestamp,
    last_refresh_failed: optionalFlag,
    next_refresh_at: optionalTimestamp,
  }),
  v.transform((schedule): Schedule => ({
    interval: schedule.interval,
    effective: schedule.effective,
    followsDefault: schedule.follows_default,
    lastRefreshedAt: schedule.last_refreshed_at,
    lastRefreshFailed: schedule.last_refresh_failed,
    nextRefreshAt: schedule.next_refresh_at,
  })),
)

const scheduleEnvelopeSchema = v.pipe(
  fields({ schedule: scheduleSchema }),
  v.transform((envelope): Schedule => envelope.schedule),
)

const listDetailSchema = v.pipe(
  fields({
    list: listSchema,
    outputs: outputCardsSchema,
    resolved: texts,
    missing_categories: texts,
    schedule: scheduleSchema,
  }),
  v.transform((detail): ListDetail => ({
    list: detail.list,
    outputs: detail.outputs,
    resolved: detail.resolved,
    missingCategories: detail.missing_categories,
    schedule: detail.schedule,
  })),
)

// A library row states the list and what it publishes in one object, so the
// card reads both out of the same record rather than asking twice.
const listCardSchema = v.pipe(
  fields({
    ...listEntries,
    resolved: texts,
    missing_categories: texts,
    outputs: outputCardsSchema,
  }),
  v.transform((card): ListCard => ({
    ...asRouteList(card),
    resolved: card.resolved,
    missingCategories: card.missing_categories,
    outputs: card.outputs,
  })),
)

const listCardsSchema = v.pipe(
  fields({ lists: v.array(listCardSchema) }),
  v.transform((library): ListCard[] => library.lists),
)

const forecastServiceSchema = v.pipe(
  fields({ service_id: text, rules: count }),
  v.transform((entry): ForecastService => ({
    serviceID: entry.service_id,
    rules: entry.rules,
  })),
)

const forecastSchema = v.pipe(
  fields({
    target_id: text,
    maximum_rules: count,
    projected_rules: count,
    fits: v.boolean(),
    per_service: v.array(forecastServiceSchema),
  }),
  v.transform((forecast): TargetForecast => ({
    targetID: forecast.target_id,
    maximumRules: forecast.maximum_rules,
    projectedRules: forecast.projected_rules,
    fits: forecast.fits,
    perService: forecast.per_service,
  })),
)

const forecastsSchema = v.pipe(
  fields({ targets: v.array(forecastSchema) }),
  v.transform((preview): TargetForecast[] => preview.targets),
)

// The refresh reply states which sources it went to. The screen shows none of
// them, so the list is admitted without being read further.
const refreshSchema = v.pipe(
  fields({ refresh: v.array(v.unknown()) }),
  v.transform((): true => true),
)

const readRefreshInterval: Decoder<RefreshInterval> = decode(refreshRule)
const parseListEnvelope: Decoder<RouteList> = decode(listEnvelopeSchema)
const parseScheduleEnvelope: Decoder<Schedule> = decode(scheduleEnvelopeSchema)
const parseListDetail: Decoder<ListDetail> = decode(listDetailSchema)
const parseListCards: Decoder<ListCard[]> = decode(listCardsSchema)
const parseForecasts: Decoder<TargetForecast[]> = decode(forecastsSchema)
const parseRefresh: Decoder<true> = decode(refreshSchema)
