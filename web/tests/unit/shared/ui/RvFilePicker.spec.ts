import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import RvFilePicker from '@/shared/ui/RvFilePicker.vue'

const props = {
  actionLabel: 'Choose file',
  emptyLabel: 'No file selected',
  inputId: 'transfer-file',
  label: 'Configuration file',
}

describe('RvFilePicker', () => {
  it('keeps a labelled native chooser behind the custom surface', async () => {
    const wrapper = mount(RvFilePicker, {
      props,
      global: { stubs: { RvIcon: true } },
    })
    const input = wrapper.get<HTMLInputElement>('input[type="file"]')
    expect(wrapper.get('label').attributes('for')).toBe('transfer-file')
    expect(input.attributes('id')).toBe('transfer-file')
    expect(wrapper.text()).toContain('No file selected')

    const file = new File(['{}'], 'routevane.json', {
      type: 'application/json',
    })
    Object.defineProperty(input.element, 'files', {
      configurable: true,
      value: { item: () => file, length: 1 },
    })
    await input.trigger('change')

    expect(wrapper.emitted('select')?.at(-1)).toEqual([file])
  })

  it('does not open or accept a file while disabled', async () => {
    const wrapper = mount(RvFilePicker, {
      props: { ...props, disabled: true },
      global: { stubs: { RvIcon: true } },
    })
    expect(wrapper.get<HTMLButtonElement>('button').element.disabled).toBe(true)
    expect(wrapper.get<HTMLInputElement>('input').element.disabled).toBe(true)
    expect(wrapper.emitted('select')).toBeUndefined()
  })
})
