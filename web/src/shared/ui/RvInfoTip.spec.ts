import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'

import RvInfoTip from '@/shared/ui/RvInfoTip.vue'

function panel(): HTMLElement | null {
  return document.body.querySelector<HTMLElement>('.rv-infotip__panel')
}

async function settle(): Promise<void> {
  await flushPromises()
  await new Promise((resolve) => {
    setTimeout(resolve, 0)
  })
}

function mountInfoTip() {
  return mount(RvInfoTip, {
    attachTo: document.body,
    props: {
      label: 'How this works',
      text: 'A subscription link is fetched by the device on its own schedule.',
    },
    global: { stubs: { RvIcon: true } },
  })
}

describe('RvInfoTip', () => {
  let open: { unmount: () => void } | null = null

  afterEach(() => {
    open?.unmount()
    open = null
    document.body.innerHTML = ''
  })

  // The panel stays open once it is open: it closes on a deliberate dismissal,
  // never because the focus arrived in it. An informer that vanished when the
  // keyboard reached it could not be read by the keyboard at all.
  it('opens on its trigger and stays open while it is being read', async () => {
    const wrapper = mountInfoTip()
    open = wrapper

    const trigger = wrapper.get('.rv-infotip__trigger')
    expect(panel()).toBeNull()

    await trigger.trigger('click')
    await settle()

    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(panel()?.textContent).toContain('A subscription link')
    // The explanation is a viewport overlay, not part of whatever it explains.
    expect(wrapper.element.contains(panel())).toBe(false)
  })

  it('closes on Escape and hands focus back to its trigger', async () => {
    const wrapper = mountInfoTip()
    open = wrapper

    const trigger = wrapper.get<HTMLButtonElement>('.rv-infotip__trigger')
    await trigger.trigger('click')
    await settle()
    expect(panel()).not.toBeNull()

    document.dispatchEvent(
      new KeyboardEvent('keydown', { bubbles: true, key: 'Escape' }),
    )
    await settle()

    expect(panel()).toBeNull()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
  })
})
