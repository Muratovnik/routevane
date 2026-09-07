import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import RvFilePicker from '@/shared/ui/RvFilePicker.vue'

const PROPS = {
  actionLabel: 'Choose file',
  emptyLabel: 'No file selected',
  inputId: 'transfer-file',
  label: 'Configuration file',
}

describe('RvFilePicker', () => {
  it('keeps a labelled native chooser behind the custom surface', async () => {
    const screen = await render(RvFilePicker, {
      props: PROPS,
    })

    // The drawn surface is a button; the file input is what the visible label
    // names, so reaching the native chooser through that label is the check
    // that the two are still wired to each other.
    const chooser = screen.getByLabelText('Configuration file')
    await expect.element(chooser).toHaveAttribute('type', 'file')
    await expect.element(screen.getByText('No file selected')).toBeVisible()

    const file = new File(['{}'], 'routevane.json', {
      type: 'application/json',
    })
    await chooser.upload(file)

    const selected = screen.emitted<[File]>('select')?.at(-1)
    expect(selected?.[0]?.name).toBe('routevane.json')
  })

  it('does not open or accept a file while disabled', async () => {
    const screen = await render(RvFilePicker, {
      props: { ...PROPS, disabled: true },
    })

    await expect
      .element(screen.getByRole('button', { name: 'Choose file' }))
      .toBeDisabled()
    await expect
      .element(screen.getByLabelText('Configuration file'))
      .toBeDisabled()
    expect(screen.emitted('select')).toBeUndefined()
  })
})
