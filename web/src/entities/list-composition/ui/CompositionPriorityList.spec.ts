import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'

import { useLocale } from '@/shared/i18n/useLocale'

import CompositionPriorityList from './CompositionPriorityList.vue'

describe('CompositionPriorityList', () => {
  afterEach(() => useLocale().setLocale('en'))

  it('shows the composition without visible ordinals and supports keyboard reordering', async () => {
    const wrapper = mount(CompositionPriorityList, {
      props: {
        items: [
          { id: 'alpha', title: 'Alpha' },
          { id: 'beta', title: 'Beta' },
        ],
      },
      global: { stubs: { RvIcon: true } },
    })

    expect(wrapper.text()).toContain('List order sets the priority')
    expect(
      wrapper.findAll('.priority-list__item').map((row) => row.text()),
    ).toEqual(['Alpha', 'Beta'])
    expect(wrapper.find('.priority-list__position').exists()).toBe(false)
    expect(
      wrapper.get('.priority-list__handle').attributes('aria-label'),
    ).toContain('position 1')
    await wrapper.get('.priority-list__handle').trigger('keydown', {
      key: 'ArrowDown',
    })
    expect(wrapper.emitted('reorder')?.at(-1)).toEqual([['beta', 'alpha']])
    wrapper.unmount()
  })

  it('shows overlap tags, an honest unknown state, and an overrideable context', async () => {
    const wrapper = mount(CompositionPriorityList, {
      props: {
        description: 'Library-wide order',
        items: [
          { id: 'alpha', overlaps: ['Beta'], title: 'Alpha' },
          { id: 'beta', overlaps: null, title: 'Beta' },
        ],
        title: 'Default priority',
      },
      global: { stubs: { RvIcon: true } },
    })

    expect(wrapper.text()).toContain('Default priority')
    expect(wrapper.text()).toContain('Library-wide order')
    expect(wrapper.text()).toContain('Overlap: Beta')
    expect(wrapper.text()).toContain('Overlaps unknown')
    await wrapper.setProps({ overlapPending: true })
    expect(wrapper.text()).toContain('Calculating overlaps')
    wrapper.unmount()
  })

  it('disables its drag handles with the owning operation', () => {
    const wrapper = mount(CompositionPriorityList, {
      props: {
        disabled: true,
        items: [{ id: 'alpha', title: 'Alpha' }],
      },
      global: { stubs: { RvIcon: true } },
    })

    expect(
      wrapper.get<HTMLButtonElement>('.priority-list__handle').element.disabled,
    ).toBe(true)
    wrapper.unmount()
  })
})
