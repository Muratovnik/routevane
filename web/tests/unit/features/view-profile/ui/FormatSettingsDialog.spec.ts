import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'
import { userEvent } from 'vitest/browser'

import { useLocale } from '@/shared/i18n/useLocale'
import FormatSettingsDialog from '@/features/view-profile/ui/FormatSettingsDialog.vue'

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status })
const stored = (prefix: string, keepAddresses = false) =>
  json({
    output: {
      id: 'out',
      list_id: 'profile',
      target_id: 'keenetic-dns',
      fqdn_group_prefix: prefix,
      keep_addresses: keepAddresses,
    },
  })

// A format that has the prefix and nothing else, as the dialog stood before the
// keep-addresses setting; a case names what differs.
const renderDialog = (overrides: Record<string, unknown> = {}) =>
  render(FormatSettingsDialog, {
    props: {
      busy: false,
      keepAddresses: false,
      keepAddressesAvailable: false,
      open: true,
      outputId: 'out',
      prefix: '',
      prefixAvailable: true,
      title: 'Keenetic (domain-based)',
      'onUpdate:open': vi.fn(),
      onRebuild: vi.fn(),
      onSaved: vi.fn(),
      ...overrides,
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

  describe('keeping addresses beside domains', () => {
    const SWITCH = 'Keep addresses beside domains'

    it('offers the switch only to a format that can hold it, and the prefix only to the one that names groups', async () => {
      const mikrotik = await renderDialog({
        keepAddressesAvailable: true,
        prefixAvailable: false,
        title: 'MikroTik',
      })
      await expect
        .element(mikrotik.getByRole('switch', { name: SWITCH }))
        .not.toBeChecked()
      await expect
        .element(mikrotik.getByRole('textbox', { name: 'FQDN group prefix' }))
        .not.toBeInTheDocument()
      await mikrotik.unmount()

      const prefixOnly = await renderDialog()
      await expect
        .element(prefixOnly.getByRole('switch'))
        .not.toBeInTheDocument()
      await expect
        .element(prefixOnly.getByRole('textbox', { name: 'FQDN group prefix' }))
        .toBeVisible()
    })

    it('writes the changed choice on Save, then closes and asks the page to read it again', async () => {
      const fetchMock = vi.fn().mockResolvedValueOnce(stored('', true))
      vi.stubGlobal('fetch', fetchMock)
      const screen = await renderDialog({ keepAddressesAvailable: true })
      const keep = screen.getByRole('switch', { name: SWITCH })

      await expect.element(keep).not.toBeChecked()
      await keep.click()
      await expect.element(keep).toBeChecked()
      expect(fetchMock).not.toHaveBeenCalled()
      await screen.getByRole('button', { name: 'Save', exact: true }).click()
      await expect.poll(() => screen.emitted('update:open')).toEqual([[false]])
      expect(screen.emitted('saved')).toEqual([[]])
      expect(screen.emitted('rebuild')).toBeUndefined()
      expect(sentBodies(fetchMock)).toEqual([
        ['/v1/outputs/out/keep-addresses', 'PUT', { keep_addresses: true }],
      ])
    })

    it('writes the choice off when the stored one was on', async () => {
      const fetchMock = vi.fn().mockResolvedValueOnce(stored('', false))
      vi.stubGlobal('fetch', fetchMock)
      const screen = await renderDialog({
        keepAddresses: true,
        keepAddressesAvailable: true,
      })
      const keep = screen.getByRole('switch', { name: SWITCH })

      await expect.element(keep).toBeChecked()
      await keep.click()
      await screen.getByRole('button', { name: 'Save', exact: true }).click()
      await expect.poll(() => screen.emitted('update:open')).toEqual([[false]])
      expect(sentBodies(fetchMock)).toEqual([
        ['/v1/outputs/out/keep-addresses', 'PUT', { keep_addresses: false }],
      ])
    })

    it('writes nothing for a choice put back where it was', async () => {
      const fetchMock = vi.fn()
      vi.stubGlobal('fetch', fetchMock)
      const screen = await renderDialog({
        keepAddresses: true,
        keepAddressesAvailable: true,
      })
      const keep = screen.getByRole('switch', { name: SWITCH })

      await keep.click()
      await keep.click()
      await expect.element(keep).toBeChecked()
      await screen.getByRole('button', { name: 'Save', exact: true }).click()
      await expect.poll(() => screen.emitted('update:open')).toEqual([[false]])
      expect(fetchMock).not.toHaveBeenCalled()
      expect(screen.emitted('saved')).toBeUndefined()
    })

    it('keeps a refused choice on screen beside the reason and sends it again on the next Save', async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce(json({ error: 'invalid_keep_addresses' }, 400))
        .mockResolvedValueOnce(stored('', true))
      vi.stubGlobal('fetch', fetchMock)
      const screen = await renderDialog({ keepAddressesAvailable: true })
      const keep = screen.getByRole('switch', { name: SWITCH })
      const save = screen.getByRole('button', { name: 'Save', exact: true })

      await keep.click()
      await save.click()
      await expect
        .element(screen.getByText('The setting could not be saved'))
        .toBeVisible()
      await expect.element(keep).toBeChecked()
      expect(screen.emitted('update:open')).toBeUndefined()
      expect(screen.emitted('saved')).toBeUndefined()
      await save.click()
      await expect.poll(() => screen.emitted('update:open')).toEqual([[false]])
      expect(sentBodies(fetchMock)).toEqual([
        ['/v1/outputs/out/keep-addresses', 'PUT', { keep_addresses: true }],
        ['/v1/outputs/out/keep-addresses', 'PUT', { keep_addresses: true }],
      ])
    })

    it('writes a changed prefix and a changed choice with one Save, and resends only what was refused', async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce(stored('home'))
        .mockResolvedValueOnce(json({ error: 'invalid_keep_addresses' }, 400))
        .mockResolvedValueOnce(stored('home', true))
      vi.stubGlobal('fetch', fetchMock)
      const screen = await renderDialog({ keepAddressesAvailable: true })
      const save = screen.getByRole('button', { name: 'Save', exact: true })

      await screen
        .getByRole('textbox', { name: 'FQDN group prefix' })
        .fill('home')
      await screen.getByRole('switch', { name: SWITCH }).click()
      await save.click()
      await expect
        .element(screen.getByText('The setting could not be saved'))
        .toBeVisible()
      // The prefix is stored, so the page reads it again; the dialog stays.
      expect(screen.emitted('saved')).toEqual([[]])
      expect(screen.emitted('update:open')).toBeUndefined()
      await save.click()
      await expect.poll(() => screen.emitted('update:open')).toEqual([[false]])
      expect(sentBodies(fetchMock)).toEqual([
        ['/v1/outputs/out/fqdn-prefix', 'PUT', { fqdn_group_prefix: 'home' }],
        ['/v1/outputs/out/keep-addresses', 'PUT', { keep_addresses: true }],
        ['/v1/outputs/out/keep-addresses', 'PUT', { keep_addresses: true }],
      ])
    })

    it('saves a changed choice before asking for the rebuild it promises', async () => {
      const fetchMock = vi.fn().mockResolvedValueOnce(stored('', true))
      vi.stubGlobal('fetch', fetchMock)
      const screen = await renderDialog({ keepAddressesAvailable: true })

      await screen.getByRole('switch', { name: SWITCH }).click()
      await userEvent.click(screen.getByRole('button', { name: 'Rebuild now' }))
      await expect.poll(() => screen.emitted('rebuild')).toEqual([['out']])
      expect(screen.emitted('saved')).toEqual([[]])
      expect(sentBodies(fetchMock)).toEqual([
        ['/v1/outputs/out/keep-addresses', 'PUT', { keep_addresses: true }],
      ])
    })

    it('states the choice in Russian', async () => {
      useLocale().setLocale('ru')
      const screen = await renderDialog({ keepAddressesAvailable: true })

      await expect
        .element(
          screen.getByRole('switch', {
            name: 'Оставлять адреса рядом с доменами',
          }),
        )
        .toBeVisible()
    })
  })
})
