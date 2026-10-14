import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import { useLocale } from '@/shared/i18n/useLocale'

import DevicesView from '@/features/devices/ui/DevicesView.vue'

const DEVICES = '/v1/devices'
const SAVE_SECOND = '/v1/devices/device-b/credential'
const DUMMY_FIRST = 'dummy-password-for-router-a'
const DUMMY_SECOND = 'dummy-password-for-router-b'
const DEVICE_PAYLOAD = {
  devices: [
    {
      id: 'device-a',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      name: 'Home router',
      address: 'http://10.255.0.1',
      account: 'admin',
      interface: 'Wireguard0',
      credential_stored: false,
      auto_deliver: false,
      deployable: true,
    },
    {
      id: 'device-b',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      name: 'Office router',
      address: 'http://10.255.0.2',
      account: 'admin',
      interface: 'Wireguard0',
      credential_stored: false,
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
      manual_installation_steps: ['Upload the file.'],
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
        address_label: 'Address',
        address_example: 'http://10.255.0.1',
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
    if (input === SAVE_SECOND)
      return Promise.resolve(
        json({
          device: { ...DEVICE_PAYLOAD.devices[1], credential_stored: true },
        }),
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

  it('clears a typed password when switching between connections and saves only one entered for that connection', async () => {
    const fetchMock = installFetch()
    const screen = await render(DevicesView)
    await screen.getByLabelText('Router password').fill(DUMMY_FIRST)

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
      .element(screen.getByLabelText('Router password'))
      .toHaveValue('')
    const save = screen.getByRole('button', {
      name: 'Save password',
      exact: true,
    })
    await expect.element(save).toBeDisabled()
    expect(
      fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST'),
    ).toEqual([])

    // Returning to A is also a departure from B: neither draft can survive.
    await screen.getByLabelText('Router password').fill(DUMMY_SECOND)
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
      .element(screen.getByLabelText('Router password'))
      .toHaveValue('')
    await expect.element(save).toBeDisabled()
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
      .element(screen.getByLabelText('Router password'))
      .toHaveValue('')
    await expect.element(save).toBeDisabled()

    await screen.getByLabelText('Router password').fill(DUMMY_SECOND)
    await save.click()
    const request = fetchMock.mock.calls.find(
      ([input]) => input === SAVE_SECOND,
    )
    expect(request?.[1]?.method).toBe('POST')
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({
      credential: DUMMY_SECOND,
    })
  })

  it('clears the password when adding a connection and does not restore it on cancel', async () => {
    const fetchMock = installFetch()
    const screen = await render(DevicesView)
    await screen.getByLabelText('Router password').fill(DUMMY_FIRST)
    await screen
      .getByRole('button', { name: 'Add a connection', exact: true })
      .click()
    await expect
      .element(screen.getByLabelText('Router password'))
      .not.toBeInTheDocument()
    await screen.getByRole('button', { name: 'Cancel', exact: true }).click()
    await expect
      .element(
        screen.getByRole('heading', { name: 'Home router', exact: true }),
      )
      .toBeVisible()
    await expect
      .element(screen.getByLabelText('Router password'))
      .toHaveValue('')
    await expect
      .element(
        screen.getByRole('button', { name: 'Save password', exact: true }),
      )
      .toBeDisabled()
    expect(
      fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST'),
    ).toEqual([])
  })
})
