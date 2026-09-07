import { toRaw } from 'vue'

import type { CategoryDetail, ListDetail } from '@/shared/api/catalog'
import type { ProfileComposition, TargetForecast } from '@/shared/api/profiles'

export type CategorySelectionState = 'none' | 'partial' | 'all'

export function resolvedComposition(
  composition: ProfileComposition,
  categories: CategoryDetail[],
): string[] {
  const resolved = new Set(composition.lists)
  for (const id of composition.categories) {
    const category = categories.find((entry) => entry.id === id)
    for (const listID of category?.lists ?? []) resolved.add(listID)
  }
  for (const id of composition.exclusions) resolved.delete(id)
  const remaining = [...resolved].sort()
  const ordered: string[] = []
  for (const id of composition.priority ?? []) {
    if (!resolved.has(id) || ordered.includes(id)) continue
    ordered.push(id)
    resolved.delete(id)
  }
  return [...ordered, ...remaining.filter((id) => resolved.has(id))]
}

export function categoryListIDs(
  composition: ProfileComposition,
  categories: CategoryDetail[],
): Set<string> {
  const carried = new Set<string>()
  for (const id of composition.categories) {
    const category = categories.find((entry) => entry.id === id)
    for (const listID of category?.lists ?? []) carried.add(listID)
  }
  return carried
}

export function listIncluded(
  composition: ProfileComposition,
  categories: CategoryDetail[],
  listID: string,
): boolean {
  return resolvedComposition(composition, categories).includes(listID)
}

export function categorySelectionState(
  composition: ProfileComposition,
  categories: CategoryDetail[],
  categoryID: string,
): CategorySelectionState {
  const members =
    categories.find((category) => category.id === categoryID)?.lists ?? []
  if (members.length === 0) return 'none'
  const included = members.filter((listID) =>
    listIncluded(composition, categories, listID),
  ).length
  if (included === 0) return 'none'
  return included === members.length ? 'all' : 'partial'
}

export function toggleCompositionList(
  composition: ProfileComposition,
  categories: CategoryDetail[],
  listID: string,
): ProfileComposition {
  const next = cloneComposition(composition)
  const carried = categoryListIDs(next, categories)
  if (listIncluded(next, categories, listID)) {
    next.lists = next.lists.filter((id) => id !== listID)
    if (carried.has(listID)) next.exclusions.push(listID)
  } else {
    next.exclusions = next.exclusions.filter((id) => id !== listID)
    if (!carried.has(listID)) next.lists.push(listID)
  }
  return normalizeComposition(next, categories)
}

export function toggleCompositionCategory(
  composition: ProfileComposition,
  categories: CategoryDetail[],
  categoryID: string,
): ProfileComposition {
  const next = cloneComposition(composition)
  const members =
    categories.find((category) => category.id === categoryID)?.lists ?? []
  if (members.length === 0) return normalizeComposition(next, categories)

  const selectAll =
    categorySelectionState(next, categories, categoryID) !== 'all'
  if (selectAll) {
    if (!next.categories.includes(categoryID)) next.categories.push(categoryID)
    next.exclusions = next.exclusions.filter((id) => !members.includes(id))
    // A selected reference already carries its members. Keeping the same ids
    // as explicit picks would preserve a duplicate explanation in storage.
    next.lists = next.lists.filter((id) => !members.includes(id))
  } else {
    next.categories = next.categories.filter((id) => id !== categoryID)
    next.lists = next.lists.filter((id) => !members.includes(id))
    // A member may still be carried by another selected category. The parent
    // checkbox controls the visible children, so preserve the user's clear-all
    // intent as an exclusion in that overlap case.
    const stillCarried = categoryListIDs(next, categories)
    for (const id of members) {
      if (stillCarried.has(id) && !next.exclusions.includes(id))
        next.exclusions.push(id)
    }
  }
  const stillCarried = categoryListIDs(next, categories)
  next.exclusions = next.exclusions.filter((id) => stillCarried.has(id))
  return normalizeComposition(next, categories)
}

/** Set whether a draft follows one live category reference. */
export function setCompositionCategoryReference(
  composition: ProfileComposition,
  categories: CategoryDetail[],
  categoryID: string,
  selected: boolean,
): ProfileComposition {
  const next = cloneComposition(composition)
  const members =
    categories.find((category) => category.id === categoryID)?.lists ?? []
  if (members.length === 0) return normalizeComposition(next, categories)

  if (selected) {
    if (!next.categories.includes(categoryID)) next.categories.push(categoryID)
    next.lists = next.lists.filter((id) => !members.includes(id))
    next.exclusions = next.exclusions.filter((id) => !members.includes(id))
  } else {
    next.categories = next.categories.filter((id) => id !== categoryID)
    next.lists = next.lists.filter((id) => !members.includes(id))
    const stillCarried = categoryListIDs(next, categories)
    next.exclusions = next.exclusions.filter((id) => stillCarried.has(id))
  }
  return normalizeComposition(next, categories)
}

