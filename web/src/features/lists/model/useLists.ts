import { computed, ref } from 'vue'

import {
  asListDetail,
  categoryInUse,
  createCategory,
  createCustomList,
  invalidateCatalogCache,
  loadCatalogCached,
  removeCategory,
  removeList,
  saveDefaultPriority,
  listInUse,
  updateCategory,
  type Catalog,
  type CategoryDetail,
  type CategoryLists,
  type ProfileReference,
  type ListDetail,
} from '@/shared/api/catalog'

export type LibraryState = 'loading' | 'ready' | 'failed'

/**
 * The write and the read that follows it have separate outcomes. A successful
 * write whose read failed is still committed, but the copy on screen cannot be
 * treated as current until the operator retries the read.
 */
export type LibraryWriteStatus = 'failed' | 'blocked' | 'saved' | 'stale'

export type LibraryWriteResult<T = void> = {
  refusal?: LibraryRefusal
  status: LibraryWriteStatus
  value?: T
}

/**
 * What a write had to say when it did not go through. A refusal that names the
 * routes standing in the way is the only one carrying more than a sentence.
 */
export type LibraryRefusal = {
  kind: 'failed' | 'inUse'
  profiles: string[]
}

/**
 * The library of lists and the categories that hold them (ADR 0029).
 *
 * Every write here is followed by a re-read while the mutation guard is held:
 * this screen is the one place those objects are curated, and the copy it shows
 * has to be the copy it just wrote. When that read is unavailable, the previous
 * verified copy stays visible and is marked stale until a GET-only retry lands.
 * The cached catalog is dropped with each write, so a composing screen returning
 * to focus reads the same thing.
 */
