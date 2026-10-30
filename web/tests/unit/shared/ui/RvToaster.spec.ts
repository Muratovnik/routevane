import { afterEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { render } from 'vitest-browser-vue'
import { nextTick } from 'vue'

import {
  NOTICE_ACTION_DURATION,
  NOTICE_DURATION,
  useNotify,
} from '@/shared/model/useNotify'
import RvToaster from '@/shared/ui/RvToaster.vue'

const renderToaster = () =>
  render(RvToaster, {
    props: { closeLabel: 'Close the notification', label: 'Notifications' },
  })

// The stack is the page's, not one toaster's, so every case leaves it empty.
const clear = (): void => {
  const { dismiss, notices } = useNotify()
  for (const notice of [...notices.value]) dismiss(notice.id)
}

describe('RvToaster', () => {
  afterEach(() => {
    clear()
    vi.useRealTimers()
  })

  it('reports a finished act with its title and its consequence', async () => {
    await renderToaster()

    useNotify().notify({
      body: 'It is at the end of the table.',
      title: 'List «Мой тест» created',
    })

    await expect
      .element(page.getByText('List «Мой тест» created'))
      .toBeVisible()
    await expect
      .element(page.getByText('It is at the end of the table.'))
      .toBeVisible()
  })

  it('runs the one step a notice offers, and then goes', async () => {
    await renderToaster()
    const undo = vi.fn()

    useNotify().notify({
      action: { label: 'Undo', run: undo },
      title: 'Profile archived',
    })
    await page.getByRole('button', { name: 'Undo' }).click()

    expect(undo).toHaveBeenCalledTimes(1)
    await expect
      .element(page.getByText('Profile archived'))
      .not.toBeInTheDocument()
  })

  it('closes on request', async () => {
    await renderToaster()

    useNotify().notify({ title: 'Connection forgotten' })
    await page.getByRole('button', { name: 'Close the notification' }).click()

    await expect
      .element(page.getByText('Connection forgotten'))
      .not.toBeInTheDocument()
  })

  // A notice with a step stays long enough to reach for it; one without goes
  // on its own, so the corner does not fill with history.
  it('leaves by itself, later when it offers a step', async () => {
    await renderToaster()
    // Only the close timers are faked, and the clock does not run on its own:
    // a clock that also followed real time would let a slow render close a
    // notice early and blur which duration closed it. What is read is the
    // stack itself, which needs no polling timer.
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] })
    const { notices, notify } = useNotify()
    const titles = (): string[] => notices.value.map((notice) => notice.title)

    notify({ title: 'Plain' })
    notify({ action: { label: 'Open', run: vi.fn() }, title: 'With a step' })
    await nextTick()

    await vi.advanceTimersByTimeAsync(NOTICE_DURATION - 100)
    expect(titles()).toEqual(['Plain', 'With a step'])
    await vi.advanceTimersByTimeAsync(200)
    expect(titles()).toEqual(['With a step'])
    await vi.advanceTimersByTimeAsync(NOTICE_ACTION_DURATION - NOTICE_DURATION)
    expect(titles()).toEqual([])
  })

  it('keeps the newest three when more arrive at once', () => {
    const { notices, notify } = useNotify()

    for (const title of ['one', 'two', 'three', 'four']) notify({ title })

    expect(notices.value.map((notice) => notice.title)).toEqual([
      'two',
      'three',
      'four',
    ])
  })
})
