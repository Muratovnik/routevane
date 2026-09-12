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

// What a profile is made of, as the operator said it: the lists it names, the
// categories it references, and what it took back out of them. Resolution
// happens on the server, so this stays the stored shape rather than the result.
export type ProfileComposition = {
  lists: string[]
  categories: string[]
  exclusions: string[]
  listDomains: Record<string, string[]>
  /**
   * Resolved profile ids in descending ownership priority. It is stored beside
   * the live category references: a category may change later, so readers
   * append newly resolved ids after the order the operator already chose.
   */
  priority?: string[]
}

// A profile is the unit the operator works with: a name and a composition, with no
// target of its own.
export type ProfileRecord = ProfileComposition & {
  id: string
  name: string
  refreshInterval: RefreshInterval
  lastRefreshedAt: string
  lastRefreshFailed: boolean
  // When the profile left the shelf, empty while it is on it. It is one fact
  // rather than a flag beside a date, so nothing can report an archived profile
  // with no date or a date with no archival.
  archivedAt: string
  createdAt: string
  updatedAt: string
}

// An archived profile is read from its date. The helper exists so no screen has to
// remember which of the two questions the field answers.
export const isArchived = (profile: { archivedAt: string }): boolean =>
  profile.archivedAt !== ''

// One row of the library: a profile and the outputs it feeds.
export type ProfileCard = ProfileRecord & {
  // What the profile publishes right now: named lists plus every category's
  // members, minus exclusions, deduplicated by the server.
  resolved: string[]
  // References the catalog no longer supplies. The profile keeps working on what
  // remains, and the screen says which reference is gone.
  missingCategories: string[]
  outputs: OutputCard[]
}

// One bounded page of the profile library. nextCursor is empty after the final
// page; it is returned by the service and used only as the next path segment.
export type ProfileCardPage = {
  profiles: ProfileCard[]
  nextCursor: string
}

// How a profile is refreshed right now, and where that rule came from. The server
// resolves it: a screen that combined the default with the profile's own value
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
// profile deferring to the service-wide default, which is a different statement
// from naming the default's current value.
export type RefreshInterval = '' | 'off' | 'daily' | 'weekly'

export type ListDetail = {
  profile: ProfileRecord
  outputs: OutputCard[]
  schedule: Schedule
  // Resolution is the server's answer, not the screen's: a stale catalog copy
  // would disagree with what the next build actually plans.
  resolved: string[]
  missingCategories: string[]
}

export const createProfile = (
  name: string,
  composition: ProfileComposition,
): Promise<ProfileRecord> =>
  postJSON(
    '/v1/profiles',
    {
      name,
      lists: composition.lists,
      categories: composition.categories,
      exclusions: composition.exclusions,
      list_domains: composition.listDomains,
      priority: composition.priority ?? [],
    },
    parseProfileEnvelope,
  )

// What a composition would weigh, per format, before anything is stored. The
// answer belongs to the server: the same planner that will build the file
// decides how many rules a list contributes, and a screen that guessed would
// disagree with the build it is trying to predict.
export type ForecastList = {
  listID: string
  rules: number
}

export type OverlapValue = {
  ruleKind:
    'domain_exact' | 'domain_suffix' | 'ipv4' | 'ipv6' | 'prefix4' | 'prefix6'
  value: string
  lists: string[]
}

export type CompositionOverlap =
  | { kind: 'duplicate'; entry: OverlapValue; covering?: undefined }
  | { kind: 'covered'; entry: OverlapValue; covering: OverlapValue }

export type CompositionOverlaps = {
  items: CompositionOverlap[]
  truncated: boolean
  // Newer servers state one row for every selected list. It is optional in
  // the type so an older binary's bounded overlap response remains readable.
  summary?: OverlapSummary[]
}

export type OverlapSummary = {
  listID: string
  overlaps: string[]
}

export type TargetForecast = {
  incompleteLists?: string[]
  targetID: string
  // Zero means the format states no bound, which is a different fact from a
  // bound that happens to be large.
  maximumRules: number
  // The total the build would produce after profile-local priority assigns safe
  // overlaps to their winning profile.
  projectedRules: number
  fits: boolean
  // Every list with complete data, including one whose rules all belong
  // to a higher-priority profile. Incomplete lists are omitted, not zero.
  perList: ForecastList[]
  // Missing on an older server means unknown, never "no overlaps".
  overlaps?: CompositionOverlaps
}

/**
 * previewComposition reads what a draft composition would produce. It stores
 * nothing and publishes nothing, so it is safe to call while the operator is
 * still choosing. An empty target list asks about every format the build knows,
 * and the answer arrives sorted by target identifier rather than in the order
 * asked, so callers look a format up by its id.
 *
 * The server refuses a composition that resolves to no list, one that names
 * something the catalog does not carry, or a failed storage/renderer read.
 * Missing coverage is represented per target by incompleteLists; facts
 * for complete lists remain available without claiming the draft fits.
 */
