import { useColorMode, useLocalStorage } from '@vueuse/core'
import { computed } from 'vue'

/**
 * Two preferences the operator owns: how much detail the surface shows, and
 * which ground it uses. Both are module scope so the shell and the screens read
 * one value, both survive a refusal to store them, and both are read back
 * through a check rather than trusted, because anything on this origin can
 * write to the same keys.
 */

export type DetailMode = 'simple' | 'expert'
export type Appearance = 'dark' | 'light' | 'system'

const modeKey = 'rv.mode'
const appearanceKey = 'rv.appearance'
const hiddenTargetsKey = 'rv.hiddenTargets'

// Keys an earlier version stored and nothing reads anymore. Local storage
// holds display preferences only, so an orphaned record of product state is
// removed on the first visit rather than kept in the browser forever.
const retiredKeys = ['rv.lastBuild']

const mode = useLocalStorage<DetailMode>(modeKey, 'simple', {
  flush: 'sync',
  onError: noteStorageRefusal,
  serializer: {
    read: (raw) => (raw === 'expert' || raw === 'simple' ? raw : 'simple'),
    write: (value) => value,
  },
  writeDefaults: false,
})

const appearance = useLocalStorage<Appearance>(appearanceKey, 'system', {
  flush: 'sync',
  onError: noteStorageRefusal,
  serializer: {
    read: (raw) =>
      raw === 'dark' || raw === 'light' || raw === 'system' ? raw : 'system',
    write: (value) => value,
  },
  writeDefaults: false,
})

// Target ids the operator asked the builder not to offer. A hidden target is a
// display preference of this browser, never a server fact: its lists, files
// and deployments stay exactly as they are.
const hiddenTargets = useLocalStorage<string[]>(hiddenTargetsKey, [], {
  flush: 'sync',
  onError: noteStorageRefusal,
  serializer: {
    read: readHiddenTargets,
    write: (value) => JSON.stringify(value),
  },
  writeDefaults: false,
})

/**
 * The ground is applied by writing the resolved theme onto the document, and
 * the stored word for "follow the system" is ours: `system` is what a browser
 * already holds and what settings already offers, `auto` is what the color-mode
 * helper understands. This bridge translates between the two vocabularies so a
 * value stored by an earlier build keeps both its meaning and its spelling.
 *
 * Following is live: with `system` chosen, changing the operating system's
 * appearance changes the page's without a reload.
 */
const colorModeChoice = computed<'auto' | 'dark' | 'light'>({
  get: () => (appearance.value === 'system' ? 'auto' : appearance.value),
  set: (next) => {
    appearance.value = next === 'auto' ? 'system' : next
  },
})

useColorMode({
  attribute: 'data-rv-theme',
  // The themes carry no colour transitions, so there is nothing to suppress and
  // no stylesheet to inject on every switch.
  disableTransition: false,
  selector: 'html',
  storageRef: colorModeChoice,
})

if (typeof window !== 'undefined') {
  for (const key of retiredKeys) forget(key)
}

export function useSurfacePreferences() {
  function setMode(next: DetailMode): void {
    mode.value = next
  }

  function setAppearance(next: Appearance): void {
    appearance.value = next
  }

  function setTargetHidden(id: string, hidden: boolean): void {
    const without = hiddenTargets.value.filter((entry) => entry !== id)
    hiddenTargets.value = hidden ? [...without, id].sort() : without
  }

  return {
    appearance: computed(() => appearance.value),
    expert: computed(() => mode.value === 'expert'),
    hiddenTargets: computed(() => hiddenTargets.value),
    mode: computed(() => mode.value),
    setAppearance,
    setMode,
    setTargetHidden,
  }
}

function readHiddenTargets(raw: string): string[] {
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((entry): entry is string => typeof entry === 'string')
  } catch {
    return []
  }
}

function forget(key: string): void {
  try {
    window.localStorage.removeItem(key)
  } catch {
    // Cleanup is best-effort: a browser that refuses it loses nothing.
  }
}

function noteStorageRefusal(): void {
  // A preference that cannot be remembered still applies to this session.
}
