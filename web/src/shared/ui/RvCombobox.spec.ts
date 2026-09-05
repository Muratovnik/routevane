import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
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

function visibleOptions(): string[] {
  return [
    ...(list()?.querySelectorAll<HTMLElement>('[role="option"]') ?? []),
  ].map((option) => option.textContent?.trim() ?? '')
}
function groupLabels(): string[] {
  return [...document.querySelectorAll('.rv-search-select__group-label')].map(
    (label) => label.textContent?.trim() ?? '',
  )
}
function search() {
  return new DOMWrapper(
    document.querySelector<HTMLInputElement>(
      '.rv-search-select__search input',
    )!,
  )
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

    await wrapper.get('.rv-search-select__trigger').trigger('click')
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

    await wrapper.get('.rv-search-select__trigger').trigger('click')
    await flushPromises()

    await search().setValue('sing')
    await flushPromises()
    expect(visibleOptions()).toHaveLength(1)
    expect(groupLabels()).toEqual(['Applications'])

    await search().setValue('.bat')
    await flushPromises()
    expect(groupLabels()).toEqual(['Routers'])

    await search().setValue('nothing-here')
    await flushPromises()
    expect(visibleOptions()).toHaveLength(0)
    expect(document.querySelector('[role="status"]')?.textContent).toContain(
      'Nothing found.',
    )
  })

  it('chooses with the keyboard and reads the choice back in the field', async () => {
    const wrapper = mountCombobox()
    open = wrapper

    await wrapper.get('button').trigger('click')
    await flushPromises()
    await search().setValue('sing')
    await flushPromises()

    await search().trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    await search().trigger('keydown', { key: 'Enter' })
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([['singbox']])
    await wrapper.setProps({ modelValue: 'singbox' })
    await flushPromises()
    expect(wrapper.get('button').text()).toBe('sing-box')
  })

  it('clears a closed search and offers all options when reopened', async () => {
    const wrapper = mountCombobox()
    open = wrapper
    await wrapper.get('button').trigger('click')
    await flushPromises()
    await search().setValue('sing')
    await flushPromises()
    expect(visibleOptions()).toHaveLength(1)
    await search().trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(search().element.value).toBe('')
    expect(visibleOptions()).toHaveLength(3)
  })

  it('preserves the field label and refuses disabled choices', async () => {
    const wrapper = mountCombobox({
      groups: undefined,
      options: [{ value: 'blocked', label: 'Blocked', disabled: true }],
    })
    open = wrapper
    expect(wrapper.get('button').attributes('id')).toBe('target')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    document.querySelector<HTMLElement>('[role="option"]')!.click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('chooses with the pointer', async () => {
    const wrapper = mountCombobox()
    open = wrapper

    await wrapper.get('.rv-search-select__trigger').trigger('click')
    await flushPromises()
    const option = [
      ...(list()?.querySelectorAll<HTMLElement>('[role="option"]') ?? []),
    ].find((candidate) => candidate.textContent?.includes('Keenetic'))
    option?.click()
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([['keenetic']])
  })
})