export const previewComposition = (
  composition: ProfileComposition,
  targets: string[] = [],
): Promise<TargetForecast[]> =>
  postJSON(
    '/v1/profiles/preview',
    {
      lists: composition.lists,
      categories: composition.categories,
      exclusions: composition.exclusions,
      list_domains: composition.listDomains,
      priority: composition.priority ?? [],
      ...(targets.length === 0 ? {} : { targets }),
    },
    parseForecasts,
  )

export const updateProfile = (
  profileID: string,
  name: string,
  composition: ProfileComposition,
): Promise<ProfileRecord> =>
  postJSON(
    `/v1/profiles/${profileID}/update`,
    {
      name,
      lists: composition.lists,
      categories: composition.categories,
      exclusions: composition.exclusions,
      list_domains: composition.listDomains,
      priority: composition.priority ?? [],
    },
    parseProfileEnvelope,
  )

// Archiving takes a profile off the shelf; nothing it published is removed and its
// subscription keeps resolving. The reply is the profile, so the caller reads the
// resulting state rather than assuming the verb it sent.
export const archiveProfile = (profileID: string): Promise<ProfileRecord> =>
  postJSON(`/v1/profiles/${profileID}/archive`, {}, parseProfileEnvelope)

export const restoreProfile = (profileID: string): Promise<ProfileRecord> =>
  postJSON(`/v1/profiles/${profileID}/restore`, {}, parseProfileEnvelope)

export const refreshProfile = async (profileID: string): Promise<void> => {
  await postJSON(`/v1/profiles/${profileID}/refresh`, {}, parseRefresh)
}

export const loadProfile = (profileID: string): Promise<ListDetail> =>
  getJSON(`/v1/profiles/${profileID}`, parseProfileDetail)

export const loadProfiles = (): Promise<ProfileCard[]> =>
  getJSON('/v1/profiles', parseProfileCards)

export const loadProfilePage = (afterID = ''): Promise<ProfileCardPage> =>
  getJSON(
    afterID === '' ? '/v1/profiles' : `/v1/profile-pages/${afterID}`,
    parseProfileCardPage,
  )

export const saveProfileRefreshInterval = (
  profileID: string,
  interval: RefreshInterval,
): Promise<Schedule> =>
  postJSON(
    `/v1/profiles/${profileID}/schedule`,
    { refresh_interval: interval },
    parseScheduleEnvelope,
  )

// An unrecognised stored rule reads as off: a timer nobody implements must not
// be inferred from a value nobody wrote. A rule the reply does not state at all
// is the other answer -- the empty rule, which is the profile deferring to the
// service-wide default rather than naming one.
const statedRule = v.optional(optionalText, '')

export const refreshRule = v.fallback(
  v.pipe(statedRule, v.picklist(['', 'off', 'daily', 'weekly'])),
  'off',
)

export const refreshInterval = (value: unknown): RefreshInterval =>
  readRefreshInterval(value) ?? 'off'

// A list the profile names carries the operator's own additions for that
// list. An absent map is an empty one; a map that is not a map of names is a
// reply outside the contract.
const listDomainsSchema = v.nullish(
  v.pipe(jsonObject, v.record(v.string(), texts)),
  {},
)

const profileEntries = {
  id: text,
  name: text,
  lists: texts,
  categories: texts,
  exclusions: texts,
  list_domains: listDomainsSchema,
  priority: v.optional(texts, []),
  refresh_interval: refreshRule,
  last_refreshed_at: optionalTimestamp,
  last_refresh_failed: optionalFlag,
  archived_at: optionalTimestamp,
  created_at: timestamp,
  updated_at: timestamp,
}

const profileShape = fields(profileEntries)

const asProfileRecord = (
  profile: v.InferOutput<typeof profileShape>,
): ProfileRecord => ({
  id: profile.id,
  name: profile.name,
  lists: profile.lists,
  categories: profile.categories,
  exclusions: profile.exclusions,
  listDomains: profile.list_domains,
  priority: profile.priority,
  refreshInterval: profile.refresh_interval,
  lastRefreshedAt: profile.last_refreshed_at,
  lastRefreshFailed: profile.last_refresh_failed,
  archivedAt: profile.archived_at,
  createdAt: profile.created_at,
  updatedAt: profile.updated_at,
})

const profileSchema = v.pipe(profileShape, v.transform(asProfileRecord))

const profileEnvelopeSchema = v.pipe(
  fields({ profile: profileSchema }),
  v.transform((envelope): ProfileRecord => envelope.profile),
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
    profile: profileSchema,
    outputs: outputCardsSchema,
    resolved: texts,
    missing_categories: texts,
    schedule: scheduleSchema,
  }),
  v.transform((detail): ListDetail => ({
    profile: detail.profile,
    outputs: detail.outputs,
    resolved: detail.resolved,
    missingCategories: detail.missing_categories,
    schedule: detail.schedule,
  })),
)

// A library row states the profile and what it publishes in one object, so the
// card reads both out of the same record rather than asking twice.
const profileCardSchema = v.pipe(
  fields({
    ...profileEntries,
    resolved: texts,
    missing_categories: texts,
    outputs: outputCardsSchema,
  }),
  v.transform((card): ProfileCard => ({
    ...asProfileRecord(card),
    resolved: card.resolved,
    missingCategories: card.missing_categories,
    outputs: card.outputs,
  })),
)

