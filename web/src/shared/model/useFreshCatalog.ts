import { useEventListener } from '@vueuse/core'

import {
  invalidateCatalogCache,
  loadCatalogCached,
  type Catalog,
} from '@/shared/api/catalog'

/**
 * The catalog a composing screen holds, kept current while another tab curates
 * it.
 *
 * Composing a profile writes nothing global (ADR 0029), so «Списки» is a second
 * tab and its edits land behind this screen's back. Coming back to this window
 * is when the operator expects to see them, and it is the only moment worth
 * reading: a screen that polled would re-read a catalog nobody changed.
 *
 * The listener is bound to the component that asked for it, so an unmounted
 * screen reads nothing. A screen in the middle of a save reads nothing either —
 * replacing the catalog under a request in flight would judge its result
 * against material it never saw.
 */
export function useFreshCatalog(
  apply: (catalog: Catalog) => void,
  busy: () => boolean,
): void {
  let reading = false

  async function reread(): Promise<void> {
    if (reading || busy()) return
    reading = true
    try {
      invalidateCatalogCache()
      apply(await loadCatalogCached())
    } catch {
      // Nobody asked for this read, so a catalog that did not answer has
      // nothing to report: the copy already on screen stands.
    } finally {
      reading = false
    }
  }

  useEventListener(window, 'focus', () => {
    void reread()
  })
}
