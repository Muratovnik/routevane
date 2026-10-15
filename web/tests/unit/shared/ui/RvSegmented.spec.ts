/// <reference types="@vitest/browser-playwright" />
import { describe, expect, it } from 'vitest'
import { cdp } from 'vitest/browser'
import { render } from 'vitest-browser-vue'

import RvSegmented from '@/shared/ui/RvSegmented.vue'

import { contrast, inTheme, paintedToken } from './support/contrast'

const TRANSPARENT = 'rgba(0, 0, 0, 0)'

const OPTIONS = [
  { label: 'Automatic', value: 'auto' },
  { label: 'By hand', value: 'manual' },
]

const renderSegmented = (props: Record<string, unknown> = {}) =>
  render(RvSegmented, {
    props: {
      label: 'Delivery',
      modelValue: 'auto',
      name: 'delivery',
      options: OPTIONS,
      ...props,
    },
  })

describe('RvSegmented', () => {
  it('names its group by its caption and draws that caption', async () => {
    const screen = await renderSegmented()

    await expect
      .element(screen.getByRole('group', { name: 'Delivery' }))
      .toBeVisible()
    await expect.element(screen.getByText('Delivery')).toBeVisible()
  })

  // A caption the layout has no room for stays in the accessibility tree under
  // the product's one visually-hidden role: one pixel, clipped, out of flow.
  it('keeps a hidden caption for the group and out of the layout', async () => {
    const screen = await renderSegmented({ labelHidden: true })

    await expect
      .element(screen.getByRole('group', { name: 'Delivery' }))
      .toBeInTheDocument()
    const caption = screen.getByText('Delivery').element()
    const style = getComputedStyle(caption)
    expect(style.position).toBe('absolute')
    expect(style.clipPath).toBe('inset(50%)')
    expect(caption.getBoundingClientRect().width).toBe(1)
    expect(caption.getBoundingClientRect().height).toBe(1)
  })

  // The chosen option is told by its raised ground and edge alone, so there is
  // no mark, and choosing another changes no option's width and leaves every
  // option's words centred in its box, chosen or not.
  it('draws no mark, keeps every width and centres the words of each option', async () => {
    const screen = await renderSegmented()
    const option = (label: string) =>
      screen.getByRole('radio', { name: label }).element().closest('label')!
    const chosen = option('Automatic')
    const other = option('By hand')
    const widths = () =>
      [chosen, other].map((element) => element.getBoundingClientRect().width)
    const offCentre = (element: Element) => {
      const box = element.getBoundingClientRect()
      const words = element.querySelector('span')!.getBoundingClientRect()
      return Math.abs(words.left + words.width / 2 - (box.left + box.width / 2))
    }

    expect(screen.container.querySelector('svg')).toBeNull()
    const before = widths()
    expect(offCentre(chosen)).toBeLessThan(0.5)
    expect(offCentre(other)).toBeLessThan(0.5)

    await screen.rerender({ modelValue: 'manual' })
    expect(screen.container.querySelector('svg')).toBeNull()
    expect(widths()).toEqual(before)
    expect(offCentre(chosen)).toBeLessThan(0.5)
    expect(offCentre(other)).toBeLessThan(0.5)
  })

  // The chosen option stands raised out of the track on a neutral surface and
  // is never drawn in the accent: the ground and the hairline state the choice.
  // Its words stay readable on that ground, and so do the words of an option
  // that was not chosen, on the track.
  it.each(['dark', 'light'] as const)(
    'raises the chosen option on a neutral surface in the %s theme',
    async (theme) => {
      await inTheme(theme, async () => {
        const screen = await renderSegmented()
        const label = (name: string) =>
          screen.getByRole('radio', { name }).element().closest('label')!
        const chosen = label('Automatic')
        const other = label('By hand')
        const track = chosen.parentElement!
        const chosenStyle = getComputedStyle(chosen)
        const trackGround = getComputedStyle(track).backgroundColor

        expect(chosenStyle.backgroundColor).toBe(
          paintedToken('--rv-color-surface-raised'),
        )
        expect(chosenStyle.backgroundColor).not.toBe(trackGround)
        // An edge is drawn, it is neutral, it is not the accent the option
        // used to be outlined with, and it stands at 3:1 against the track it
        // sits in: with no mark left, the edge carries the state.
        expect(parseFloat(chosenStyle.borderTopWidth)).toBeGreaterThan(0)
        expect(chosenStyle.borderTopColor).toBe(
          paintedToken('--rv-color-ink-tertiary'),
        )
        expect(
          contrast(chosenStyle.borderTopColor, trackGround),
        ).toBeGreaterThanOrEqual(3)
        expect(chosenStyle.borderTopColor).not.toBe(
          paintedToken('--rv-color-accent'),
        )
        expect(getComputedStyle(other).backgroundColor).not.toBe(
          chosenStyle.backgroundColor,
        )
        expect(
          contrast(chosenStyle.color, chosenStyle.backgroundColor),
        ).toBeGreaterThanOrEqual(4.5)
        expect(
          contrast(getComputedStyle(other).color, trackGround),
        ).toBeGreaterThanOrEqual(4.5)
      })
    },
  )

  // A choice that cannot be changed now reads as unavailable beside the same
  // one when it can: flat, the words in the disabled ink, no lowered opacity.
  // The chosen option keeps an edge stronger than the faint rule, so the choice
  // does not rest on colour.
  it.each(['dark', 'light'] as const)(
    'draws a disabled choice apart from an enabled one in the %s theme',
    async (theme) => {
      await inTheme(theme, async () => {
        const screen = await renderSegmented({ name: 'open' })
        await renderSegmented({
          disabled: true,
          label: 'Frozen delivery',
          name: 'frozen',
        })
        const group = (caption: string): Element =>
          screen.getByRole('group', { exact: true, name: caption }).element()
        const optionBox = (caption: string, label: string): Element =>
          [...group(caption).querySelectorAll('label')].find((candidate) =>
            candidate.textContent?.includes(label),
          )!
        // The track is the options' own container.
        const track = (caption: string): HTMLElement =>
          optionBox(caption, 'Automatic').parentElement!
        expect(getComputedStyle(track('Frozen delivery')).backgroundColor).toBe(
          TRANSPARENT,
        )
        expect(getComputedStyle(track('Delivery')).backgroundColor).not.toBe(
          TRANSPARENT,
        )

        const chosenBox = optionBox('Frozen delivery', 'Automatic')
        const otherBox = optionBox('Frozen delivery', 'By hand')
        const chosen = getComputedStyle(chosenBox)
        const other = getComputedStyle(otherBox)
        const enabledChosen = getComputedStyle(
          optionBox('Delivery', 'Automatic'),
        )
        for (const style of [chosen, other]) {
          expect(style.color).toBe(paintedToken('--rv-color-ink-disabled'))
          expect(style.opacity).toBe('1')
          expect(style.cursor).toBe('not-allowed')
          expect(
            contrast(style.color, paintedToken('--rv-color-canvas')),
          ).toBeGreaterThanOrEqual(3)
        }
        expect(chosen.color).not.toBe(enabledChosen.color)
        expect(chosen.backgroundColor).toBe(TRANSPARENT)
        expect(enabledChosen.backgroundColor).not.toBe(TRANSPARENT)
        expect(chosen.borderTopColor).toBe(
          paintedToken('--rv-color-rule-strong'),
        )
        expect(chosen.borderTopColor).not.toBe(other.borderTopColor)
        expect(other.borderTopColor).toBe(TRANSPARENT)
        expect(chosen.boxShadow).toBe('none')
        expect(enabledChosen.boxShadow).not.toBe('none')
      })
    },
  )

  // Forced colours drop the grounds and the shadow that raise the chosen
  // option, so it must keep an edge in a system colour the others do not wear.
  it('outlines only the chosen option in forced-colors mode', async () => {
    const session = cdp()
    const emulate = (value: 'active' | 'none') =>
      session.send('Emulation.setEmulatedMedia', {
        features: [{ name: 'forced-colors', value }],
      })
    await emulate('active')
    try {
      expect(matchMedia('(forced-colors: active)').matches).toBe(true)
      const screen = await renderSegmented()
      const label = (name: string) =>
        getComputedStyle(
          screen.getByRole('radio', { name }).element().closest('label')!,
        )
      const chosen = label('Automatic')
      const other = label('By hand')
      const highlight = (() => {
        const probe = document.createElement('span')
        probe.style.color = 'Highlight'
        document.body.append(probe)
        const painted = getComputedStyle(probe).color
        probe.remove()
        return painted
      })()

      expect(chosen.borderTopColor).toBe(highlight)
      expect(chosen.outlineStyle).toBe('solid')
      expect(chosen.outlineColor).toBe(highlight)
      expect(parseFloat(chosen.outlineWidth)).toBeGreaterThan(0)
      expect(other.outlineStyle).toBe('none')
      expect(other.borderTopColor).not.toBe(highlight)
    } finally {
      await emulate('none')
    }
  })
})
