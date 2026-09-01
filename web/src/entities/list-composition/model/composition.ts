import { toRaw } from 'vue'

import type { CategoryDetail } from '@/shared/api/catalog'
import type { ListComposition } from '@/shared/api/lists'

export type CategorySelectionState = 'none' | 'partial' | 'all'

export function resolvedComposition(
  composition: ListComposition,
  categories: CategoryDetail[],
): string[] {
  const resolved = new Set(composition.services)
  for (const id of composition.categories) {
    const category = categories.find((entry) => entry.id === id)
    for (const serviceID of category?.services ?? []) resolved.add(serviceID)
  }
  for (const id of composition.exclusions) resolved.delete(id)
  return [...resolved].sort()
}

export function categoryServices(
  composition: ListComposition,
  categories: CategoryDetail[],
): Set<string> {
  const carried = new Set<string>()
  for (const id of composition.categories) {
    const category = categories.find((entry) => entry.id === id)
    for (const serviceID of category?.services ?? []) carried.add(serviceID)
  }
  return carried
}

export function serviceIncluded(
  composition: ListComposition,
  categories: CategoryDetail[],
  serviceID: string,
): boolean {
  return resolvedComposition(composition, categories).includes(serviceID)
}

export function categorySelectionState(
  composition: ListComposition,
  categories: CategoryDetail[],
  categoryID: string,
): CategorySelectionState {
  const members =
    categories.find((category) => category.id === categoryID)?.services ?? []
  if (members.length === 0) return 'none'
  const included = members.filter((serviceID) =>
    serviceIncluded(composition, categories, serviceID),
  ).length
  if (included === 0) return 'none'
  return included === members.length ? 'all' : 'partial'
}

export function toggleCompositionService(
  composition: ListComposition,
  categories: CategoryDetail[],
  serviceID: string,
): ListComposition {
  const next = cloneComposition(composition)
  const carried = categoryServices(next, categories)
  if (serviceIncluded(next, categories, serviceID)) {
    next.services = next.services.filter((id) => id !== serviceID)
    if (carried.has(serviceID)) next.exclusions.push(serviceID)
  } else {
    next.exclusions = next.exclusions.filter((id) => id !== serviceID)
    if (!carried.has(serviceID)) next.services.push(serviceID)
  }
  return normalizeComposition(next, categories)
}

export function toggleCompositionCategory(
  composition: ListComposition,
  categories: CategoryDetail[],
  categoryID: string,
): ListComposition {
  const next = cloneComposition(composition)
  const members =
    categories.find((category) => category.id === categoryID)?.services ?? []
  if (members.length === 0) return normalizeComposition(next, categories)

  const selectAll =
    categorySelectionState(next, categories, categoryID) !== 'all'
  if (selectAll) {
    if (!next.categories.includes(categoryID)) next.categories.push(categoryID)
    next.exclusions = next.exclusions.filter((id) => !members.includes(id))
    // A selected reference already carries its members. Keeping the same ids
    // as explicit picks would preserve a duplicate explanation in storage.
    next.services = next.services.filter((id) => !members.includes(id))
  } else {
    next.categories = next.categories.filter((id) => id !== categoryID)
    next.services = next.services.filter((id) => !members.includes(id))
    // A member may still be carried by another selected category. The parent
    // checkbox controls the visible children, so preserve the user's clear-all
    // intent as an exclusion in that overlap case.
    const stillCarried = categoryServices(next, categories)
    for (const id of members) {
      if (stillCarried.has(id) && !next.exclusions.includes(id))
        next.exclusions.push(id)
    }
  }
  const stillCarried = categoryServices(next, categories)
  next.exclusions = next.exclusions.filter((id) => stillCarried.has(id))
  return normalizeComposition(next, categories)
}

export function normalizeComposition(
  composition: ListComposition,
  categories: CategoryDetail[] = [],
): ListComposition {
  const normalized: ListComposition = {
    services: [...new Set(composition.services)].sort(),
    categories: [...new Set(composition.categories)].sort(),
    exclusions: [...new Set(composition.exclusions)].sort(),
    serviceDomains: {},
  }
  const included = new Set(resolvedComposition(normalized, categories))
  for (const [serviceID, domains] of Object.entries(
    composition.serviceDomains ?? {},
  )) {
    if (!included.has(serviceID)) continue
    normalized.serviceDomains[serviceID] = [...new Set(domains)].sort()
  }
  return normalized
}

/**
 * What a composition selects, written the same way every time: two drafts that
 * resolve to the same collections, services, exceptions and per-service domains
 * read alike however they were assembled. "Changed" has to mean a different
 * selection rather than a different order before a save control can be
 * believed.
 */
export function compositionSignature(
  composition: ListComposition,
  categories: CategoryDetail[] = [],
): string {
  const normalized = normalizeComposition(composition, categories)
  return JSON.stringify([
    normalized.services,
    normalized.categories,
    normalized.exclusions,
    Object.keys(normalized.serviceDomains)
      .sort()
      .map((id) => [id, normalized.serviceDomains[id]]),
  ])
}

/**
 * A composition is plain data, and the structured-clone algorithm refuses a
 * reactive proxy outright, so every field is taken as the object its proxy
 * stands for. Reactivity is shallow at each of those objects, which is why one
 * unwrapping per field reaches all the way down.
 */
export function cloneComposition(
  composition: ListComposition,
): ListComposition {
  const source = toRaw(composition)
  return structuredClone({
    services: toRaw(source.services),
    categories: toRaw(source.categories),
    exclusions: toRaw(source.exclusions),
    serviceDomains: toRaw(source.serviceDomains ?? {}),
  })
}
