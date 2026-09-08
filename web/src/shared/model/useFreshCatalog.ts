import { useEventListener } from '@vueuse/core'
import { onScopeDispose, ref, watch } from 'vue'

import {
  invalidateCatalogCache,
  loadCatalogCached,
  type Catalog,
} from '@/shared/api/catalog'

/** Refresh on return to the window; preserve a request owed during a mutation. */
export const useFreshCatalog = (
  apply: (catalog: Catalog) => void,
  busy: () => boolean,
  scope: () => string = () => '',
) => {
  const state = ref<'idle' | 'loading' | 'stale'>('idle')
  let reading = false
  let owed = false
  let disposed = false
  let generation = 0

  const drain = async (): Promise<void> => {
    if (disposed || reading || busy() || !owed) return
    owed = false
    reading = true
    state.value = 'loading'
    const request = generation
    const identity = scope()
    try {
      invalidateCatalogCache()
      const catalog = await loadCatalogCached()
      if (disposed || request !== generation || identity !== scope()) return
      if (busy()) {
        owed = true
        return
      }
      apply(catalog)
      state.value = 'idle'
    } catch {
      if (!disposed && request === generation && identity === scope())
        state.value = 'stale'
    } finally {
      reading = false
      if (owed) void drain()
    }
  }

  const retry = (): Promise<void> => {
    owed = true
    generation += 1
    return drain()
  }

  watch(busy, (working) => {
    if (!working) void drain()
  })
  watch(scope, () => {
    void retry()
  })
  useEventListener(window, 'focus', () => {
    void retry()
  })
  onScopeDispose(() => {
    disposed = true
    generation += 1
  })

  return { state, retry }
}
