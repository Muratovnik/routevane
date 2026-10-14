import { describe, expect, it } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'

import RvTextarea from '@/shared/ui/RvTextarea.vue'

import { contrast, inTheme, paintedToken } from './support/contrast'

const TRANSPARENT = 'rgba(0, 0, 0, 0)'

// The mono face as the browser resolves it, read off a probe rather than from
// the token's text, whose quoting the computed style rewrites.
const monoFace = (): string => {
  const probe = document.createElement('span')
  probe.style.fontFamily = 'var(--rv-font-mono)'
  document.body.append(probe)
  const face = getComputedStyle(probe).fontFamily
  probe.remove()
  return face
}

const renderEditor = (props: Record<string, unknown> = {}) =>
  render(RvTextarea, {
    attrs: { 'aria-label': 'Entries' },
    props: {
      inputId: 'entries',
      modelValue: '',
      rows: undefined as number | undefined,
      ...props,
    },
  })

describe('RvTextarea', () => {
  it('reports what is typed and shows what it is given', async () => {
    const screen = await renderEditor({ modelValue: 'example.com' })

    const editor = screen.getByRole('textbox', { name: 'Entries' })
    await expect.element(editor).toHaveValue('example.com')
    await expect.element(editor).toHaveAttribute('id', 'entries')
    await userEvent.fill(editor, 'example.org')
    expect(screen.emitted('update:modelValue')?.at(-1)).toEqual(['example.org'])
  })

  // The working area is stated in rows, so a caller that says nothing still
  // gets the editor's standing height rather than the library's three rows.
  it('stands nine rows high unless told otherwise', async () => {
    const screen = await renderEditor()
    const editor = screen.getByRole('textbox', { name: 'Entries' })
    await expect.element(editor).toHaveAttribute('rows', '9')

    await screen.rerender({ rows: 4 })
    await expect.element(editor).toHaveAttribute('rows', '4')
  })

  it('announces a refused value and refuses input while disabled', async () => {
    const screen = await renderEditor({ disabled: true, invalid: true })

    const editor = screen.getByRole('textbox', { name: 'Entries' })
    await expect.element(editor).toHaveAttribute('aria-invalid', 'true')
    await expect.element(editor).toBeDisabled()
    expect(editor.element().parentElement!.dataset.color).toBe('error')
  })

  // An editor that cannot be used now reads as unavailable beside the same one
  // when it can: flat, dimmer, drawn with the faint rule, and never with a
  // lowered opacity.
  it.each(['dark', 'light'] as const)(
    'draws a disabled editor apart from an enabled one in the %s theme',
    async (theme) => {
      await inTheme(theme, async () => {
        const screen = await render(RvTextarea, {
          attrs: { 'aria-label': 'Open' },
          props: { inputId: 'open', modelValue: 'typed', placeholder: 'Hint' },
        })
        await render(RvTextarea, {
          attrs: { 'aria-label': 'Closed' },
          props: {
            disabled: true,
            inputId: 'closed',
            modelValue: 'typed',
            placeholder: 'Hint',
          },
        })

        const on = screen.getByRole('textbox', { name: 'Open' }).element()
        const off = screen.getByRole('textbox', { name: 'Closed' }).element()
        const [open, closed] = [getComputedStyle(on), getComputedStyle(off)]
        expect(closed.color).not.toBe(open.color)
        expect(closed.color).toBe(paintedToken('--rv-color-ink-disabled'))
        expect(closed.backgroundColor).toBe(TRANSPARENT)
        expect(open.backgroundColor).not.toBe(TRANSPARENT)
        expect(closed.boxShadow).not.toBe(open.boxShadow)
        expect(closed.boxShadow).toContain(paintedToken('--rv-color-rule'))
        expect(closed.opacity).toBe('1')
        expect(closed.cursor).toBe('not-allowed')
        expect(
          contrast(closed.color, paintedToken('--rv-color-canvas')),
        ).toBeGreaterThanOrEqual(3)
        expect(getComputedStyle(off, '::placeholder').color).toBe(
          paintedToken('--rv-color-ink-disabled'),
        )
      })
    },
  )

  it('sets list contents in the mono role when asked to', async () => {
    const screen = await renderEditor({ mono: true })

    expect(
      getComputedStyle(
        screen.getByRole('textbox', { name: 'Entries' }).element(),
      ).fontFamily,
    ).toBe(monoFace())
  })
})
