import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import OutputConfirmDialog from '@/features/view-profile/ui/OutputConfirmDialog.vue'

const renderDialog = (kind: 'reissue' | 'remove') =>
  render(OutputConfirmDialog, {
    props: { busy: false, error: null, kind, open: true, target: 'sing-box' },
  })

// Both acts end something other devices rely on — the link they read, or the
// format they were given — so neither may look like an ordinary save.
describe('OutputConfirmDialog', () => {
  it.each([
    ['reissue', 'Issue a new link'],
    ['remove', 'Remove format'],
  ] as const)(
    'confirms %s with the destructive button, and cancelling stays calm',
    async (kind, submit) => {
      const screen = await renderDialog(kind)

      await expect
        .element(screen.getByRole('button', { name: submit, exact: true }))
        .toHaveClass('rv-button--danger')
      await expect
        .element(screen.getByRole('button', { name: 'Cancel' }))
        .not.toHaveClass('rv-button--danger')
    },
  )
})
