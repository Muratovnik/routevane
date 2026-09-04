import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import RvDisclosure from '@/shared/ui/RvDisclosure.vue'

describe('RvDisclosure', () => {
  it('keeps progressive content inert until its named trigger opens it', async () => {
    const wrapper = mount(RvDisclosure, {
      props: { summary: 'Supported formats' },
      slots: { default: '<a href="/formats">Keenetic</a>' },
      global: { stubs: { RvIcon: true } },
    })

    const trigger = wrapper.get<HTMLButtonElement>('button')
    const panel = wrapper.get<HTMLElement>('[role="region"]')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(panel.attributes('aria-hidden')).toBe('true')
    expect(panel.attributes('inert')).toBe('')

    await trigger.trigger('click')

    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(panel.attributes('aria-hidden')).toBe('false')
    expect(panel.attributes('inert')).toBeUndefined()
    expect(wrapper.emitted('toggle')).toEqual([[true]])
  })
})