const profileCardsSchema = v.pipe(
  fields({ profiles: v.array(profileCardSchema) }),
  v.transform((library): ProfileCard[] => library.profiles),
)

const profileCardPageSchema = v.pipe(
  fields({
    profiles: v.array(profileCardSchema),
    next: v.string(),
  }),
  v.transform((page): ProfileCardPage => ({
    profiles: page.profiles,
    nextCursor: page.next,
  })),
)

const forecastListSchema = v.pipe(
  fields({ list_id: text, rules: count }),
  v.transform((entry): ForecastList => ({
    listID: entry.list_id,
    rules: entry.rules,
  })),
)

const overlapValueSchema = v.pipe(
  fields({
    rule_kind: v.picklist([
      'domain_exact',
      'domain_suffix',
      'ipv4',
      'ipv6',
      'prefix4',
      'prefix6',
    ]),
    value: text,
    lists: v.pipe(texts, v.minLength(1)),
  }),
  v.transform((value): OverlapValue => ({
    ruleKind: value.rule_kind,
    value: value.value,
    lists: value.lists,
  })),
)

const overlapSchema = v.union([
  v.pipe(
    fields({ kind: v.literal('duplicate'), entry: overlapValueSchema }),
    v.check((item) => new Set(item.entry.lists).size > 1),
  ),
  v.pipe(
    fields({
      kind: v.literal('covered'),
      entry: overlapValueSchema,
      covering: overlapValueSchema,
    }),
    v.check(
      (item) =>
        item.entry.lists.length === 1 &&
        item.covering.lists.every((id) => !item.entry.lists.includes(id)),
    ),
  ),
])

const overlapSummarySchema = v.pipe(
  fields({ list_id: text, overlaps: texts }),
  v.check(
    (row) =>
      !row.overlaps.includes(row.list_id) &&
      new Set(row.overlaps).size === row.overlaps.length &&
      row.overlaps.every(
        (listID, index) => index === 0 || row.overlaps[index - 1]! < listID,
      ),
  ),
  v.transform((row): OverlapSummary => ({
    listID: row.list_id,
    overlaps: row.overlaps,
  })),
)

const overlapsSchema = v.pipe(
  fields({
    items: v.pipe(v.array(overlapSchema), v.maxLength(100)),
    truncated: v.boolean(),
    summary: v.optional(v.array(overlapSummarySchema)),
  }),
  v.check((overlaps) => {
    const summary = overlaps.summary
    if (summary === undefined) return true
    const byList = new Map(summary.map((row) => [row.listID, row]))
    if (byList.size !== summary.length) return false
    if (
      !summary.every(
        (row, index) => index === 0 || summary[index - 1]!.listID < row.listID,
      )
    ) {
      return false
    }
    return summary.every((row) =>
      row.overlaps.every(
        (other) => byList.get(other)?.overlaps.includes(row.listID) ?? false,
      ),
    )
  }),
  v.transform((overlaps): CompositionOverlaps => ({
    items: overlaps.items,
    truncated: overlaps.truncated,
    ...(overlaps.summary === undefined ? {} : { summary: overlaps.summary }),
  })),
)

const forecastSchema = v.pipe(
  fields({
    target_id: text,
    maximum_rules: count,
    projected_rules: count,
    fits: v.boolean(),
    per_list: v.array(forecastListSchema),
    overlaps: v.optional(overlapsSchema),
    incomplete_lists: v.optional(texts),
  }),
  v.transform((forecast): TargetForecast => ({
    targetID: forecast.target_id,
    ...(forecast.incomplete_lists === undefined
      ? {}
      : { incompleteLists: forecast.incomplete_lists }),
    maximumRules: forecast.maximum_rules,
    projectedRules: forecast.projected_rules,
    fits: forecast.fits,
    perList: forecast.per_list,
    ...(forecast.overlaps === undefined ? {} : { overlaps: forecast.overlaps }),
  })),
)

const forecastsSchema = v.pipe(
  fields({ targets: v.array(forecastSchema) }),
  v.transform((preview): TargetForecast[] => preview.targets),
)

// The refresh reply states which sources it went to. The screen shows none of
// them, so the profile is admitted without being read further.
const refreshSchema = v.pipe(
  fields({ refresh: v.array(v.unknown()) }),
  v.transform((): true => true),
)

const readRefreshInterval: Decoder<RefreshInterval> = decode(refreshRule)
const parseProfileEnvelope: Decoder<ProfileRecord> = decode(
  profileEnvelopeSchema,
)
const parseScheduleEnvelope: Decoder<Schedule> = decode(scheduleEnvelopeSchema)
const parseProfileDetail: Decoder<ListDetail> = decode(listDetailSchema)
const parseProfileCards: Decoder<ProfileCard[]> = decode(profileCardsSchema)
const parseProfileCardPage: Decoder<ProfileCardPage> = decode(
  profileCardPageSchema,
)
const parseForecasts: Decoder<TargetForecast[]> = decode(forecastsSchema)
const parseRefresh: Decoder<true> = decode(refreshSchema)
