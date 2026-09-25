import { describe, expect, it } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'

import RvTextarea from '@/shared/ui/RvTextarea.vue'

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

  it('sets list contents in the mono role when asked to', async () => {
    const screen = await renderEditor({ mono: true })

    expect(
      getComputedStyle(
        screen.getByRole('textbox', { name: 'Entries' }).element(),
      ).fontFamily,
    ).toBe(monoFace())
  })
})
