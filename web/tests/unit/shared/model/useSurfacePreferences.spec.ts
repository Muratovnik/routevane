import { describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

type SchemeListener = (event: { matches: boolean }) => void

const RETIRED_KEY = 'rv.lastBuild'

/**
 * The operating system's appearance, driveable. The real one cannot be changed
 * from a test, and the behaviour under test is precisely what happens when it
 * changes while the page is open. The double is installed here rather than in a
 * case, because the module under test reads the media query once — while the
 * page evaluates it.
 */
const SYSTEM = { dark: false }
const schemeListeners = new Set<SchemeListener>()

vi.stubGlobal('matchMedia', (query: string) => ({
  addEventListener: (_: string, listener: SchemeListener) => {
    schemeListeners.add(listener)
  },
  matches: query.includes('dark') ? SYSTEM.dark : false,
  media: query,
  removeEventListener: (_: string, listener: SchemeListener) => {
    schemeListeners.delete(listener)
  },
}))

const setSystemDark = async (dark: boolean): Promise<void> => {
  SYSTEM.dark = dark
  for (const listener of schemeListeners) listener({ matches: dark })
  await nextTick()
}

// Another tab on this origin writing the same key. The browser delivers that as
// a storage event; a write from this page never fires one, so this is how a
// value that arrived from outside is put in front of the module.
const storeInAnotherTab = (key: string, value: string): void => {
  window.localStorage.setItem(key, value)
  window.dispatchEvent(
    new StorageEvent('storage', {
      key,
      newValue: value,
      storageArea: window.localStorage,
    }),
  )
}

const theme = (): string | null =>
  document.documentElement.getAttribute('data-rv-theme')

// These preferences are module scope so the shell and the screens read one
// value, which means one instance per page and one sweep of the retired keys.
// Seed before the import and read after it: both happen while the module is
// evaluated, and no case below can observe them again.
window.localStorage.setItem(RETIRED_KEY, '{"id":"stale"}')
const { useSurfacePreferences } =
  await import('@/shared/model/useSurfacePreferences')
const RETIRED_AFTER_LOAD = window.localStorage.getItem(RETIRED_KEY)

describe('surface preferences', () => {
  // Local storage holds display preferences only, so an orphaned record of
  // product state is removed rather than kept in the browser forever.
  it('forgets a key nothing reads anymore', () => {
    expect(RETIRED_AFTER_LOAD).toBeNull()
  })

  // The keys and the words in them are a contract with every browser that has
  // already stored them: a rename is a preference silently lost.
  it('reads back the words an earlier build stored, and refuses what is not a preference', async () => {
    const preferences = useSurfacePreferences()

    storeInAnotherTab('rv.mode', 'expert')
    storeInAnotherTab('rv.appearance', 'light')
    storeInAnotherTab('rv.hiddenTargets', '["keenetic","sing-box"]')
    await nextTick()

    expect(preferences.mode.value).toBe('expert')
    expect(preferences.expert.value).toBe(true)
    expect(preferences.appearance.value).toBe('light')
    expect(preferences.hiddenTargets.value).toEqual(['keenetic', 'sing-box'])
    expect(theme()).toBe('light')

    // Storage is shared with everything else on this origin, so what comes out
    // of it is checked rather than believed.
    storeInAnotherTab('rv.mode', 'wizard')
    storeInAnotherTab('rv.appearance', 'midnight')
    storeInAnotherTab('rv.hiddenTargets', '{"keenetic":true}')
    await nextTick()

    expect(preferences.mode.value).toBe('simple')
    expect(preferences.appearance.value).toBe('system')
    expect(preferences.hiddenTargets.value).toEqual([])
  })

  it('writes a choice back in the same words', async () => {
    const preferences = useSurfacePreferences()

    preferences.setAppearance('dark')
    preferences.setMode('expert')
    preferences.setTargetHidden('keenetic', true)
    await nextTick()

    expect(window.localStorage.getItem('rv.appearance')).toBe('dark')
    expect(window.localStorage.getItem('rv.mode')).toBe('expert')
    expect(window.localStorage.getItem('rv.hiddenTargets')).toBe('["keenetic"]')
    expect(theme()).toBe('dark')
  })

  // "System" is a standing instruction, not a reading taken once: an evening
  // that turns the desk dark turns the page dark with it.
  it('follows the operating system while the system ground is chosen', async () => {
    const preferences = useSurfacePreferences()

    preferences.setAppearance('system')
    await setSystemDark(false)
    expect(preferences.appearance.value).toBe('system')
    expect(theme()).toBe('light')

    await setSystemDark(true)
    expect(theme()).toBe('dark')

    await setSystemDark(false)
    expect(theme()).toBe('light')

    // An explicit choice stops following, and the stored word stays ours.
    preferences.setAppearance('dark')
    await nextTick()
    await setSystemDark(false)
    expect(theme()).toBe('dark')
    expect(window.localStorage.getItem('rv.appearance')).toBe('dark')
  })
})
