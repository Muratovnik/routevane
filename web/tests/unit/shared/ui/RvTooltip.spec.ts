import { describe, expect, it } from 'vitest'
import { page, userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'
import { defineComponent, h } from 'vue'

import RvTooltip from '@/shared/ui/RvTooltip.vue'

// The painted panel has no role or name of its own (the tooltip role sits on a
// hidden node inside it), so it is read through its hook.
const panel = () => page.getByTestId('rv-tooltip')

const renderTooltip = (props: { disabled?: boolean; text: string }) =>
  render(
    defineComponent({
      setup: () => () =>
        h('div', [
          h(RvTooltip, props, () =>
            h('button', { type: 'button' }, 'Collapse sidebar'),
          ),
          // Well clear of the panel, which opens to the trigger's right.
          h('p', { style: 'margin-top: 12rem' }, [
            h('button', { type: 'button' }, 'Elsewhere'),
          ]),
        ]),
    }),
  )

describe('RvTooltip', () => {
  // Keyboard focus is a way in as well as the pointer, and the words the panel
  // shows are what a reader's software hears as the control's description.
  it('shows on focus as its trigger’s description and leaves on Escape', async () => {
    const screen = await renderTooltip({ text: 'Expand the sidebar' })
    const trigger = screen.getByRole('button', { name: 'Collapse sidebar' })

    await expect.element(panel()).not.toBeInTheDocument()
    await userEvent.tab()
    await expect.element(trigger).toHaveFocus()
    await expect.element(panel()).toBeVisible()
    // The hidden node carrying the tooltip role repeats the words for a
    // reader's software, so the panel's text holds them at least once.
    expect(panel().element().textContent).toContain('Expand the sidebar')
    await expect
      .element(trigger)
      .toHaveAccessibleDescription('Expand the sidebar')

    await userEvent.keyboard('{Escape}')
    await expect.element(panel()).not.toBeInTheDocument()
    await expect.element(trigger).toHaveFocus()
  })

  it('shows on hover beside its trigger and leaves when the pointer does', async () => {
    const screen = await renderTooltip({ text: 'Lists' })
    const trigger = screen.getByRole('button', { name: 'Collapse sidebar' })

    await trigger.hover()
    await expect.element(panel()).toBeVisible()
    await expect.element(panel()).toHaveAttribute('data-side', 'right')

    await screen.getByRole('button', { name: 'Elsewhere' }).hover()
    await expect.element(panel()).not.toBeInTheDocument()
  })

  // The sidebar's tooltips stand down while the sidebar is wide enough to name
  // its own links.
  it('shows nothing while it is disabled', async () => {
    const screen = await renderTooltip({ disabled: true, text: 'Lists' })
    const trigger = screen.getByRole('button', { name: 'Collapse sidebar' })

    await userEvent.tab()
    await expect.element(trigger).toHaveFocus()
    await trigger.hover()
    await new Promise((resolve) => setTimeout(resolve, 400))
    await expect.element(panel()).not.toBeInTheDocument()
    await expect.element(trigger).not.toHaveAttribute('aria-describedby')
  })
})