export function useLists() {
  const catalog = ref<Catalog | null>(null)
  const state = ref<LibraryState>('loading')
  const mutating = ref(false)
  const refreshing = ref(false)
  const busy = computed(() => mutating.value || refreshing.value)
  const stale = ref(false)
  const refusal = ref<LibraryRefusal | null>(null)

  const lists = computed<ListDetail[]>(() => catalog.value?.listDetails ?? [])
  const categories = computed<CategoryDetail[]>(
    () => catalog.value?.categories ?? [],
  )
  const defaultPriority = computed<string[]>(() => {
    const listIDs = lists.value.map((list) => list.id)
    const available = new Set(listIDs)
    const saved = (catalog.value?.defaultPriority ?? []).filter((id) =>
      available.has(id),
    )
    const included = new Set(saved)
    return [...saved, ...listIDs.filter((id) => !included.has(id))]
  })

  async function initialize(): Promise<void> {
    if (busy.value) return
    state.value = 'loading'
    refreshing.value = true
    try {
      invalidateCatalogCache()
      catalog.value = await loadCatalogCached()
      stale.value = false
      state.value = 'ready'
    } catch {
      state.value = 'failed'
    } finally {
      refreshing.value = false
    }
  }

  function clearRefusal(): void {
    refusal.value = null
  }

  /**
   * Creation already returns the complete list identity. Add that authoritative
   * record to the visible catalog instead of blanking the library for a GET.
   * The API helper invalidates the shared cache, so another screen still reads
   * the server rather than this local projection.
   */
  async function addCustomList(
    title: string,
    domains: string[],
  ): Promise<LibraryWriteResult<ListDetail>> {
    if (busy.value || stale.value) return { status: 'blocked' }
    mutating.value = true
    refusal.value = null
    try {
      const detail = asListDetail(await createCustomList(title, domains))
      const current = catalog.value
      if (current !== null) {
        const details = new Map(
          [...current.listDetails, detail].map((list) => [list.id, list]),
        )
        // The server appends ids missing from the saved preference in
        // canonical list-id order. Keep the saved basis untouched and sort
        // only the live collection, so several unsaved creations produce the
        // same merged order without a corrective refresh.
        const listIDs = [...current.lists, detail.id].sort()
        catalog.value = {
          ...current,
          listDetails: listIDs
            .map((id) => details.get(id))
            .filter((list): list is ListDetail => list !== undefined),
          lists: listIDs,
        }
      }
      return { status: 'saved', value: detail }
    } catch {
      return { status: 'failed' }
    } finally {
      mutating.value = false
    }
  }

  /** A renamed custom list is equally authoritative and needs no catalog flash. */
  function acceptUpdatedList(detail: ListDetail): void {
    const current = catalog.value
    if (current === null) return
    catalog.value = {
      ...current,
      listDetails: current.listDetails.map((list) =>
        list.id === detail.id
          ? {
              ...list,
              ...detail,
              categories: list.categories,
              sourceCount: list.sourceCount,
              sources: list.sources,
            }
          : list,
      ),
    }
  }

  /**
   * Global priority is library state, not route state. Saving replaces only the
   * catalog's complete default permutation; existing routes are never written.
   */
  async function setDefaultPriority(
    priority: string[],
  ): Promise<LibraryWriteResult<string[]>> {
    if (busy.value || stale.value) return { status: 'blocked' }
    mutating.value = true
    refusal.value = null
    try {
      const saved = await saveDefaultPriority(priority)
      const current = catalog.value
      if (current !== null)
        catalog.value = { ...current, defaultPriority: saved }
      return { status: 'saved', value: saved }
    } catch {
      return { status: 'failed' }
    } finally {
      mutating.value = false
    }
  }

  /**
   * One write, then the read that proves it. A refusal leaves the catalog
   * alone and states itself; a re-read that fails after a write that did land
   * keeps the copy on screen rather than reporting the committed change as one
   * that did not happen.
   */
  type WriteKind = 'membership' | 'independent'

  async function readCatalog(): Promise<void> {
    invalidateCatalogCache()
    catalog.value = await loadCatalogCached()
    stale.value = false
    state.value = 'ready'
  }

  async function write<T>(
    mutate: () => Promise<T>,
    refused: (reason: unknown) => ProfileReference[] | null,
    kind: WriteKind = 'independent',
  ): Promise<LibraryWriteResult<T>> {
    // A membership edit states the whole membership. Rebuilding it from an
    // old copy would silently overwrite an edit made by the successful write,
    // so it remains unavailable until a fresh catalog is read. Independent
    // endpoint writes (rename/remove/create) do not derive that membership and
    // can still be offered, but every write remains serialized here.
    if (busy.value || (stale.value && kind === 'membership'))
      return { status: 'blocked' }

    mutating.value = true
    refusal.value = null
    let value: T
    try {
      value = await mutate()
    } catch (reason) {
      let holders: ProfileReference[] | null
      try {
        holders = refused(reason)
      } catch {
        // A malformed refusal payload is still a write failure; it must not
        // strand the mutation guard or make a failed request look committed.
        holders = null
      }
      const nextRefusal: LibraryRefusal =
        holders === null
          ? { kind: 'failed', profiles: [] }
          : { kind: 'inUse', profiles: holders.map((held) => held.title) }
      refusal.value = nextRefusal
      mutating.value = false
      return { refusal: nextRefusal, status: 'failed' }
    }

    // Keep mutating=true through this read. In particular, a second click while
    // the GET is in flight cannot send the same POST/PATCH/DELETE again.
    try {
      await readCatalog()
      return { status: 'saved', value }
    } catch {
      // The mutation landed. Keep the last verified copy visible and make the
      // distinction explicit; refresh() below is GET-only recovery.
      stale.value = true
      state.value = catalog.value === null ? 'failed' : 'ready'
      return { status: 'stale', value }
    } finally {
      mutating.value = false
    }
  }

  /**
   * Re-read the catalog without repeating the preceding mutation. A retry that
   * races another retry is ignored, and all controls remain guarded until it
   * settles so two reads cannot reorder the visible catalog.
   */
  async function refresh(): Promise<boolean> {
    if (busy.value) return false
    refreshing.value = true
    try {
      await readCatalog()
      return true
    } catch {
      // Keep the old verified copy and its stale marker. The next retry remains
      // a GET and can recover without inviting the operator to resubmit a write.
      stale.value = catalog.value !== null
      state.value = catalog.value === null ? 'failed' : 'ready'
      return false
    } finally {
      refreshing.value = false
    }
  }

  // A created category has to be the one on screen, so its identity travels
  // back to the caller rather than being looked up again.
  function addCategory(
    title: string,
  ): Promise<LibraryWriteResult<CategoryDetail>> {
    return write(() => createCategory(title), categoryInUse, 'independent')
  }

  function renameCategory(
    categoryID: string,
    title: string,
  ): Promise<LibraryWriteResult<CategoryDetail>> {
    return write(
      () => updateCategory(categoryID, { title }),
      categoryInUse,
      'independent',
    )
  }

  // The server works the overlay out itself, so a membership edit states the
  // whole membership the operator wants rather than the one list that moved.
  function addList(
    category: CategoryDetail,
    listID: string,
  ): Promise<LibraryWriteResult<CategoryDetail>> {
    return write(
      () =>
        updateCategory(category.id, {
          lists: [...category.lists, listID],
        }),
      categoryInUse,
      'membership',
    )
  }

  function detachList(
    category: CategoryDetail,
    listID: string,
  ): Promise<LibraryWriteResult<CategoryDetail>> {
    return write(
      () =>
        updateCategory(category.id, {
          lists: category.lists.filter((id) => id !== listID),
        }),
      categoryInUse,
      'membership',
    )
  }

  function deleteCategory(
    categoryID: string,
    lists: CategoryLists,
  ): Promise<LibraryWriteResult> {
    return write(() => removeCategory(categoryID, lists), categoryInUse)
  }

  function deleteList(listID: string): Promise<LibraryWriteResult> {
    return write(() => removeList(listID), listInUse)
  }

  return {
    acceptUpdatedList,
    addCategory,
    addCustomList,
    addList,
    busy,
    categories,
    clearRefusal,
    defaultPriority,
    deleteCategory,
    deleteList,
    detachList,
    initialize,
    refresh,
    refusal,
    renameCategory,
    refreshing,
    lists,
    setDefaultPriority,
    stale,
    state,
  }
}
