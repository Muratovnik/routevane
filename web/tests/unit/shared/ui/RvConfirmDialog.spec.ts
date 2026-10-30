import { describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { render } from 'vitest-browser-vue'
import { defineComponent, h, ref } from 'vue'

import RvConfirmDialog from '@/shared/ui/RvConfirmDialog.vue'

type Scenario = {
  busy?: boolean
  error?: string | null
  irreversible?: string
  onConfirm?: () => void
}

// A host that owns the open state, the way every caller does.
const host = (scenario: Scenario) =>
  defineComponent(() => {
    const open = ref(true)
    return () =>
      h(
        RvConfirmDialog,
        {
          busy: scenario.busy ?? false,
          cancelLabel: 'Cancel',
          closeLabel: 'Close',
          confirmLabel: 'Delete',
          error: scenario.error ?? null,
          irreversible: scenario.irreversible,
          onConfirm: scenario.onConfirm,
          'onUpdate:open': (value: boolean) => {
            open.value = value
          },
          open: open.value,
          title: 'Delete list «ChatGPT»?',
        },
        { default: () => 'Its entries are deleted with it.' },
      )
  })

describe('RvConfirmDialog', () => {
  it('names the act, states its consequence and asks once', async () => {
    const onConfirm = vi.fn()
    await render(host({ irreversible: 'This cannot be undone.', onConfirm }))
    const dialog = page.getByRole('dialog', { name: 'Delete list «ChatGPT»?' })

    await expect
      .element(dialog.getByText('Its entries are deleted with it.'))
      .toBeVisible()
    await expect
      .element(dialog.getByText('This cannot be undone.'))
      .toBeVisible()
    await dialog.getByRole('button', { name: 'Delete' }).click()

    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('keeps a refusal inside, with the act still offered', async () => {
    await render(host({ error: 'The list is in two profiles.' }))
    const dialog = page.getByRole('dialog')

    await expect
      .element(dialog.getByRole('status'))
      .toHaveTextContent('The list is in two profiles.')
    await expect
      .element(dialog.getByRole('button', { name: 'Delete' }))
      .toBeEnabled()
  })

  it('cancels without acting', async () => {
    const onConfirm = vi.fn()
    await render(host({ onConfirm }))

    await page.getByRole('button', { name: 'Cancel' }).click()

    await expect.element(page.getByRole('dialog')).not.toBeInTheDocument()
    expect(onConfirm).not.toHaveBeenCalled()
  })

  // While the act runs the reader cannot walk away from it half done.
  it('cannot be cancelled while the act is running', async () => {
    await render(host({ busy: true }))

    await expect
      .element(page.getByRole('button', { name: 'Cancel' }))
      .toBeDisabled()
  })
})
