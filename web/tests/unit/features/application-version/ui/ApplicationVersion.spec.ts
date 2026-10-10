import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import ApplicationVersion from '@/features/application-version/ui/ApplicationVersion.vue'
import { useLocale } from '@/shared/i18n/useLocale'

describe('ApplicationVersion', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('shows the reported version after loading and keeps it through a language change', async () => {
    const response = Promise.withResolvers<Response>()
    const fetchMock = vi.fn(() => response.promise)
    vi.stubGlobal('fetch', fetchMock)
    useLocale().setLocale('en')

    const screen = await render(ApplicationVersion)
    expect(screen.getByText('Version 0.2.7').query()).toBeNull()
    response.resolve(
      new Response(JSON.stringify({ status: 'ok', version: '0.2.7' }), {
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    await expect.element(screen.getByText('Version 0.2.7')).toBeVisible()
    useLocale().setLocale('ru')
    await expect.element(screen.getByText('Версия 0.2.7')).toBeVisible()
    await screen.rerender({ collapsed: true })
    await expect
      .element(screen.getByText('0.2.7', { exact: true }))
      .toBeVisible()
    await expect
      .element(screen.getByText('0.2.7', { exact: true }))
      .toHaveAttribute('aria-hidden', 'true')
    await expect.element(screen.getByText('Версия 0.2.7')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
