import { useLocalStorage, useMediaQuery } from '@vueuse/core'
import { computed } from 'vue'

/** Where the operator's own choice is kept: `true`, `false`, or nothing. */
const KEY = 'rv.sidebarCollapsed'

/**
 * Below this width the rail gives its labels up to the page. The width is in
 * rem rather than pixels, so enlarged text collapses it earlier, as a smaller
 * window would. 68.75rem is 1100px at the default size.
 */
export const NARROW_WINDOW = '(width < 68.75rem)'

/**
 * Whether the navigation rail shows icons only.
 *
 * The operator's own choice — the collapse control — always wins and is kept.
 * Only while nothing is kept does the window decide: a narrow window collapses
 * the rail and a wide one opens it. The choice is read back through a check
 * rather than trusted, because anything on this origin can write the key.
 */
export const useSidebarCollapse = () => {
  const chosen = useLocalStorage<boolean | null>(KEY, null, {
    flush: 'sync',
    onError: () => {
      // A choice that cannot be kept still holds for this session.
    },
    serializer: {
      read: (raw) => {
        if (raw === 'true') return true
        return raw === 'false' ? false : null
      },
      write: (value) => String(value),
    },
    writeDefaults: false,
  })
  const narrow = useMediaQuery(NARROW_WINDOW)

  const collapsed = computed(() => chosen.value ?? narrow.value)
  const toggle = (): void => {
    chosen.value = !collapsed.value
  }

  return { collapsed, toggle }
}
