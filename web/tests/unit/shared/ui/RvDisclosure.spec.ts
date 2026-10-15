import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import RvDisclosure from '@/shared/ui/RvDisclosure.vue'

describe('RvDisclosure', () => {
  it('keeps progressive content inert until its named trigger opens it', async () => {
    const screen = await render(RvDisclosure, {
      props: { summary: 'Supported formats' },
      slots: { default: '<a href="/formats">Keenetic</a>' },
    })

    const trigger = screen.getByRole('button', { name: 'Supported formats' })
    // The panel names itself after the trigger and is hidden until it opens,
    // so the accessible query has to reach past `aria-hidden` to see it shut.
    const panel = screen.getByRole('region', {
      includeHidden: true,
      name: 'Supported formats',
    })
    await expect.element(trigger).toHaveAttribute('aria-expanded', 'false')
    await expect.element(panel).toHaveAttribute('aria-hidden', 'true')
    await expect.element(panel).toHaveAttribute('inert', '')

    await trigger.click()

    await expect.element(trigger).toHaveAttribute('aria-expanded', 'true')
    await expect.element(panel).toHaveAttribute('aria-hidden', 'false')
    await expect.element(panel).not.toHaveAttribute('inert')
    expect(screen.emitted('toggle')).toEqual([[true]])
  })

  it('retains a draft while uncontrolled content is closed and reopened', async () => {
    const screen = await render(RvDisclosure, {
      props: { summary: 'Advanced settings' },
      slots: { default: '<input aria-label="Prefix" />' },
    })
    const trigger = screen.getByRole('button', { name: 'Advanced settings' })

    await trigger.click()
    const field = screen.getByLabelText('Prefix')
    await field.fill('routevane')
    await trigger.click()
    const panel = screen.getByRole('region', {
      includeHidden: true,
      name: 'Advanced settings',
    })
    await expect.element(panel).toHaveAttribute('inert', '')
    await expect.element(field).toBeInTheDocument()
    await trigger.click()

    await expect.element(field).toHaveValue('routevane')
  })

  // The informer is a control of its own, so it sits beside the toggle, never
  // inside it, follows it in focus order, and takes room the toggle gives up.
  it('places a summary-end control after the toggle without overlapping it', async () => {
    const screen = await render(RvDisclosure, {
      props: { summary: 'Subscription link · Keenetic' },
      slots: { 'summary-end': '<button type="button">About the link</button>' },
    })
    const trigger = screen
      .getByRole('button', { name: 'Subscription link · Keenetic' })
      .element()
    const info = screen
      .getByRole('button', { name: 'About the link' })
      .element()

    expect(trigger.contains(info)).toBe(false)
    expect(
      trigger.compareDocumentPosition(info) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(trigger.getBoundingClientRect().right).toBeLessThanOrEqual(
      info.getBoundingClientRect().left,
    )
  })

  it('draws no summary-end box when the slot is empty', async () => {
    const screen = await render(RvDisclosure, { props: { summary: 'Plain' } })
    const trigger = screen.getByRole('button', { name: 'Plain' }).element()

    expect(trigger.parentElement?.children).toHaveLength(1)
  })
})
