import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'

import type { ChoiceGroup } from '@/shared/ui/kinds'
import RvCombobox from '@/shared/ui/RvCombobox.vue'

const groups: ChoiceGroup[] = [
  {
    key: 'router',
    label: 'Routers',
    options: [
      {
        label: 'Keenetic',
        mono: '.bat',
        note: 'Can be configured from Routevane',
        value: 'keenetic',
      },
    ],
  },
  {
    key: 'app',
    label: 'Applications',
    options: [
      {
        label: 'sing-box',
        mono: '.json',
        note: 'Subscription or manual installation',
        value: 'singbox',
      },
      {
        label: 'Limited fixture',
        mono: '.txt',
        value: 'limited-fixture',
        warning: 'Cannot hold this list',
      },
    ],
  },
]

function list(): HTMLElement | null {
  return document.body.querySelector<HTMLElement>('[role="listbox"]')
}

// What a reader can actually see: a filtered-out run and a filtered-out choice
// are hidden in place rather than removed, so the query says so too.
function visibleOptions(): string[] {
  return [
    ...(list()?.querySelectorAll<HTMLElement>(
      '.rv-combobox__group:not([hidden]) [role="option"]:not([hidden])',
    ) ?? []),
  ].map((option) => option.textContent?.trim() ?? '')
}

function groupLabels(): string[] {
  return [
    ...(list()?.querySelectorAll<HTMLElement>(
      '.rv-combobox__group:not([hidden]) .rv-combobox__group-label',
    ) ?? []),
  ].map((label) => label.textContent?.trim() ?? '')
}

function mountCombobox(props: Record<string, unknown> = {}) {
  return mount(RvCombobox, {
    attachTo: document.body,
    props: {
      emptyLabel: 'Nothing found.',
      groups,
      inputId: 'target',
      modelValue: '',
      placeholder: 'Choose a device or application',
      toggleLabel: 'Show the list',
      ...props,
    },
    global: { stubs: { RvIcon: true } },
  })
}

describe('RvCombobox', () => {
  let open: { unmount: () => void } | null = null

  afterEach(() => {
    open?.unmount()
    open = null
    document.body.innerHTML = ''
  })

  it('opens the whole list under its group names', async () => {
    const wrapper = mountCombobox()
    open = wrapper

    await wrapper.get('.rv-combobox__toggle').trigger('click')
    await flushPromises()

    expect(groupLabels()).toEqual(['Routers', 'Applications'])
    expect(visibleOptions()).toHaveLength(3)
    // A choice states its format and, when it has one, the limit that stops it
    // being used — in words, not in colour alone.
    expect(list()?.textContent).toContain('.bat')
    expect(list()?.textContent).toContain('Cannot hold this list')
  })

  // Typing narrows the list in place, and a run that keeps nothing stops being
  // drawn rather than standing empty under its own heading.
  it('filters by name and by format, dropping the runs that keep nothing', async () => {
    const wrapper = mountCombobox()
    open = wrapper

    const field = wrapper.get('.rv-combobox__input')
    await wrapper.get('.rv-combobox__toggle').trigger('click')
    await flushPromises()

    await field.setValue('sing')
    await flushPromises()
    expect(visibleOptions()).toHaveLength(1)
    expect(groupLabels()).toEqual(['Applications'])

    await field.setValue('.bat')
    await flushPromises()
    expect(groupLabels()).toEqual(['Routers'])

    await field.setValue('nothing-here')
    await flushPromises()
    expect(visibleOptions()).toHaveLength(0)
    expect(list()?.textContent).toContain('Nothing found.')
  })

  it('chooses with the keyboard and reads the choice back in the field', async () => {
    const wrapper = mountCombobox()
    open = wrapper

    const field = wrapper.get('.rv-combobox__input')
    await field.trigger('click')
    await flushPromises()
    await field.setValue('sing')
    await flushPromises()

    await field.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    await field.trigger('keydown', { key: 'Enter' })
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([['singbox']])
    await wrapper.setProps({ modelValue: 'singbox' })
    await flushPromises()
    expect(
      wrapper.get<HTMLInputElement>('.rv-combobox__input').element.value,
    ).toBe('sing-box')
  })

  it('drops a closed highlight and can choose after reopening', async () => {
    const wrapper = mountCombobox()
    open = wrapper

    const field = wrapper.get('.rv-combobox__input')
    await field.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()

    const firstActive = field.attributes('aria-activedescendant')
    expect(firstActive).toBeTruthy()
    if (firstActive === undefined) throw new Error('no active descendant')
    expect(document.getElementById(firstActive)).not.toBeNull()

    await field.trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(field.attributes('aria-expanded')).toBe('false')
    expect(field.attributes('aria-activedescendant')).toBeUndefined()

    await field.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const reopenedActive = field.attributes('aria-activedescendant')
    expect(reopenedActive).toBeTruthy()
    if (reopenedActive === undefined) throw new Error('no active descendant')
    expect(document.getElementById(reopenedActive)).not.toBeNull()

    await field.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([['keenetic']])
    expect(field.attributes('aria-activedescendant')).toBeUndefined()
  })

  it('chooses with the pointer', async () => {
    const wrapper = mountCombobox()
    open = wrapper

    await wrapper.get('.rv-combobox__toggle').trigger('click')
    await flushPromises()
    const option = [
      ...(list()?.querySelectorAll<HTMLElement>('[role="option"]') ?? []),
    ].find((candidate) => candidate.textContent?.includes('Keenetic'))
    option?.click()
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([['keenetic']])
  })
})
