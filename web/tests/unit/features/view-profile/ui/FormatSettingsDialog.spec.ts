import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'
import { userEvent } from 'vitest/browser'

import { useLocale } from '@/shared/i18n/useLocale'
import FormatSettingsDialog from '@/features/view-profile/ui/FormatSettingsDialog.vue'

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status })
const stored = (prefix: string) =>
  json({
    output: {
      id: 'out',
      list_id: 'profile',
      target_id: 'keenetic-dns',
      fqdn_group_prefix: prefix,
    },
  })

const renderDialog = () =>
  render(FormatSettingsDialog, {
    props: {
      busy: false,
      open: true,
      outputId: 'out',
      prefix: '',
      title: 'Keenetic (domain-based)',
      'onUpdate:open': vi.fn(),
      onRebuild: vi.fn(),
    },
  })

const sentBodies = (fetchMock: ReturnType<typeof vi.fn>) =>
  fetchMock.mock.calls.map(([url, init]) => [
    url,
    init.method,
    JSON.parse(init.body),
  ])

describe('FormatSettingsDialog', () => {
  beforeEach(() => useLocale().setLocale('en'))
  afterEach(() => vi.unstubAllGlobals())

  it('keeps Save available and names the rule beside the field only when a save is refused for it', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const screen = await renderDialog()
    const field = screen.getByRole('textbox', { name: 'FQDN group prefix' })
    const save = screen.getByRole('button', { name: 'Save', exact: true })

    await expect.element(field).toHaveAttribute('placeholder', 'routevane')
    await expect
      .element(screen.getByText('Applies at the next build'))
      .toBeVisible()
    await field.fill('-invalid')
    // Typing states nothing yet; the rule is the answer to a refused save.
    await expect.element(field).not.toHaveAttribute('aria-invalid', 'true')
    await expect.element(save).toBeEnabled()
    await save.click()
    await expect
      .element(
        screen.getByText('Use up to 24 lowercase letters', { exact: false }),
      )
      .toBeVisible()
    await expect.element(field).toHaveAttribute('aria-invalid', 'true')
    await expect.element(field).toHaveFocus()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('preserves a refused draft, closes on a stored prefix and never rebuilds on Save', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(json({ error: 'invalid_fqdn_prefix' }, 400))
      .mockResolvedValueOnce(stored('my-vpn'))
    vi.stubGlobal('fetch', fetchMock)
    const screen = await renderDialog()
    const field = screen.getByRole('textbox', { name: 'FQDN group prefix' })
    const save = screen.getByRole('button', { name: 'Save', exact: true })

    await field.fill('my-vpn')
    await save.click()
    await expect
      .element(screen.getByText('The prefix could not be saved'))
      .toBeVisible()
    await expect.element(field).toHaveValue('my-vpn')
    await save.click()
    await expect.poll(() => screen.emitted('update:open')).toEqual([[false]])
    expect(screen.emitted('rebuild')).toBeUndefined()
    expect(sentBodies(fetchMock)).toEqual([
      ['/v1/outputs/out/fqdn-prefix', 'PUT', { fqdn_group_prefix: 'my-vpn' }],
      ['/v1/outputs/out/fqdn-prefix', 'PUT', { fqdn_group_prefix: 'my-vpn' }],
    ])
  })

  it('saves a changed draft before asking for the rebuild it promises', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(stored('home'))
    vi.stubGlobal('fetch', fetchMock)
    const screen = await renderDialog()

    await screen
      .getByRole('textbox', { name: 'FQDN group prefix' })
      .fill('home')
    await userEvent.click(screen.getByRole('button', { name: 'Rebuild now' }))
    await expect.poll(() => screen.emitted('rebuild')).toEqual([['out']])
    expect(screen.emitted('update:open')).toEqual([[false]])
    expect(sentBodies(fetchMock)).toEqual([
      ['/v1/outputs/out/fqdn-prefix', 'PUT', { fqdn_group_prefix: 'home' }],
    ])
  })
})
