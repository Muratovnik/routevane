import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

type SchemeListener = (event: { matches: boolean }) => void

/**
 * The operating system's appearance, driveable. The real one cannot be changed
 * from a test, and the behaviour under test is precisely what happens when it
 * changes while the page is open.
 */
const stubColorScheme = (dark: boolean) => {
  const listeners = new Set<SchemeListener>()
  let matches = dark
  vi.stubGlobal('matchMedia', (query: string) => ({
    addEventListener: (_: string, listener: SchemeListener) => {
      listeners.add(listener)
    },
    matches: query.includes('dark') ? matches : false,
    media: query,
    removeEventListener: (_: string, listener: SchemeListener) => {
      listeners.delete(listener)
    },
  }))
  return async (next: boolean): Promise<void> => {
    matches = next
    for (const listener of listeners) listener({ matches: next })
    await nextTick()
  }
}

const loadPreferences = async () =>
  (await import('@/shared/model/useSurfacePreferences')).useSurfacePreferences

const theme = (): string | null =>
  document.documentElement.getAttribute('data-rv-theme')

describe('surface preferences', () => {
  beforeEach(() => {
    vi.resetModules()
    window.localStorage.clear()
    document.documentElement.removeAttribute('data-rv-theme')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // The keys and the words in them are a contract with every browser that has
  // already stored them: a rename is a preference silently lost.
  it('reads back the words an earlier build stored', async () => {
    stubColorScheme(true)
    window.localStorage.setItem('rv.mode', 'expert')
    window.localStorage.setItem('rv.appearance', 'light')
    window.localStorage.setItem('rv.hiddenTargets', '["keenetic","sing-box"]')

    const preferences = (await loadPreferences())()

    expect(preferences.mode.value).toBe('expert')
    expect(preferences.expert.value).toBe(true)
    expect(preferences.appearance.value).toBe('light')
    expect(preferences.hiddenTargets.value).toEqual(['keenetic', 'sing-box'])
    expect(theme()).toBe('light')
  })

  it('writes a choice back in the same words', async () => {
    stubColorScheme(false)
    const preferences = (await loadPreferences())()

    preferences.setAppearance('dark')
    preferences.setMode('expert')
    preferences.setTargetHidden('keenetic', true)
    await nextTick()

    expect(window.localStorage.getItem('rv.appearance')).toBe('dark')
    expect(window.localStorage.getItem('rv.mode')).toBe('expert')
    expect(window.localStorage.getItem('rv.hiddenTargets')).toBe('["keenetic"]')
    expect(theme()).toBe('dark')
  })

  // Storage is shared with everything else on this origin, so what comes out of
  // it is checked rather than believed.
  it('ignores a stored value that is not a preference', async () => {
    stubColorScheme(false)
    window.localStorage.setItem('rv.mode', 'wizard')
    window.localStorage.setItem('rv.appearance', 'midnight')
    window.localStorage.setItem('rv.hiddenTargets', '{"keenetic":true}')

    const preferences = (await loadPreferences())()

    expect(preferences.mode.value).toBe('simple')
    expect(preferences.appearance.value).toBe('system')
    expect(preferences.hiddenTargets.value).toEqual([])
  })

  // "System" is a standing instruction, not a reading taken once: an evening
  // that turns the desk dark turns the page dark with it.
  it('follows the operating system while the system ground is chosen', async () => {
    const setDark = stubColorScheme(false)
    const preferences = (await loadPreferences())()
    expect(preferences.appearance.value).toBe('system')
    expect(theme()).toBe('light')

    await setDark(true)
    expect(theme()).toBe('dark')

    await setDark(false)
    expect(theme()).toBe('light')

    // An explicit choice stops following, and the stored word stays ours.
    preferences.setAppearance('dark')
    await nextTick()
    await setDark(false)
    expect(theme()).toBe('dark')
    expect(window.localStorage.getItem('rv.appearance')).toBe('dark')
  })

  it('forgets a key nothing reads anymore', async () => {
    stubColorScheme(false)
    window.localStorage.setItem('rv.lastBuild', '{"id":"stale"}')

    await loadPreferences()

    expect(window.localStorage.getItem('rv.lastBuild')).toBeNull()
  })
})
