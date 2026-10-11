import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import { useLocale } from '@/shared/i18n/useLocale'

import DevicesView from '@/features/devices/ui/DevicesView.vue'

const DEVICES = '/v1/devices'
const ENABLE_SECOND = '/v1/devices/device-b/auto-delivery'
const DUMMY_FIRST = 'dummy-password-for-router-a'
const DUMMY_SECOND = 'dummy-password-for-router-b'
const DEVICE_PAYLOAD = {
  devices: [
    {
      id: 'device-a',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      name: 'Home router',
      address: 'http://192.168.1.1',
      account: 'admin',
      interface: 'Wireguard0',
      auto_deliver: false,
      deployable: true,
    },
    {
      id: 'device-b',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      name: 'Office router',
      address: 'http://192.168.2.1',
      account: 'admin',
      interface: 'Wireguard0',
      auto_deliver: false,
      deployable: true,
    },
  ],
  secret_store_available: true,
}
const TARGETS_PAYLOAD = {
  targets: [
    {
      id: 'keenetic',
      title: 'Keenetic',
      kind: 'router',
      format_key: 'keenetic-bat-ipv4-v1',
      renderer_id: 'keenetic-route-bat',
      file_extension: 'bat',
      manual_installation_hint: 'Upload the file.',
    },
  ],
}
const REQUIREMENTS_PAYLOAD = {
  targets: [
    {
      target_id: 'keenetic',
      title: 'Keenetic',
      deployer_id: 'keenetic',
      requirements: {
        address_label: 'Device address',
        address_example: 'http://192.168.1.1',
        needs_credential: true,
        needs_interface: true,
        interface_label: 'Device interface',
      },
    },
  ],
}

const json = (payload: unknown): Response =>
  new Response(JSON.stringify(payload), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })

const installFetch = () => {
  const fetchMock = vi.fn((input: string, init?: RequestInit) => {
    if (input === ENABLE_SECOND)
      return Promise.resolve(
        json({ device: { ...DEVICE_PAYLOAD.devices[1], auto_deliver: true } }),
      )
    if (input === DEVICES) return Promise.resolve(json(DEVICE_PAYLOAD))
    if (input === '/v1/lists')
      return Promise.resolve(
        json({ lists: [], list_details: [], categories: [] }),
      )
    if (input === '/v1/targets') return Promise.resolve(json(TARGETS_PAYLOAD))
    if (input === '/v1/deployments/targets')
      return Promise.resolve(json(REQUIREMENTS_PAYLOAD))
    return Promise.reject(
      new Error(`unexpected request ${input} ${init?.method}`),
    )
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('DevicesView credential ownership', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('clears passwords when switching between connections and sends only a newly entered password', async () => {
    const fetchMock = installFetch()
    const screen = await render(DevicesView)
    await screen.getByLabelText('Password', { exact: true }).fill(DUMMY_FIRST)

    await screen
      .getByRole('button', {
        name: 'Configure connection Office router',
        exact: true,
      })
      .click()
    await expect
      .element(
        screen.getByRole('heading', { name: 'Office router', exact: true }),
      )
      .toBeVisible()
    await expect
      .element(screen.getByLabelText('Password', { exact: true }))
      .toHaveValue('')
    const enable = screen.getByRole('button', {
      name: 'Turn on automatic delivery',
      exact: true,
    })
    await expect.element(enable).toBeDisabled()
    expect(
      fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST'),
    ).toEqual([])

    // Returning to A is also a departure from B: neither draft can survive.
    await screen.getByLabelText('Password', { exact: true }).fill(DUMMY_SECOND)
    await screen
      .getByRole('button', {
        name: 'Configure connection Home router',
        exact: true,
      })
      .click()
    await expect
      .element(
        screen.getByRole('heading', { name: 'Home router', exact: true }),
      )
      .toBeVisible()
    await expect
      .element(screen.getByLabelText('Password', { exact: true }))
      .toHaveValue('')
    await expect.element(enable).toBeDisabled()
    expect(
      fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST'),
    ).toEqual([])

    await screen
      .getByRole('button', {
        name: 'Configure connection Office router',
        exact: true,
      })
      .click()
    await expect
      .element(screen.getByLabelText('Password', { exact: true }))
      .toHaveValue('')
    await expect.element(enable).toBeDisabled()

    await screen.getByLabelText('Password', { exact: true }).fill(DUMMY_SECOND)
    await enable.click()
    const request = fetchMock.mock.calls.find(
      ([input]) => input === ENABLE_SECOND,
    )
    expect(request?.[1]?.method).toBe('POST')
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({
      enabled: true,
      credential: DUMMY_SECOND,
    })
  })

  it('clears the password when adding a connection and does not restore it on cancel', async () => {
    const fetchMock = installFetch()
    const screen = await render(DevicesView)
    await screen.getByLabelText('Password', { exact: true }).fill(DUMMY_FIRST)
    await screen
      .getByRole('button', { name: 'Add a connection', exact: true })
      .click()
    await expect
      .element(screen.getByLabelText('Password', { exact: true }))
      .not.toBeInTheDocument()
    await screen.getByRole('button', { name: 'Cancel', exact: true }).click()
    await expect
      .element(
        screen.getByRole('heading', { name: 'Home router', exact: true }),
      )
      .toBeVisible()
    await expect
      .element(screen.getByLabelText('Password', { exact: true }))
      .toHaveValue('')
    await expect
      .element(
        screen.getByRole('button', {
          name: 'Turn on automatic delivery',
          exact: true,
        }),
      )
      .toBeDisabled()
    expect(
      fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST'),
    ).toEqual([])
  })
})
