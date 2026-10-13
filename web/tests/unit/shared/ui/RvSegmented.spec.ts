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
})
