import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  onTestFinished,
} from 'vitest'
import { page } from 'vitest/browser'
import { effectScope } from 'vue'

import { useSidebarCollapse } from '@/shared/model/useSidebarCollapse'

const KEY = 'rv.sidebarCollapsed'
const NARROW = { height: 640, width: 1099 }
const WIDE = { height: 640, width: 1100 }

// The composable is read inside a scope the way a component reads it, and the
// scope is stopped afterwards so its window listener does not outlive the case.
const open = () => {
  const scope = effectScope()
  onTestFinished(() => scope.stop())
  return scope.run(() => useSidebarCollapse())!
}

describe('useSidebarCollapse', () => {
  beforeEach(() => {
    window.localStorage.removeItem(KEY)
  })

  afterEach(async () => {
    window.localStorage.removeItem(KEY)
    await page.viewport(1024, 640)
  })

  // Nothing kept: the window decides, at 1100px and not a pixel before.
  it.each([
    ['one pixel under the threshold', NARROW, true],
    ['at the threshold', WIDE, false],
  ])(
    'collapses by the window when nothing is kept, %s',
    async (_, size, expected) => {
      await page.viewport(size.width, size.height)

      expect(open().collapsed.value).toBe(expected)
    },
  )

  it('follows the window as it is resized while nothing is kept', async () => {
    await page.viewport(WIDE.width, WIDE.height)
    const sidebar = open()
    expect(sidebar.collapsed.value).toBe(false)

    await page.viewport(NARROW.width, NARROW.height)
    await expect.poll(() => sidebar.collapsed.value).toBe(true)
    await page.viewport(WIDE.width, WIDE.height)
    await expect.poll(() => sidebar.collapsed.value).toBe(false)
  })

  // The operator's own choice wins over the window in both directions, and the
  // window changing sides afterwards does not take the choice back.
  it.each([
    ['opened in a narrow window', 'false', NARROW, WIDE, false],
    ['collapsed in a wide window', 'true', WIDE, NARROW, true],
  ])('keeps the kept choice, %s', async (_, kept, start, resized, expected) => {
    window.localStorage.setItem(KEY, kept)
    await page.viewport(start.width, start.height)
    const sidebar = open()
    expect(sidebar.collapsed.value).toBe(expected)

    await page.viewport(resized.width, resized.height)
    await expect.poll(() => window.innerWidth).toBe(resized.width)
    expect(sidebar.collapsed.value).toBe(expected)
  })

  it('keeps what the control chose, starting from the state the window gave', async () => {
    await page.viewport(NARROW.width, NARROW.height)
    const sidebar = open()
    expect(sidebar.collapsed.value).toBe(true)

    sidebar.toggle()

    expect(sidebar.collapsed.value).toBe(false)
    expect(window.localStorage.getItem(KEY)).toBe('false')
  })

  // The key can hold anything: only the two words the control writes count.
  it('treats a stored value that is not a choice as no choice', async () => {
    window.localStorage.setItem(KEY, 'maybe')
    await page.viewport(NARROW.width, NARROW.height)

    expect(open().collapsed.value).toBe(true)
  })
})
