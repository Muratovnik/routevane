import { describe, expect, it } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'
import { defineComponent, h, ref } from 'vue'

import RvDialog from '@/shared/ui/RvDialog.vue'
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

  it('dismisses when keyboard focus deliberately moves elsewhere', async () => {
    const Host = defineComponent({
      setup() {
        return () =>
          h('div', [
            h(RvInfoTip, { label: LABEL, text: EXPLANATION }),
            h('button', { type: 'button' }, 'Continue'),
          ])
      },
    })
    const screen = await render(Host)

    await screen.getByRole('button', { name: LABEL }).click()
    await userEvent.keyboard('{Tab}')

    await expect.element(screen.getByText(EXPLANATION)).not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: 'Continue' }))
      .toHaveFocus()
  })

  it('yields focus and the accessibility tree to a nested dialog', async () => {
    const Host = defineComponent({
      setup() {
        const open = ref(false)
        return () =>
          h('div', [
            h(RvInfoTip, { label: LABEL, text: EXPLANATION }),
            h(
              'button',
              {
                onClick: () => {
                  open.value = true
                },
                type: 'button',
              },
              'Open decision',
            ),
            h(
              RvDialog,
              {
                closeLabel: 'Close',
                description: 'Review the requested change.',
                open: open.value,
                title: 'Confirm change',
                variant: 'panel',
                'onUpdate:open': (value: boolean) => {
                  open.value = value
                },
              },
              { default: () => h('button', { type: 'button' }, 'Confirm') },
            ),
          ])
      },
    })
    const screen = await render(Host)

    await screen.getByRole('button', { name: LABEL }).click()
    const explanation = screen.getByText(EXPLANATION).element()
    ;(
      screen
        .getByRole('button', { name: 'Open decision' })
        .element() as HTMLElement
    ).click()
    const decision = screen.getByRole('dialog', { name: 'Confirm change' })
    await expect.element(decision).toBeVisible()
    expect(decision.element().contains(document.activeElement)).toBe(true)
    expect(screen.getByRole('dialog').all()).toHaveLength(1)
    expect(getComputedStyle(explanation).pointerEvents).toBe('none')
  })
})
