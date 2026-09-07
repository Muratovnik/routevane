import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'

import { useLocale } from '@/shared/i18n/useLocale'

import DevicesView from '@/features/devices/ui/DevicesView.vue'

const DEVICES = '/v1/devices'
const LISTS = '/v1/lists'
const TARGETS = '/v1/targets'
const REQUIREMENTS = '/v1/deployments/targets'
const MISSING_FIELD = 'Fill in this field'

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const DEVICE_PAYLOAD = {
  devices: [
    {
      id: 'device-1',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      name: 'Home router',
      address: 'http://192.168.1.1',
      account: 'admin',
      auto_deliver: false,
      deployable: true,
    },
  ],
  secret_store_available: true,
}

const CATALOG_PAYLOAD = { lists: [], list_details: [], categories: [] }

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

// One fetch double per case, so no case inherits another's answers, and the
// record of calls is the double's own.
const installFetch = (
  answer: (input: string, init?: RequestInit) => Promise<Response>,
): ReturnType<typeof vi.fn> => {
  const fetchMock = vi.fn(answer)
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const callsTo = (
  fetchMock: ReturnType<typeof vi.fn>,
  url: string,
): unknown[][] => fetchMock.mock.calls.filter(([input]) => input === url)

describe('DevicesView prerequisite audit', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('keeps the form and devices visible while requirements fail, then retries in place', async () => {
    const fetchMock = installFetch((input: string) => {
      if (input === DEVICES) return Promise.resolve(json(DEVICE_PAYLOAD))
      if (input === LISTS) return Promise.resolve(json(CATALOG_PAYLOAD))
      if (input === TARGETS) return Promise.resolve(json(TARGETS_PAYLOAD))
      if (input === REQUIREMENTS) {
        return callsTo(fetchMock, REQUIREMENTS).length === 1
          ? Promise.reject(new TypeError('unavailable'))
          : Promise.resolve(json(REQUIREMENTS_PAYLOAD))
      }
      return Promise.reject(new Error(`unexpected request ${input}`))
    })
    const screen = await render(DevicesView)

    // The registered connection and the form are both still readable: a failed
    // prerequisite read is not a reason to take the screen away.
    await expect.element(screen.getByText('Home router')).toBeVisible()
    await expect
      .element(screen.getByText('Connection requirements unavailable'))
      .toBeVisible()
    await expect
      .element(screen.getByLabelText('Device or application'))
      .toBeVisible()
    // Automatic delivery cannot be offered while its requirements are unknown.
    await expect
      .element(
        screen.getByRole('button', { name: 'Turn on automatic delivery' }),
      )
      .not.toBeInTheDocument()

    await screen.getByLabelText('Device or application').click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()

    // Nothing has been touched yet, so nothing is reported as missing.
    await expect
      .element(screen.getByText(MISSING_FIELD))
      .not.toBeInTheDocument()

    await screen.getByLabelText('Connection name').fill('Draft router')
    await screen.getByLabelText('Device address').fill('http://192.168.1.2')
    const submit = screen.getByRole('button', { name: 'Save' })
    await expect.element(submit).toBeDisabled()

    await screen.getByRole('button', { name: 'Retry' }).click()

    // The retry lands in place: what was typed is still typed.
    await expect
      .element(screen.getByLabelText('Connection name'))
      .toHaveValue('Draft router')
    await expect
      .element(screen.getByLabelText('Device address'))
      .toHaveValue('http://192.168.1.2')
    // With the requirements read, the fields they ask for appear.
    const account = screen.getByLabelText('Router login')
    const deviceInterface = screen.getByLabelText('Interface for routes')
    await expect.element(account).toBeVisible()
    await expect.element(deviceInterface).toBeVisible()

    await deviceInterface.fill('Wireguard0')
    await account.click()
    await userEvent.tab()
    await expect.element(submit).toBeDisabled()
    await expect.element(screen.getByText(MISSING_FIELD)).toBeVisible()

    await account.fill('admin')
    await expect.element(submit).toBeEnabled()
    expect(callsTo(fetchMock, REQUIREMENTS)).toHaveLength(2)
  })

  it('keeps a no-deployer target registerable without inventing credential fields', async () => {
    const fetchMock = installFetch((input: string, init?: RequestInit) => {
      if (input === DEVICES && init?.method === 'POST')
        return Promise.resolve(json({}))
      if (input === DEVICES)
        return Promise.resolve(
          json({ devices: [], secret_store_available: true }),
        )
      if (input === LISTS) return Promise.resolve(json(CATALOG_PAYLOAD))
      if (input === TARGETS) return Promise.resolve(json(TARGETS_PAYLOAD))
      if (input === REQUIREMENTS) return Promise.resolve(json({ targets: [] }))
      return Promise.reject(new Error(`unexpected request ${input}`))
    })
    const screen = await render(DevicesView)

    await screen.getByLabelText('Device or application').click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()
    await screen.getByLabelText('Connection name').fill('Manual router')
    await screen.getByLabelText('Device address').fill('file:///router.conf')

    // A target with no deployer asks for nothing beyond a name and an address,
    // so no credential field is invented for it.
    await expect
      .element(screen.getByLabelText('Router login'))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByLabelText('Interface for routes'))
      .not.toBeInTheDocument()

    const submit = screen.getByRole('button', { name: 'Save' })
    await expect.element(submit).toBeEnabled()
    await submit.click()

    await vi.waitFor(() => {
      expect(
        fetchMock.mock.calls.filter(
          ([input, request]) =>
            input === DEVICES &&
            (request as RequestInit | undefined)?.method === 'POST',
        ),
      ).toHaveLength(1)
    })
  })
})
