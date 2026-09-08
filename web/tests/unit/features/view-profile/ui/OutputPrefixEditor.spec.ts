import { afterEach, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import { useLocale } from '@/shared/i18n/useLocale'
import OutputPrefixEditor from '@/features/view-profile/ui/OutputPrefixEditor.vue'

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status })
afterEach(() => vi.unstubAllGlobals())

it('validates a prefix, preserves a refused draft, and stores the default without rebuilding', async () => {
  useLocale().setLocale('en')
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(json({ error: 'invalid_fqdn_prefix' }, 400))
    .mockResolvedValueOnce(
      json({
        output: {
          id: 'out',
          list_id: 'profile',
          target_id: 'keenetic-dns',
          fqdn_group_prefix: 'my-vpn',
        },
      }),
    )
    .mockResolvedValueOnce(
      json({
        output: {
          id: 'out',
          list_id: 'profile',
          target_id: 'keenetic-dns',
          fqdn_group_prefix: '',
        },
      }),
    )
  vi.stubGlobal('fetch', fetchMock)
  const screen = await render(OutputPrefixEditor, {
    props: { outputId: 'out', prefix: '', disabled: false },
  })
  const field = screen.getByRole('textbox', { name: 'FQDN group prefix' })
  const save = screen.getByRole('button', { name: 'Save prefix' })
  await field.fill('-invalid')
  await expect.element(save).toBeDisabled()
  await expect.element(field).toHaveAttribute('aria-invalid', 'true')
  await field.fill('1-invalid')
  await expect.element(save).toBeDisabled()
  await field.fill('my-vpn')
  await save.click()
  await expect
    .element(screen.getByText('The prefix could not be saved'))
    .toBeVisible()
  await expect.element(field).toHaveValue('my-vpn')
  await screen.getByRole('button', { name: 'Retry' }).click()
  await expect
    .element(screen.getByText('Prefix saved for the next build'))
    .toBeVisible()
  await field.fill('')
  await save.click()
  await expect.element(save).toBeDisabled()
  expect(
    fetchMock.mock.calls.map(([url, init]) => [
      url,
      init.method,
      JSON.parse(init.body),
    ]),
  ).toEqual([
    ['/v1/outputs/out/fqdn-prefix', 'PUT', { fqdn_group_prefix: 'my-vpn' }],
    ['/v1/outputs/out/fqdn-prefix', 'PUT', { fqdn_group_prefix: 'my-vpn' }],
    ['/v1/outputs/out/fqdn-prefix', 'PUT', { fqdn_group_prefix: '' }],
  ])
})
