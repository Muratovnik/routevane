import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import RvCopyButton from '@/shared/ui/RvCopyButton.vue'

const LINK = 'https://127.0.0.1/s/token'
const ORIGINAL_CLIPBOARD = Object.getOwnPropertyDescriptor(
  navigator,
  'clipboard',
)

const stubClipboard = (value: unknown): void => {
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value })
}

const renderButton = () =>
  render(RvCopyButton, {
    props: {
      copiedLabel: 'Copied',
      failedLabel: 'Could not copy',
      label: 'Copy the link',
      value: LINK,
    },
  })

describe('RvCopyButton', () => {
  afterEach(() => {
    vi.useRealTimers()
    if (ORIGINAL_CLIPBOARD === undefined)
      Reflect.deleteProperty(navigator, 'clipboard')
    else Object.defineProperty(navigator, 'clipboard', ORIGINAL_CLIPBOARD)
  })

  it('confirms a write the clipboard accepted, and keeps saying so', async () => {
    // The clock is faked, so the browser's own waiting has to keep advancing
    // with real time or a click would never settle.
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const writeText = vi.fn(async () => {})
    stubClipboard({ writeText })

    const screen = await renderButton()
    const outcome = screen.getByRole('status')

    await screen.getByRole('button', { name: 'Copy the link' }).click()
    await expect.element(outcome).toHaveTextContent('Copied')
    expect(writeText).toHaveBeenCalledWith(LINK)

    // The confirmation is not on a timer: a secret copied a minute ago is still
    // a secret on the clipboard, and a line that erased itself would leave the
    // operator guessing whether to press again.
    await vi.advanceTimersByTimeAsync(60_000)
    await expect.element(outcome).toHaveTextContent('Copied')
  })

  // A confirmation is a fact that happened. A clipboard that refused the write
  // says so, and never borrows the words of one that accepted it.
  it('reports a refusal instead of confirming it', async () => {
    stubClipboard({
      writeText: vi.fn(async () => {
        throw new Error('NotAllowedError')
      }),
    })

    const screen = await renderButton()
    await screen.getByRole('button', { name: 'Copy the link' }).click()

    await expect
      .element(screen.getByRole('status'))
      .toHaveTextContent('Could not copy')
  })

  it('says so when the browser offers no clipboard at all', async () => {
    stubClipboard(undefined)

    const screen = await renderButton()
    await screen.getByRole('button', { name: 'Copy the link' }).click()

    await expect
      .element(screen.getByRole('status'))
      .toHaveTextContent('Could not copy')
  })
})
