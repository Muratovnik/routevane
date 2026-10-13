import { nextTick } from 'vue'

/**
 * Runs a write and puts focus back where the operator left it.
 *
 * While a write is in flight the screen marks its controls unavailable, and a
 * browser moves focus to the document when the control that holds it becomes
 * unavailable. The write itself is the reason; the operator is still working in
 * the same place. Once the controls answer again, focus returns to the one that
 * held it, or to `fallback` when that control is gone or has nothing left to
 * offer — a Save with nothing left to save, a field that was replaced by the
 * state it produced. When nothing held focus to begin with, nothing is moved.
 */
export const keepFocus = async <T>(
  work: () => Promise<T>,
  fallback: () => HTMLElement | null | undefined,
): Promise<T> => {
  const held = document.activeElement
  const result = await work()
  if (held instanceof HTMLElement && held !== document.body) {
    await nextTick()
    const target = held.isConnected && !unavailable(held) ? held : fallback()
    target?.focus({ preventScroll: true })
  }
  return result
}

const unavailable = (element: HTMLElement): boolean =>
  element.matches(':disabled') ||
  element.getAttribute('aria-disabled') === 'true'
