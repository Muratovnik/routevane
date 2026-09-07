import { describe, expect, it } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'
import { defineComponent, h } from 'vue'

import type { ChoiceGroup } from '@/shared/ui/kinds'
import RvCombobox from '@/shared/ui/RvCombobox.vue'

const GROUPS: ChoiceGroup[] = [
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

const PROPS = {
  emptyLabel: 'Nothing found.',
  groups: GROUPS,
  inputId: 'target',
  modelValue: '',
  placeholder: 'Choose a device or application',
  toggleLabel: 'Show the list',
}

const renderCombobox = (props: Record<string, unknown> = {}) =>
  render(RvCombobox, { props: { ...PROPS, ...props } })

describe('RvCombobox', () => {
  it('opens the whole list under its group names', async () => {
    const screen = await renderCombobox()

    await screen
      .getByRole('button', { name: 'Choose a device or application' })
      .click()

    await expect.element(screen.getByText('Routers')).toBeVisible()
    await expect.element(screen.getByText('Applications')).toBeVisible()
    expect(screen.getByRole('option').all()).toHaveLength(3)
    // A choice states its format and, when it has one, the limit that stops it
    // being used — in words, not in colour alone.
    await expect.element(screen.getByText('.bat')).toBeVisible()
    await expect
      .element(screen.getByText('Cannot hold this list'))
      .toBeVisible()
  })

  // Typing narrows the list in place, and a run that keeps nothing stops being
  // drawn rather than standing empty under its own heading.
  it('filters by name and by format, dropping the runs that keep nothing', async () => {
    const screen = await renderCombobox()

    await screen
      .getByRole('button', { name: 'Choose a device or application' })
      .click()
    const search = screen.getByLabelText('Search')

    await search.fill('sing')
    expect(screen.getByRole('option').all()).toHaveLength(1)
    await expect.element(screen.getByText('Applications')).toBeVisible()
    await expect.element(screen.getByText('Routers')).not.toBeInTheDocument()

    await search.fill('.bat')
    await expect.element(screen.getByText('Routers')).toBeVisible()
    await expect
      .element(screen.getByText('Applications'))
      .not.toBeInTheDocument()

    await search.fill('nothing-here')
    expect(screen.getByRole('option').all()).toHaveLength(0)
    await expect
      .element(screen.getByRole('status'))
      .toHaveTextContent('Nothing found.')
  })

  it('chooses with the keyboard and reads the choice back in the field', async () => {
    const screen = await renderCombobox()

    const trigger = screen.getByRole('button', {
      name: 'Choose a device or application',
    })
    await trigger.click()
    await screen.getByLabelText('Search').fill('sing')

    await userEvent.keyboard('{ArrowDown}')
    await userEvent.keyboard('{Enter}')

    expect(screen.emitted('update:modelValue')).toEqual([['singbox']])
    await screen.rerender({ modelValue: 'singbox' })
    await expect
      .element(screen.getByRole('button', { name: 'sing-box' }))
      .toBeVisible()
  })

  it('clears a closed search and offers all options when reopened', async () => {
    const screen = await renderCombobox()

    const trigger = screen.getByRole('button', {
      name: 'Choose a device or application',
    })
    await trigger.click()
    await screen.getByLabelText('Search').fill('sing')
    expect(screen.getByRole('option').all()).toHaveLength(1)

    await userEvent.keyboard('{Escape}')
    await expect.element(trigger).toHaveAttribute('aria-expanded', 'false')

    await trigger.click()
    await expect.element(screen.getByLabelText('Search')).toHaveValue('')
    expect(screen.getByRole('option').all()).toHaveLength(3)
  })

  // The field's own label has to reach the control it names, which is what the
  // caller's `inputId` is for.
  it('preserves the field label and refuses disabled choices', async () => {
    const LabelHost = defineComponent({
      setup() {
        return () =>
          h('div', [
            h('label', { for: 'target' }, 'Target'),
            h(RvCombobox, {
              ...PROPS,
              groups: undefined,
              options: [{ disabled: true, label: 'Blocked', value: 'blocked' }],
            }),
          ])
      },
    })
    const screen = await render(LabelHost)

    const trigger = screen.getByLabelText('Target')
    await trigger.click()

    await screen.getByRole('option', { name: 'Blocked' }).click({ force: true })
    expect(screen.emitted('update:modelValue')).toBeUndefined()
  })

  it('chooses with the pointer', async () => {
    const screen = await renderCombobox()

    await screen
      .getByRole('button', { name: 'Choose a device or application' })
      .click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()

    expect(screen.emitted('update:modelValue')).toEqual([['keenetic']])
  })
})
