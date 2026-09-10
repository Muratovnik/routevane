import { isModuleLoadError } from '@/shared/lib/moduleLoadError'
import {
  checkService,
  clearSectionFailure,
  onSectionRecovery,
  reportDevDisconnected,
  reportSectionFailure,
} from '@/shared/model/useServiceHealth'

/**
 * What happens when a section does not arrive.
 *
 * Opening a section fetches the module that draws it. When that fetch fails the
 * router cancels the navigation, and without this plugin the operator is left
 * on the previous screen with nothing said at all — the link simply does
 * nothing. There are two reasons for it and they need opposite answers:
 *
 * - the product was rebuilt under an open page, so the file the page asks for
 *   no longer exists. One reload lands on the new build and everything works.
 * - the service stopped answering. Reloading then replaces the interface with
 *   an empty window and takes the message with it, so nothing is reloaded and
 *   the shell says what happened instead.
 *
 * The two are told apart by asking the service, which is why every path here
 * checks before it decides. Nuxt's own chunk handling reacts to a
 * production-only browser event and always answers with a reload, which is why
 * `nuxt.config.ts` sets `emitRouteChunkError: 'manual'` and this owns it.
 */

const RELOAD_KEY = 'rv.sectionReload'

/**
 * How long one reload nobody asked for answers for. A rebuild is fixed by the
 * first reload; a second failure inside this window means the reload did not
 * fix it, and reloading again on its own would only replace the message with
 * another blank attempt. It is longer than the reload's own trip so the two
 * cannot disagree.
 *
 * The budget bounds reloads this code decides to take. It does not bound the
 * operator: a press is an instruction, not a symptom.
 */
const RELOAD_WINDOW_MILLISECONDS = 30_000

/** Whether the surface may reload itself again without being asked. */
const unaskedReloadAllowed = (): boolean => {
  try {
    const previous = Number(window.sessionStorage.getItem(RELOAD_KEY))
    return (
      !Number.isFinite(previous) ||
      Date.now() - previous >= RELOAD_WINDOW_MILLISECONDS
    )
  } catch {
    // A browser that refuses storage cannot be told a reload already happened,
    // so it is not offered one on its own: a message the operator can act on
    // beats a loop they cannot leave. The press below still works.
    return false
  }
}

/** Spends the budget, so the next unasked reload is bounded again. */
const noteReload = (): void => {
  try {
    window.sessionStorage.setItem(RELOAD_KEY, String(Date.now()))
  } catch {
    // Without a record every later reload counts as already spent, which is
    // the safe direction for the automatic path and no limit on the press.
  }
}

export default defineNuxtPlugin((nuxtApp) => {
  const router = useRouter()
  // One recovery at a time. A single failed module can be reported twice — the
  // preload event and the router error are the same event seen twice — and the
  // second report must not spend the one reload the first was allowed.
  let recovering = false

  const recover = async (path: string): Promise<void> => {
    if (recovering) return
    recovering = true
    try {
      const alive = await checkService()
      if (alive && unaskedReloadAllowed()) {
        noteReload()
        reloadNuxtApp({ path, persistState: true })
        return
      }
      // Either the service is not answering — in which case the shell already
      // has the larger message and a reload would erase it — or this page has
      // reloaded for the same reason moments ago and it did not help. The
      // message carries the press that is not bound by that budget.
      reportSectionFailure(path)
    } finally {
      recovering = false
    }
  }

  /**
   * The operator pressed retry on the message about that section.
   *
   * A browser remembers that a module URL failed for as long as the document
   * lives, so asking the router for the same section again in this document
   * can only fail again: a reload is the single thing that opens it. Refusing
   * the press because this page reloaded a moment ago would leave an action
   * that provably changes nothing, so the budget above does not apply to it —
   * only the service does, because reloading from a service that is not
   * answering trades the message for an empty window.
   */
  const recoverOnRequest = async (path: string): Promise<void> => {
    if (!(await checkService())) return
    noteReload()
    // Nuxt keeps a record of its own, ten seconds per path, and silently does
    // nothing inside it. That record is what would swallow a press that
    // follows an automatic reload, so this one is stated as deliberate.
    reloadNuxtApp({ force: true, path, persistState: true })
  }

  onSectionRecovery(recoverOnRequest)

  // The one handler that fires in development and in the built product alike.
  router.onError((error, to) => {
    if (!isModuleLoadError(error)) return
    void recover(to.fullPath)
  })

  // A section that arrives answers the message about one that did not.
  router.afterEach(() => {
    clearSectionFailure()
  })

  // The same failure away from a navigation: a module a screen loads for
  // itself. In the built product the bundler announces it as `vite:preloadError`
  // and Nuxt re-states it as this hook, which is what `manual` leaves for us.
  nuxtApp.hook('app:chunkError', ({ error }) => {
    if (!isModuleLoadError(error)) return
    void recover(router.currentRoute.value.fullPath)
  })

  // And in development, where no such event exists, the rejection nobody caught.
  window.addEventListener('unhandledrejection', (event) => {
    if (!isModuleLoadError(event.reason)) return
    void recover(router.currentRoute.value.fullPath)
  })

  if (import.meta.dev) {
    // The development server's own socket. Its client reports the loss to the
    // console alone, and while it is down no section that was never compiled
    // can arrive, so the shell says so rather than failing silently. This block
    // is compiled out of the built product.
    import.meta.hot?.on('vite:ws:disconnect', () => {
      reportDevDisconnected(true)
    })
    import.meta.hot?.on('vite:ws:connect', () => {
      reportDevDisconnected(false)
    })
  }
})
