import { describe, expect, it } from 'vitest'
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

  // The chosen option has to be told from the others by more than a shade:
  // a mark that holds its place in every option and is drawn only on the
  // chosen one.
  it('marks the chosen option with a shape, and the mark holds its place', async () => {
    const screen = await renderSegmented()
    const option = (label: string) =>
      screen.getByRole('radio', { name: label }).element().closest('label')!
    const chosen = option('Automatic')
    const other = option('By hand')

    await expect
      .element(screen.getByTestId('rv-segmented-mark-auto'))
      .toBeVisible()
    await expect
      .element(screen.getByTestId('rv-segmented-mark-manual'))
      .not.toBeVisible()
    // Choosing another option moves the mark and not the options' widths.
    const widths = [chosen, other].map(
      (element) => element.getBoundingClientRect().width,
    )
    await screen.rerender({ modelValue: 'manual' })
    await expect
      .element(screen.getByTestId('rv-segmented-mark-manual'))
      .toBeVisible()
    await expect
      .element(screen.getByTestId('rv-segmented-mark-auto'))
      .not.toBeVisible()
    expect(
      [chosen, other].map((element) => element.getBoundingClientRect().width),
    ).toEqual(widths)
  })

  // The chosen option stands raised out of the track on a neutral surface and
  // is never drawn in the accent: the check mark states the choice, and the
  // ground and hairline only lift it. Its words stay readable on that ground,
  // and so do the words of an option that was not chosen, on the track.
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
        // An edge is drawn, it is the neutral overlay hairline, and it is not
        // the accent the option used to be outlined with.
        expect(parseFloat(chosenStyle.borderTopWidth)).toBeGreaterThan(0)
        expect(chosenStyle.borderTopColor).toBe(
          paintedToken('--rv-color-rule-raised'),
        )
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
        await expect
          .element(screen.getByTestId('rv-segmented-mark-auto'))
          .toBeVisible()
      })
    },
  )

  // A choice that cannot be changed now reads as unavailable beside the same
  // one when it can: flat, the words in the disabled ink and the faint rule for
  // every edge, no lowered opacity. The chosen option still carries its mark,
  // so the choice does not rest on colour.
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
        // The track is the options' own container; the marks are aria-hidden
        // and are read by their test hook, the first of each being the enabled
        // choice's and the second the disabled one's.
        const track = (caption: string): HTMLElement =>
          optionBox(caption, 'Automatic').parentElement!
        const marks = (value: string): Element[] =>
          screen.getByTestId(`rv-segmented-mark-${value}`).elements()
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
        expect(chosen.borderTopColor).toBe(paintedToken('--rv-color-rule'))
        expect(chosen.boxShadow).toBe('none')
        expect(enabledChosen.boxShadow).not.toBe('none')
        // The chosen option is still told from the other by its mark.
        const chosenMark = getComputedStyle(marks('auto')[1]!)
        expect(chosenMark.visibility).toBe('visible')
        expect(chosenMark.color).toBe(paintedToken('--rv-color-ink-disabled'))
        expect(getComputedStyle(marks('manual')[1]!).visibility).toBe('hidden')
      })
    },
  )
})
