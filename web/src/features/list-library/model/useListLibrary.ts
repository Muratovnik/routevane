import { computed, ref } from 'vue'

import {
  categoryInUse,
  createCategory,
  invalidateCatalogCache,
  loadCatalogCached,
  removeCategory,
  removeService,
  serviceInUse,
  updateCategory,
  type Catalog,
  type CategoryDetail,
  type CategoryLists,
  type ListReference,
  type ServiceDetail,
} from '@/shared/api/catalog'

export type LibraryState = 'loading' | 'ready' | 'failed'

/**
 * What a write had to say when it did not go through. A refusal that names the
 * routes standing in the way is the only one carrying more than a sentence.
 */
export type LibraryRefusal = {
  kind: 'failed' | 'inUse'
  routes: string[]
}

/**
 * The library of lists and the categories that hold them (ADR 0029).
 *
 * Every write here is the server's the moment it is made, so each is followed
 * by a re-read: this screen is the one place those objects are curated, and the
 * copy it shows has to be the copy it just wrote. The cached catalog is dropped
 * with it, so a composing screen returning to focus reads the same thing.
 */
export function useListLibrary() {
  const catalog = ref<Catalog | null>(null)
  const state = ref<LibraryState>('loading')
  const busy = ref(false)
  const refusal = ref<LibraryRefusal | null>(null)

  const services = computed<ServiceDetail[]>(
    () => catalog.value?.serviceDetails ?? [],
  )
  const categories = computed<CategoryDetail[]>(
    () => catalog.value?.categories ?? [],
  )

  async function initialize(): Promise<void> {
    state.value = 'loading'
    try {
      invalidateCatalogCache()
      catalog.value = await loadCatalogCached()
      state.value = 'ready'
    } catch {
      state.value = 'failed'
    }
  }

  function clearRefusal(): void {
    refusal.value = null
  }

  /**
   * One write, then the read that proves it. A refusal leaves the catalog
   * alone and states itself; a re-read that fails after a write that did not
   * keeps the copy on screen rather than reporting a change that happened as a
   * change that did not.
   */
  async function write(
    mutate: () => Promise<unknown>,
    refused: (reason: unknown) => ListReference[] | null,
  ): Promise<boolean> {
    if (busy.value) return false
    busy.value = true
    refusal.value = null
    try {
      await mutate()
    } catch (reason) {
      const holders = refused(reason)
      refusal.value =
        holders === null
          ? { kind: 'failed', routes: [] }
          : { kind: 'inUse', routes: holders.map((held) => held.title) }
      return false
    } finally {
      busy.value = false
    }
    await refresh()
    return true
  }

  async function refresh(): Promise<void> {
    try {
      invalidateCatalogCache()
      catalog.value = await loadCatalogCached()
    } catch {
      // The write landed; only the read did not. What is on screen is one edit
      // behind rather than wrong, and the next read corrects it.
    }
  }

  // A created category has to be the one on screen, so its identity travels
  // back to the caller rather than being looked up again.
  async function addCategory(title: string): Promise<string> {
    let created = ''
    const done = await write(async () => {
      created = (await createCategory(title)).id
    }, categoryInUse)
    return done ? created : ''
  }

  function renameCategory(categoryID: string, title: string): Promise<boolean> {
    return write(() => updateCategory(categoryID, { title }), categoryInUse)
  }

  // The server works the overlay out itself, so a membership edit states the
  // whole membership the operator wants rather than the one list that moved.
  function addList(
    category: CategoryDetail,
    serviceID: string,
  ): Promise<boolean> {
    return write(
      () =>
        updateCategory(category.id, {
          services: [...category.services, serviceID],
        }),
      categoryInUse,
    )
  }

  function detachList(
    category: CategoryDetail,
    serviceID: string,
  ): Promise<boolean> {
    return write(
      () =>
        updateCategory(category.id, {
          services: category.services.filter((id) => id !== serviceID),
        }),
      categoryInUse,
    )
  }

  function deleteCategory(
    categoryID: string,
    lists: CategoryLists,
  ): Promise<boolean> {
    return write(() => removeCategory(categoryID, lists), categoryInUse)
  }

  function deleteList(serviceID: string): Promise<boolean> {
    return write(() => removeService(serviceID), serviceInUse)
  }

  return {
    addCategory,
    addList,
    busy,
    categories,
    clearRefusal,
    deleteCategory,
    deleteList,
    detachList,
    initialize,
    refresh,
    refusal,
    renameCategory,
    services,
    state,
  }
}
