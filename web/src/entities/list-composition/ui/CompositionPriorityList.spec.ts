import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'

import { useLocale } from '@/shared/i18n/useLocale'

import CompositionPriorityList from './CompositionPriorityList.vue'

describe('CompositionPriorityList', () => {
  afterEach(() => useLocale().setLocale('en'))

  it('states the winning order and supports keyboard reordering', async () => {
    const wrapper = mount(CompositionPriorityList, {
      props: {
        items: [
          { id: 'alpha', title: 'Alpha' },
          { id: 'beta', title: 'Beta' },
        ],
      },
      global: { stubs: { RvIcon: true } },
    })

    expect(wrapper.text()).toContain('the higher list owns the entry')
    expect(
      wrapper.findAll('.priority-list__item').map((row) => row.text()),
    ).toEqual(['1Alpha', '2Beta'])
    await wrapper.get('.priority-list__handle').trigger('keydown', {
      key: 'ArrowDown',
    })
    expect(wrapper.emitted('reorder')?.at(-1)).toEqual([['beta', 'alpha']])
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
