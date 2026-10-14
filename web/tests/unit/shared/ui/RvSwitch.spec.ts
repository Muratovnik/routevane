import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import RvSwitch from '@/shared/ui/RvSwitch.vue'

import { contrast, inTheme, paintedToken } from './support/contrast'

const TRANSPARENT = 'rgba(0, 0, 0, 0)'

describe('RvSwitch', () => {
  it('is named by its label and reports a change', async () => {
    const screen = await render(RvSwitch, {
      props: { label: 'Offer Keenetic', modelValue: false },
    })

    const control = screen.getByRole('switch', { name: 'Offer Keenetic' })
    await expect.element(control).not.toBeChecked()
    await control.click()
    expect(screen.emitted('update:modelValue')).toEqual([[true]])
  })

  it('refuses a change while disabled', async () => {
    const screen = await render(RvSwitch, {
      props: { disabled: true, label: 'Offer Keenetic', modelValue: false },
    })

    const control = screen.getByRole('switch', { name: 'Offer Keenetic' })
    await expect.element(control).toBeDisabled()
    await control.click({ force: true })
    expect(screen.emitted('update:modelValue')).toBeUndefined()
  })

  // A switch that cannot be changed now reads as unavailable beside the same
  // switch when it can: flat, drawn with the faint rule and no lowered opacity,
  // and still saying whether it is on by where the thumb stands.
  it.each(['dark', 'light'] as const)(
    'draws a disabled switch apart from an enabled one in the %s theme',
    async (theme) => {
      await inTheme(theme, async () => {
        const screen = await render(RvSwitch, {
          props: { label: 'Open off', modelValue: false },
        })
        await render(RvSwitch, {
          props: { label: 'Open on', modelValue: true },
        })
        await render(RvSwitch, {
          props: { disabled: true, label: 'Closed off', modelValue: false },
        })
        await render(RvSwitch, {
          props: { disabled: true, label: 'Closed on', modelValue: true },
        })
        const control = (label: string): Element =>
          screen.getByRole('switch', { name: label }).element()
        const thumb = (label: string): CSSStyleDeclaration =>
          getComputedStyle(control(label).querySelector('span')!)
        const paint = (label: string) => getComputedStyle(control(label))

        for (const label of ['Closed off', 'Closed on']) {
          const style = paint(label)
          expect(style.opacity).toBe('1')
          expect(style.cursor).toBe('not-allowed')
          expect(style.borderTopColor).toBe(paintedToken('--rv-color-rule'))
          expect(thumb(label).backgroundColor).toBe(
            paintedToken('--rv-color-ink-disabled'),
          )
          expect(
            contrast(
              thumb(label).backgroundColor,
              paintedToken('--rv-color-canvas'),
            ),
          ).toBeGreaterThanOrEqual(3)
        }

        // Off: no ground, where the enabled one has one, and a dimmer thumb.
        expect(paint('Closed off').backgroundColor).toBe(TRANSPARENT)
        expect(paint('Open off').backgroundColor).not.toBe(TRANSPARENT)
        expect(thumb('Closed off').backgroundColor).not.toBe(
          thumb('Open off').backgroundColor,
        )
        // On: the accent at low emphasis, not the full accent, and not the
        // groundless look of the switch that is off.
        expect(paint('Closed on').backgroundColor).toBe(
          paintedToken('--rv-color-accent-quiet'),
        )
        expect(paint('Closed on').backgroundColor).not.toBe(
          paint('Open on').backgroundColor,
        )
        expect(paint('Closed on').backgroundColor).not.toBe(
          paint('Closed off').backgroundColor,
        )
        // Where the thumb stands still says which it is.
        expect(thumb('Closed on').transform).not.toBe(
          thumb('Closed off').transform,
        )
      })
    },
  )
})
