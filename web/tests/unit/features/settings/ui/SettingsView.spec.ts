import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import { useLocale } from '@/shared/i18n/useLocale'

import SettingsView from '@/features/settings/ui/SettingsView.vue'

const SETTINGS = '/v1/settings'
const UPDATE = '/v1/settings/update'
const READ_FAILED = 'The refresh rule could not be read.'
const SAVE_FAILED = 'The refresh rule could not be saved.'

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

// One fetch double per case, so no case inherits another's answers. The record
// of calls is the double's own, which is what the counting below reads.
const installFetch = (
  answer: (input: string) => Promise<Response>,
): ReturnType<typeof vi.fn> => {
  const fetchMock = vi.fn(answer)
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const callsTo = (
  fetchMock: ReturnType<typeof vi.fn>,
  url: string,
): unknown[][] => fetchMock.mock.calls.filter(([input]) => input === url)

const isChecked = (radio: { element: () => Element }): boolean =>
  (radio.element() as HTMLInputElement).checked

describe('SettingsView prerequisite audit', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('does not present Off as confirmed or save after a failed first read', async () => {
    const fetchMock = installFetch(() =>
      Promise.reject(new TypeError('unavailable')),
    )
    const screen = await render(SettingsView)

    // Nothing is confirmed, so no interval is presented as the stored one.
    for (const name of ['Off', 'Daily', 'Weekly']) {
      await expect
        .element(screen.getByRole('radio', { name }))
        .not.toBeChecked()
    }
    await expect.element(screen.getByText(READ_FAILED)).toBeVisible()
    await expect.element(screen.getByText(SAVE_FAILED)).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await expect
      .element(screen.getByRole('button', { name: 'Retry' }))
      .toBeVisible()
  })

  it('retries in the settings section and renders the server value', async () => {
    const fetchMock = installFetch((input: string) => {
      if (input !== SETTINGS)
        return Promise.reject(new Error(`unexpected request ${input}`))
      return Promise.resolve(
        callsTo(fetchMock, SETTINGS).length === 1
          ? json({ error: 'unavailable' }, 503)
          : json({ refresh_interval: 'daily' }),
      )
    })
    const screen = await render(SettingsView)

    await screen.getByRole('button', { name: 'Retry' }).click()

    await expect
      .element(screen.getByRole('radio', { name: 'Daily' }))
      .toBeChecked()
    await expect.element(screen.getByText(READ_FAILED)).not.toBeInTheDocument()
  })

  it.each([true, false])(
    'keeps native selection confirmed through saving (success=%s)',
    async (success) => {
      const held = Promise.withResolvers<undefined>()
      const fetchMock = installFetch((input: string) => {
        if (input === SETTINGS)
          return Promise.resolve(json({ refresh_interval: 'daily' }))
        if (input === UPDATE)
          return held.promise.then(() =>
            success
              ? json({ refresh_interval: 'weekly' })
              : json({ error: 'unavailable' }, 503),
          )
        return Promise.reject(new Error(`unexpected request ${input}`))
      })
      const screen = await render(SettingsView)

      const daily = screen.getByRole('radio', { name: 'Daily' })
      const weekly = screen.getByRole('radio', { name: 'Weekly' })
      await expect.element(daily).toBeChecked()

      // The native radio is the accessible control and is deliberately kept out
      // of sight; the segment beside it is what a pointer lands on. The control
      // moves the moment it is pressed, and the confirmed value is still the
      // stored one until the server answers.
      await screen.getByText('Weekly').click()
      await expect.element(daily).toBeChecked()
      await expect.element(weekly).not.toBeChecked()
      await expect.element(screen.getByText('Saving the rule…')).toBeVisible()
      expect(callsTo(fetchMock, UPDATE)).toHaveLength(1)

      // A second press while one write is in flight starts no second write:
      // the whole choice stops taking input until the first one is answered.
      await expect
        .element(screen.getByRole('radio', { name: 'Off' }))
        .toBeDisabled()
      expect(callsTo(fetchMock, UPDATE)).toHaveLength(1)

      held.resolve(undefined)

      // The answer decides which value is confirmed. On a refusal the stored
      // value is still the confirmed one and the refusal is stated in words.
      await vi.waitFor(() => {
        expect(isChecked(weekly)).toBe(success)
        expect(isChecked(daily)).toBe(!success)
        expect(screen.getByText(SAVE_FAILED).query() === null).toBe(success)
      })
    },
  )
})
