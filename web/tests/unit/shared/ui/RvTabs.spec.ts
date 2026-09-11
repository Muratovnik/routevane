import { describe, expect, it } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'
import { defineComponent, h, ref } from 'vue'

import RvTabs from '@/shared/ui/RvTabs.vue'

const TABS = [
  { id: 'overview', label: 'Contents' },
  { id: 'outputs', label: 'Publishing' },
  { id: 'file', label: 'File' },
  { id: 'diagnostics', label: 'Diagnostics' },
]

const Host = defineComponent({
  setup() {
    const active = ref('overview')
    return () =>
      h(
        RvTabs,
        {
          label: 'Profile sections',
          modelValue: active.value,
          tabs: TABS,
          'onUpdate:modelValue': (value: string) => {
            active.value = value
          },
        },
        {
          overview: () =>
            h('div', [h('input', { 'aria-label': 'Profile name' })]),
          outputs: () => h('div', 'Existing connections'),
          file: () => h('div'),
          diagnostics: () => h('div', 'No diagnostics'),
        },
      )
  },
})

describe('RvTabs', () => {
  it('delegates selection, keyboard traversal and panel focus to Reka', async () => {
    const screen = await render(Host)
    const contents = screen.getByRole('tab', { name: 'Contents' })
    const connection = screen.getByRole('tab', { name: 'Publishing' })
    const file = screen.getByRole('tab', { name: 'File' })
    const diagnostics = screen.getByRole('tab', { name: 'Diagnostics' })

    contents.element().focus()
    await userEvent.keyboard('{ArrowLeft}')
    await expect.element(diagnostics).toHaveFocus()
    await expect.element(diagnostics).toHaveAttribute('aria-selected', 'true')

    await userEvent.keyboard('{Home}')
    await expect.element(contents).toHaveFocus()
    await userEvent.keyboard('{End}')
    await expect.element(diagnostics).toHaveFocus()
    await userEvent.keyboard('{ArrowRight}')
    await expect.element(contents).toHaveFocus()

    await connection.click()
    await expect.element(connection).toHaveAttribute('aria-selected', 'true')
    const panel = screen.getByRole('tabpanel', { name: 'Publishing' })
    await expect.element(panel).toHaveAttribute('tabindex', '0')

    connection.element().focus()
    await userEvent.keyboard('{Tab}')
    await expect.element(panel).toHaveFocus()
    await userEvent.keyboard('{Shift>}{Tab}{/Shift}')
    await expect.element(connection).toHaveFocus()

    await file.click()
    await userEvent.keyboard('{Tab}')
    await expect
      .element(screen.getByRole('tabpanel', { name: 'File' }))
      .toHaveFocus()
  })

  it('retains hidden panel content and its draft while switching tabs', async () => {
    const screen = await render(Host)
    const field = screen.getByLabelText('Profile name')
    await field.fill('Unsaved profile')
    const fieldElement = field.element()
    const contentsID = screen
      .getByRole('tab', { name: 'Contents' })
      .element()
      .getAttribute('aria-controls')

    await screen.getByRole('tab', { name: 'File' }).click()
    expect(document.getElementById(contentsID ?? '')?.hidden).toBe(true)
    expect(fieldElement.isConnected).toBe(true)
    await screen.getByRole('tab', { name: 'Contents' }).click()

    await expect.element(field).toHaveValue('Unsaved profile')
  })
})
