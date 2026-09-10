import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  clearSectionFailure,
  reportReachable,
  retrySection,
  useServiceHealth,
} from '@/shared/model/useServiceHealth'

/**
 * What the application does about a section that did not arrive.
 *
 * Three of its inputs are Nuxt's — the router, the plugin hook bus, and the one
 * call that replaces the document — and the fourth is the service. All four are
 * handed over here, so every decision it makes can be read without a browser
 * reloading anything and without a service to stop.
 */

const RELOAD_KEY = 'rv.sectionReload'
const RELOAD_WINDOW_MILLISECONDS = 30_000
const SECTION = '/lists'
const CURRENT = '/'

/** What a browser says when the module a section is drawn from never arrived. */
const moduleGone = (): TypeError =>
  new TypeError('Failed to fetch dynamically imported module: /_nuxt/x.js')

type ReloadRequest = { force?: boolean; path?: string; persistState?: boolean }
type NavigationError = (error: unknown, to: { fullPath: string }) => void
type ChunkError = (payload: { error: unknown }) => void

const stubService = (respond: (path: string) => Promise<Response>) => {
  const service = vi.fn(respond)
  vi.stubGlobal('fetch', service)
  return service
}

/**
 * Runs the plugin against stand-ins for everything Nuxt supplies it, and hands
 * back the two ways a failure reaches it plus the record of what it asked to
 * reload.
 */
const runPlugin = async (): Promise<{
  reloads: ReloadRequest[]
  failNavigation: (error: unknown) => void
  failChunk: (error: unknown) => void
}> => {
  const reloads: ReloadRequest[] = []
  const navigationErrors: NavigationError[] = []
  const chunkErrors: ChunkError[] = []
  vi.stubGlobal('defineNuxtPlugin', (setup: unknown) => setup)
  vi.stubGlobal('reloadNuxtApp', (options: ReloadRequest) => {
    reloads.push(options)
  })
  vi.stubGlobal('useRouter', () => ({
    afterEach: () => undefined,
    currentRoute: { value: { fullPath: CURRENT } },
    onError: (handler: NavigationError) => navigationErrors.push(handler),
  }))
  // The plugin's default export is whatever `defineNuxtPlugin` was handed,
  // which the stub above returns unchanged: the setup Nuxt would call with the
  // application instance.
  const plugin = await import('@/plugins/navigation.client')
  const setup = plugin.default as unknown as (app: {
    hook: (name: string, handler: ChunkError) => void
  }) => void
  setup({ hook: (_name, handler) => chunkErrors.push(handler) })
  return {
    failChunk: (error) => {
      for (const handler of chunkErrors) handler({ error })
    },
    failNavigation: (error) => {
      for (const handler of navigationErrors)
        handler(error, { fullPath: SECTION })
    },
    reloads,
  }
}

/** A reload spent moments ago, which is what bounds the next unasked one. */
const spendTheBudget = (): void => {
  window.sessionStorage.setItem(RELOAD_KEY, String(Date.now()))
}

const spentRecently = (): boolean =>
  Date.now() - Number(window.sessionStorage.getItem(RELOAD_KEY)) <
  RELOAD_WINDOW_MILLISECONDS

afterEach(() => {
  reportReachable()
  clearSectionFailure()
  window.sessionStorage.removeItem(RELOAD_KEY)
  vi.unstubAllGlobals()
})

describe('a section that did not arrive', () => {
  it('reloads once at the section address while the service answers', async () => {
    stubService(async () => new Response('{}'))
    const { sectionFailure } = useServiceHealth()
    const app = await runPlugin()

    app.failNavigation(moduleGone())

    await vi.waitFor(() => expect(app.reloads).toHaveLength(1))
    expect(app.reloads[0]).toEqual({ path: SECTION, persistState: true })
    expect(sectionFailure.value).toBe('')
    // The budget is spent with it, which is what bounds the next one.
    expect(spentRecently()).toBe(true)
  })

  it('states the failure instead of reloading a second time on its own', async () => {
    stubService(async () => new Response('{}'))
    spendTheBudget()
    const { sectionFailure } = useServiceHealth()
    const app = await runPlugin()

    app.failNavigation(moduleGone())

    await vi.waitFor(() => expect(sectionFailure.value).toBe(SECTION))
    expect(app.reloads).toEqual([])
  })

  /**
   * A browser remembers a module URL that failed for as long as the document
   * lives, so asking for that section again in this document can only fail
   * again: a reload is the one thing that opens it. A press the budget refused
   * would therefore be an action that provably changes nothing, which is why
   * the budget bounds what this code decides and not what it is told.
   */
  it('spends a reload on the press even inside the window that refused one', async () => {
    stubService(async () => new Response('{}'))
    spendTheBudget()
    const { sectionFailure } = useServiceHealth()
    const app = await runPlugin()
    app.failNavigation(moduleGone())
    await vi.waitFor(() => expect(sectionFailure.value).toBe(SECTION))

    await retrySection()

    expect(app.reloads).toHaveLength(1)
    // Forced, because Nuxt keeps a ten-second record of its own per address
    // that would otherwise swallow a press following an automatic reload.
    expect(app.reloads[0]).toEqual({
      force: true,
      path: SECTION,
      persistState: true,
    })
    // The budget moved with it, so the next unasked reload is bounded again.
    expect(spentRecently()).toBe(true)
  })

  it('reloads nothing while the service is not answering, and says so instead', async () => {
    stubService(() => Promise.reject(new TypeError('offline')))
    const { reach, sectionFailure } = useServiceHealth()
    const app = await runPlugin()

    app.failNavigation(moduleGone())

    await vi.waitFor(() => expect(reach.value).toBe('unreachable'))
    expect(app.reloads).toEqual([])
    expect(sectionFailure.value).toBe(SECTION)

    // The press answers to the service too: reloading from one that is not
    // answering trades the message for an empty window.
    await retrySection()

    expect(app.reloads).toEqual([])
  })

  it('carries a module that failed away from a navigation to the same recovery', async () => {
    stubService(async () => new Response('{}'))
    const app = await runPlugin()

    app.failChunk(moduleGone())

    await vi.waitFor(() => expect(app.reloads).toHaveLength(1))
    expect(app.reloads[0]).toEqual({ path: CURRENT, persistState: true })
  })

  it('leaves a failure that is not a missing module alone', async () => {
    const service = stubService(async () => new Response('{}'))
    const { sectionFailure } = useServiceHealth()
    const app = await runPlugin()

    app.failNavigation(new Error('Navigation aborted from / to /lists'))
    app.failChunk(new TypeError('Failed to fetch'))
    await Promise.resolve()

    expect(service).not.toHaveBeenCalled()
    expect(app.reloads).toEqual([])
    expect(sectionFailure.value).toBe('')
  })
})