export function normalizeComposition(
  composition: ProfileComposition,
  categories: CategoryDetail[] = [],
): ProfileComposition {
  const normalized: ProfileComposition = {
    lists: [...new Set(composition.lists)].sort(),
    categories: [...new Set(composition.categories)].sort(),
    exclusions: [...new Set(composition.exclusions)].sort(),
    listDomains: {},
  }
  const includedIDs = resolvedComposition(normalized, categories)
  const included = new Set(includedIDs)
  normalized.priority = [...new Set(composition.priority ?? [])].filter((id) =>
    included.has(id),
  )
  for (const id of includedIDs) {
    if (!normalized.priority.includes(id)) normalized.priority.push(id)
  }
  for (const [listID, domains] of Object.entries(
    composition.listDomains ?? {},
  )) {
    if (!included.has(listID)) continue
    normalized.listDomains[listID] = [...new Set(domains)].sort()
  }
  return normalized
}

/**
 * Give a new draft the library's default order without turning that global
 * preference into a live dependency. The caller decides when to stop applying
 * it; existing routes keep using their stored priority.
 */
export function applyDefaultPriority(
  composition: ProfileComposition,
  categories: CategoryDetail[],
  defaultPriority: string[],
): ProfileComposition {
  const included = new Set(resolvedComposition(composition, categories))
  const priority = defaultPriority.filter((id) => included.delete(id))
  priority.push(...[...included].sort())
  return normalizeComposition(
    { ...cloneComposition(composition), priority },
    categories,
  )
}

/**
 * The summary is optional for compatibility with an older local service. A
 * missing summary is unknown, not proof that the list has no intersections.
 */
export function overlapListIDs(
  forecast: TargetForecast | null | undefined,
  listID: string,
): string[] | null {
  const summary = forecast?.overlaps?.summary
  if (summary === undefined) return null
  return summary.find((entry) => entry.listID === listID)?.overlaps ?? null
}

/**
 * Titles are operator-owned and therefore are not identifiers. Keep the normal
 * label terse, but add the shortest stable id prefix when two lists share the
 * same title so an overlap tag can still name the row it refers to.
 */
export function listIdentityLabels(lists: ListDetail[]): Map<string, string> {
  const byTitle = new Map<string, ListDetail[]>()
  for (const list of lists) {
    const peers = byTitle.get(list.title) ?? []
    peers.push(list)
    byTitle.set(list.title, peers)
  }

  const labels = new Map<string, string>()
  for (const list of lists) {
    const peers = byTitle.get(list.title) ?? []
    if (peers.length < 2) {
      labels.set(list.id, list.title)
      continue
    }
    let length = 4
    while (
      length < list.id.length &&
      peers.some(
        (peer) =>
          peer.id !== list.id &&
          peer.id.slice(0, length) === list.id.slice(0, length),
      )
    )
      length += 1
    const prefix = list.id.slice(0, length)
    labels.set(
      list.id,
      `${list.title} · ${prefix}${length < list.id.length ? '…' : ''}`,
    )
  }
  return labels
}

/**
 * What a composition selects, written the same way every time: two drafts that
 * resolve to the same collections, lists, exceptions and per-list domains
 * read alike however they were assembled. "Changed" has to mean a different
 * selection rather than a different order before a save control can be
 * believed.
 */
export function compositionSignature(
  composition: ProfileComposition,
  categories: CategoryDetail[] = [],
): string {
  const normalized = normalizeComposition(composition, categories)
  return JSON.stringify([
    normalized.lists,
    normalized.categories,
    normalized.exclusions,
    normalized.priority,
    Object.keys(normalized.listDomains)
      .sort()
      .map((id) => [id, normalized.listDomains[id]]),
  ])
}

/**
 * A composition is plain data, and the structured-clone algorithm refuses a
 * reactive proxy outright, so every field is taken as the object its proxy
 * stands for. Reactivity is shallow at each of those objects, which is why one
 * unwrapping per field reaches all the way down.
 */
export function cloneComposition(
  composition: ProfileComposition,
): ProfileComposition {
  const source = toRaw(composition)
  return structuredClone({
    lists: toRaw(source.lists),
    categories: toRaw(source.categories),
    exclusions: toRaw(source.exclusions),
    listDomains: toRaw(source.listDomains ?? {}),
    priority: toRaw(source.priority ?? []),
  })
}

/** Move one resolved list without changing how the route references it. */
export function moveCompositionPriority(
  composition: ProfileComposition,
  categories: CategoryDetail[],
  from: number,
  to: number,
): ProfileComposition {
  const ordered = resolvedComposition(composition, categories)
  if (
    from < 0 ||
    to < 0 ||
    from >= ordered.length ||
    to >= ordered.length ||
    from === to
  )
    return cloneComposition(composition)
  const [moved] = ordered.splice(from, 1)
  if (moved === undefined) return cloneComposition(composition)
  ordered.splice(to, 0, moved)
  return normalizeComposition(
    { ...cloneComposition(composition), priority: ordered },
    categories,
  )
}
