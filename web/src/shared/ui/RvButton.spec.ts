import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import RvButton from '@/shared/ui/RvButton.vue'

describe('RvButton', () => {
  it('announces and blocks a pending action without hiding its name', () => {
    const wrapper = mount(RvButton, {
      props: { loading: true, loadingLabel: 'Saving route' },
      slots: { default: 'Save' },
    })

    const button = wrapper.get<HTMLButtonElement>('button')
    expect(button.element.disabled).toBe(true)
    expect(button.attributes('aria-busy')).toBe('true')
    expect(button.attributes('aria-label')).toBe('Saving route')
    expect(button.text()).toContain('Save')
    expect(button.find('.rv-button__spinner').exists()).toBe(true)
  })

  it('prevents a disabled link from navigating or firing its action', () => {
    const action = vi.fn()
    const wrapper = mount(RvButton, {
      props: { disabled: true, href: '/artifact' },
      slots: { default: 'Download' },
      attrs: { onClick: action },
    })

    const click = new MouseEvent('click', { bubbles: true, cancelable: true })
    wrapper.get('a').element.dispatchEvent(click)

    expect(click.defaultPrevented).toBe(true)
    expect(action).not.toHaveBeenCalled()
  })
})
