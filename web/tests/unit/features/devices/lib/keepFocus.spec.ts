import { nextTick } from 'vue'
import { describe, expect, it, onTestFinished } from 'vitest'

import { keepFocus } from '@/features/devices/lib/keepFocus'

// Two controls in a document of their own: the one a write is started from,
// and the field focus falls back to.
const scenario = () => {
  const action = document.createElement('button')
  const fallback = document.createElement('input')
  document.body.append(action, fallback)
  onTestFinished(() => {
    action.remove()
    fallback.remove()
  })
  return { action, fallback }
}

describe('keepFocus', () => {
  it('returns focus to the control that held it when it can be used again', async () => {
    const { action, fallback } = scenario()
    action.focus()

    const result = await keepFocus(
      async () => {
        // A browser drops focus to the document while its holder is disabled.
        action.disabled = true
        await nextTick()
        action.disabled = false
        return 'done'
      },
      () => fallback,
    )

    expect(result).toBe('done')
    expect(document.activeElement).toBe(action)
  })

  it('moves focus to the fallback when the holder has nothing left to offer', async () => {
    const { action, fallback } = scenario()
    action.focus()

    await keepFocus(
      async () => {
        action.disabled = true
      },
      () => fallback,
    )

    expect(document.activeElement).toBe(fallback)
  })

  it('moves focus to the fallback when the holder is gone', async () => {
    const { action, fallback } = scenario()
    action.focus()

    await keepFocus(
      async () => {
        action.remove()
      },
      () => fallback,
    )

    expect(document.activeElement).toBe(fallback)
  })

  it('moves nothing when nothing held focus', async () => {
    const { fallback } = scenario()
    expect(document.activeElement).toBe(document.body)

    await keepFocus(
      async () => 'done',
      () => fallback,
    )

    expect(document.activeElement).toBe(document.body)
  })
})
