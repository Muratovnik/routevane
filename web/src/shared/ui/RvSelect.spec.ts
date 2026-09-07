import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'

import type { ChoiceGroup, ChoiceOption } from '@/shared/ui/kinds'
import RvSelect from '@/shared/ui/RvSelect.vue'

const options: ChoiceOption[] = [
  { label: 'As in settings', value: 'default' },
  { label: 'Off', value: 'off' },
  { label: 'Daily', value: 'daily' },
  { disabled: true, label: 'Weekly', value: 'weekly' },
]

const groups: ChoiceGroup[] = [
  {
    key: 'router',
    label: 'Routers',
    options: [{ label: 'Keenetic', mono: '.bat', value: 'keenetic' }],
  },
  {
    key: 'app',
    label: 'Applications',
    options: [{ label: 'sing-box', mono: '.json', value: 'singbox' }],
  },
]

function list(): HTMLElement | null {
  return document.body.querySelector<HTMLElement>('[role="listbox"]')
}

function optionWithText(text: string): HTMLElement | undefined {
  return [
    ...(list()?.querySelectorAll<HTMLElement>('[role="option"]') ?? []),
  ].find((item) => item.textContent?.includes(text))
}

// A list option commits on pointer release, the way an operating system's own
// select does: pressing and dragging off it must not choose anything.
async function chooseOption(text: string): Promise<void> {
  const option = optionWithText(text)
  expect(option, text).toBeDefined()
  option?.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }))
  option?.dispatchEvent(new MouseEvent('pointerup', { bubbles: true }))
  await flushPromises()
}

// The list opens from the keyboard as well as from the pointer, which is the
// half a native select gives for free and this one has to state.
async function openProfile(wrapper: {
  get: (selector: string) => {
    trigger: (event: string, options?: object) => Promise<void>
  }
}): Promise<void> {
  await wrapper.get('.rv-select__trigger').trigger('keydown', { key: 'Enter' })
  await flushPromises()
}

function mountSelect(props: Record<string, unknown>) {
  return mount(RvSelect, {
    attachTo: document.body,
    props: {
      inputId: 'choice',
      modelValue: '',
      placeholder: 'Choose one',
      ...props,
    },
    global: { stubs: { RvIcon: true } },
  })
}

describe('RvSelect', () => {
  let open: { unmount: () => void } | null = null

  afterEach(() => {
    open?.unmount()
    open = null
    document.body.innerHTML = ''
  })

  it('reads the placeholder until something is chosen, then the choice', async () => {
    const wrapper = mountSelect({ modelValue: '', options })
    open = wrapper

    const trigger = wrapper.get('.rv-select__trigger')
    expect(trigger.text()).toContain('Choose one')

    await openProfile(wrapper)
    await chooseOption('Daily')

    expect(wrapper.emitted('update:modelValue')).toEqual([['daily']])
    await wrapper.setProps({ modelValue: 'daily' })
    expect(wrapper.get('.rv-select__trigger').text()).toContain('Daily')
  })

  // The empty string is how every caller stores "nothing chosen", so it must
  // survive the round trip through a primitive that has its own idea of absence.
  it('shows the placeholder for an empty model and never emits one back', async () => {
    const wrapper = mountSelect({ modelValue: '', options })
    open = wrapper

    const value = wrapper.get('.rv-select__value')
    expect(value.text()).toBe('Choose one')

    await openProfile(wrapper)
    await chooseOption('Off')

    expect(wrapper.emitted('update:modelValue')).toEqual([['off']])
  })

  it('refuses a disabled option and a disabled control', async () => {
    const wrapper = mountSelect({ modelValue: '', options })
    open = wrapper

    await openProfile(wrapper)
    const weekly = optionWithText('Weekly')
    expect(weekly?.getAttribute('data-disabled')).not.toBeNull()
    await chooseOption('Weekly')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    await wrapper.setProps({ disabled: true })
    expect(
      wrapper.get<HTMLButtonElement>('.rv-select__trigger').element.disabled,
    ).toBe(true)
  })

  // A list with nothing in it cannot be opened: a control that opens on an
  // empty panel says there is something to choose when there is not.
  it('disables itself when there is nothing to choose', () => {
    const wrapper = mountSelect({ modelValue: '', options: [] })
    open = wrapper

    expect(
      wrapper.get<HTMLButtonElement>('.rv-select__trigger').element.disabled,
    ).toBe(true)
  })

  it('keeps grouped choices under their own names', async () => {
    const wrapper = mountSelect({ groups, modelValue: '' })
    open = wrapper

    await openProfile(wrapper)

    const panel = list()
    expect(panel?.textContent).toContain('Routers')
    expect(panel?.textContent).toContain('Applications')
    expect(panel?.textContent).toContain('.bat')

    await chooseOption('sing-box')
    expect(wrapper.emitted('update:modelValue')).toEqual([['singbox']])
  })
})
