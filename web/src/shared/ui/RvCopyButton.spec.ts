import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import RvCopyButton from '@/shared/ui/RvCopyButton.vue'

const original = Object.getOwnPropertyDescriptor(navigator, 'clipboard')

const stubClipboard = (value: unknown): void => {
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value })
}

const mountButton = () =>
  mount(RvCopyButton, {
    props: {
      copiedLabel: 'Copied',
      failedLabel: 'Could not copy',
      label: 'Copy the link',
      value: 'https://127.0.0.1/s/token',
    },
  })

const press = async (
  wrapper: ReturnType<typeof mountButton>,
): Promise<string> => {
  await wrapper.get('button').trigger('click')
  await flushPromises()
  return wrapper.get('.rv-copy__outcome').text()
}

describe('RvCopyButton', () => {
  afterEach(() => {
    vi.useRealTimers()
    if (original === undefined) Reflect.deleteProperty(navigator, 'clipboard')
    else Object.defineProperty(navigator, 'clipboard', original)
  })

  it('confirms a write the clipboard accepted, and keeps saying so', async () => {
    vi.useFakeTimers()
    const writeText = vi.fn(async () => {})
    stubClipboard({ writeText })

    const wrapper = mountButton()
    expect(await press(wrapper)).toBe('Copied')
    expect(writeText).toHaveBeenCalledWith('https://127.0.0.1/s/token')

    // The confirmation is not on a timer: a secret copied a minute ago is still
    // a secret on the clipboard, and a line that erased itself would leave the
    // operator guessing whether to press again.
    vi.advanceTimersByTime(60_000)
    await flushPromises()
    expect(wrapper.get('.rv-copy__outcome').text()).toBe('Copied')
    wrapper.unmount()
  })

  // A confirmation is a fact that happened. A clipboard that refused the write
  // says so, and never borrows the words of one that accepted it.
  it('reports a refusal instead of confirming it', async () => {
    stubClipboard({
      writeText: vi.fn(async () => {
        throw new Error('NotAllowedError')
      }),
    })

    const wrapper = mountButton()
    expect(await press(wrapper)).toBe('Could not copy')
    wrapper.unmount()
  })

  it('says so when the browser offers no clipboard at all', async () => {
    stubClipboard(undefined)

    const wrapper = mountButton()
    expect(await press(wrapper)).toBe('Could not copy')
    wrapper.unmount()
  })
})
