import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import RvSegmented from '@/shared/ui/RvSegmented.vue'

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
  // a mark that holds its place in every option, drawn only on the chosen one,
  // and an edge that stands off the track by the non-text contrast WCAG asks.
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

  it.each(['dark', 'light'])(
    'stands the chosen option off the track by 3:1 in the %s theme',
    async (theme) => {
      document.documentElement.setAttribute('data-rv-theme', theme)
      try {
        const screen = await renderSegmented()
        const chosen = screen
          .getByRole('radio', { name: 'Automatic' })
          .element()
          .closest('label')!
        const track = chosen.parentElement!
        // A colour with no drawn edge behind it states nothing.
        expect(
          parseFloat(getComputedStyle(chosen).borderTopWidth),
        ).toBeGreaterThan(0)
        const edge = getComputedStyle(chosen).borderTopColor
        const ground = getComputedStyle(track).backgroundColor
        expect(contrast(edge, ground)).toBeGreaterThanOrEqual(3)
      } finally {
        document.documentElement.removeAttribute('data-rv-theme')
      }
    },
  )
})

const channel = (value: number): number => {
  const scaled = value / 255
  return scaled <= 0.03928 ? scaled / 12.92 : ((scaled + 0.055) / 1.055) ** 2.4
}

const luminance = (color: string): number => {
  const [red, green, blue] = (color.match(/[\d.]+/g) ?? []).map(Number)
  return (
    0.2126 * channel(red ?? 0) +
    0.7152 * channel(green ?? 0) +
    0.0722 * channel(blue ?? 0)
  )
}

// WCAG relative-luminance contrast of two opaque computed `rgb()` colours.
const contrast = (first: string, second: string): number => {
  const [lighter, darker] = [luminance(first), luminance(second)].sort(
    (a, b) => b - a,
  )
  return (lighter! + 0.05) / (darker! + 0.05)
}
