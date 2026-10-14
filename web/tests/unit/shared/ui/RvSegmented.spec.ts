import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import RvSegmented from '@/shared/ui/RvSegmented.vue'

import { contrast, inTheme } from './support/contrast'

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
})

// A token as the browser paints it, read off a probe rather than from the
// token's text: the hairline is written with an alpha channel in light.
const paintedToken = (name: string): string => {
  const probe = document.createElement('span')
  probe.style.background = `var(${name})`
  document.body.append(probe)
  const painted = getComputedStyle(probe).backgroundColor
  probe.remove()
  return painted
}
