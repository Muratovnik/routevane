import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import RvFilePicker from '@/shared/ui/RvFilePicker.vue'

import { contrast, inTheme, paintedToken } from './support/contrast'

const TRANSPARENT = 'rgba(0, 0, 0, 0)'

const PROPS = {
  actionLabel: 'Choose file',
  emptyLabel: 'No file selected',
  inputId: 'transfer-file',
  label: 'Configuration file',
}

describe('RvFilePicker', () => {
  it('keeps a labelled native chooser behind the custom surface', async () => {
    const screen = await render(RvFilePicker, {
      props: PROPS,
    })

    // The drawn surface is a button; the file input is what the visible label
    // names, so reaching the native chooser through that label is the check
    // that the two are still wired to each other.
    const chooser = screen.getByLabelText('Configuration file')
    await expect.element(chooser).toHaveAttribute('type', 'file')
    await expect.element(screen.getByText('No file selected')).toBeVisible()

    const file = new File(['{}'], 'routevane.json', {
      type: 'application/json',
    })
    await chooser.upload(file)

    const selected = screen.emitted<[File]>('select')?.at(-1)
    expect(selected?.[0]?.name).toBe('routevane.json')
  })

  it('does not open or accept a file while disabled', async () => {
    const screen = await render(RvFilePicker, {
      props: { ...PROPS, disabled: true },
    })

    await expect
      .element(screen.getByRole('button', { name: 'Choose file' }))
      .toBeDisabled()
    await expect
      .element(screen.getByLabelText('Configuration file'))
      .toBeDisabled()
    expect(screen.emitted('select')).toBeUndefined()
  })

  // A surface that cannot take a file now reads as unavailable beside the same
  // one when it can: flat, every word and glyph in the disabled ink, the faint
  // rule for its edge, and no lowered opacity.
  it.each(['dark', 'light'] as const)(
    'draws a disabled surface apart from an enabled one in the %s theme',
    async (theme) => {
      await inTheme(theme, async () => {
        const hint = { hint: 'JSON, up to 64 MiB' }
        const screen = await render(RvFilePicker, {
          props: { ...PROPS, ...hint, actionLabel: 'Choose', inputId: 'on' },
        })
        await render(RvFilePicker, {
          props: {
            ...PROPS,
            ...hint,
            actionLabel: 'Unavailable',
            disabled: true,
            inputId: 'off',
          },
        })

        const on = screen.getByRole('button', { name: 'Choose' }).element()
        const off = screen
          .getByRole('button', { name: 'Unavailable' })
          .element()
        const [open, closed] = [getComputedStyle(on), getComputedStyle(off)]
        expect(closed.color).not.toBe(open.color)
        expect(closed.color).toBe(paintedToken('--rv-color-ink-disabled'))
        expect(closed.backgroundColor).toBe(TRANSPARENT)
        expect(open.backgroundColor).not.toBe(TRANSPARENT)
        expect(closed.borderTopColor).toBe(paintedToken('--rv-color-rule'))
        // Still the dashed target a file can be dropped on once it is available.
        expect(closed.borderTopStyle).toBe('dashed')
        expect(closed.opacity).toBe('1')
        expect(closed.cursor).toBe('not-allowed')
        for (const part of [
          '.rv-file-picker__icon',
          'strong',
          'small',
          '.rv-file-picker__action',
        ])
          expect(getComputedStyle(off.querySelector(part)!).color).toBe(
            paintedToken('--rv-color-ink-disabled'),
          )
        expect(
          contrast(closed.color, paintedToken('--rv-color-canvas')),
        ).toBeGreaterThanOrEqual(3)
      })
    },
  )
})
