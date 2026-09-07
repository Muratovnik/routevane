import { describe, expect, it } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'

import RvInfoTip from '@/shared/ui/RvInfoTip.vue'

const LABEL = 'How this works'
const EXPLANATION =
  'A subscription link is fetched by the device on its own schedule.'

const renderInfoTip = () =>
  render(RvInfoTip, {
    props: { label: LABEL, text: EXPLANATION },
  })

describe('RvInfoTip', () => {
  // The panel stays open once it is open: it closes on a deliberate dismissal,
  // never because the focus arrived in it. An informer that vanished when the
  // keyboard reached it could not be read by the keyboard at all.
  it('opens on its trigger and stays open while it is being read', async () => {
    const screen = await renderInfoTip()

    const trigger = screen.getByRole('button', { name: LABEL })
    const explanation = screen.getByText(EXPLANATION)
    await expect.element(explanation).not.toBeInTheDocument()

    await trigger.click()

    await expect.element(trigger).toHaveAttribute('aria-expanded', 'true')
    await expect.element(explanation).toBeVisible()
    // The explanation is a viewport overlay, not part of whatever it explains.
    expect(screen.container.contains(explanation.element())).toBe(false)
  })

  it('closes on Escape and hands focus back to its trigger', async () => {
    const screen = await renderInfoTip()

    const trigger = screen.getByRole('button', { name: LABEL })
    await trigger.click()
    await expect.element(screen.getByText(EXPLANATION)).toBeVisible()

    await userEvent.keyboard('{Escape}')

    await expect.element(screen.getByText(EXPLANATION)).not.toBeInTheDocument()
    await expect.element(trigger).toHaveAttribute('aria-expanded', 'false')
    await expect.element(trigger).toHaveFocus()
  })
})
