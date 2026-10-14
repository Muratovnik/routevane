import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page, userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'

import { useLocale } from '@/shared/i18n/useLocale'

import DevicesView from '@/features/devices/ui/DevicesView.vue'

const DEVICES = '/v1/devices'
const LISTS = '/v1/lists'
const TARGETS = '/v1/targets'
const REQUIREMENTS = '/v1/deployments/targets'
const MISSING_FIELD = 'Fill in this field'
const ADDRESS_EXPECTED = 'Expected an address like http://10.255.0.1.'
const ADDRESS_RULE =
  'Start with http:// or https://, then the router’s local IP address'

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
      address: 'http://10.255.0.1',
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
        address_label: 'Device address',
        address_example: 'http://10.255.0.1',
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

describe('DevicesView router form labels', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // One subject for what a router asks for, so the form does not read
  // «Address» above «Router login» (the saved password is «Router password»).
  it('names the address and the login of a router with the same subject', async () => {
    installFetch((input: string) => {
      if (input === DEVICES)
        return Promise.resolve(
          json({ devices: [], secret_store_available: true }),
        )
      if (input === LISTS) return Promise.resolve(json(CATALOG_PAYLOAD))
      if (input === TARGETS) return Promise.resolve(json(TARGETS_PAYLOAD))
      if (input === REQUIREMENTS)
        return Promise.resolve(json(REQUIREMENTS_PAYLOAD))
      return Promise.reject(new Error(`unexpected request ${input}`))
    })
    const screen = await render(DevicesView)

    await screen
      .getByRole('button', { name: 'Add a connection', exact: true })
      .click()
    await screen.getByLabelText('Format', { exact: true }).click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()

    await expect
      .element(screen.getByLabelText('Router address', { exact: true }))
      .toBeVisible()
    await expect
      .element(screen.getByLabelText('Router login', { exact: true }))
      .toBeVisible()
    await expect
      .element(screen.getByLabelText('Address', { exact: true }))
      .not.toBeInTheDocument()
  })
})

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
    await expect
      .element(screen.getByRole('heading', { name: 'Home router' }))
      .toBeVisible()
    await expect
      .element(screen.getByText('Connection requirements unavailable'))
      .toBeVisible()
    await screen
      .getByRole('button', { name: 'Add a connection', exact: true })
      .click()
    await expect
      .element(screen.getByLabelText('Format', { exact: true }))
      .toBeVisible()
    // Automatic delivery cannot be offered while its requirements are unknown.
    await expect
      .element(
        screen.getByRole('switch', { name: 'Send new versions automatically' }),
      )
      .not.toBeInTheDocument()

    await screen.getByLabelText('Format', { exact: true }).click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()

    // Nothing has been touched yet, so nothing is reported as missing.
    await expect
      .element(screen.getByText(MISSING_FIELD))
      .not.toBeInTheDocument()

    await screen.getByLabelText('Connection name').fill('Draft router')
    await screen
      .getByLabelText('Router address', { exact: true })
      .fill('http://10.255.0.2')
    const submit = screen.getByRole('button', { name: 'Save' })
    await expect.element(submit).toBeDisabled()

    await screen.getByRole('button', { name: 'Retry' }).click()

    // The retry lands in place: what was typed is still typed.
    await expect
      .element(screen.getByLabelText('Connection name'))
      .toHaveValue('Draft router')
    await expect
      .element(screen.getByLabelText('Router address', { exact: true }))
      .toHaveValue('http://10.255.0.2')
    // With the requirements read, the fields they ask for appear.
    const account = screen.getByLabelText('Router login')
    const deviceInterface = screen.getByLabelText('Interface for routes')
    await expect.element(account).toBeVisible()
    await expect.element(deviceInterface).toBeVisible()

    await deviceInterface.fill('Wireguard0')
    await account.click()
    await userEvent.tab()
    await expect.element(screen.getByText(MISSING_FIELD)).toBeVisible()

    // Submitting stays possible: pressing it states what is missing and moves
    // focus there instead of staying silent, and sends nothing.
    await expect.element(submit).toBeEnabled()
    await submit.click()
    await expect.element(account).toHaveFocus()
    expect(
      fetchMock.mock.calls.filter(([, request]) => request?.method === 'POST'),
    ).toHaveLength(0)

    await account.fill('admin')
    await expect.element(submit).toBeEnabled()
    expect(callsTo(fetchMock, REQUIREMENTS)).toHaveLength(2)
  })

  it('keeps a no-deployer target registerable without inventing credential fields', async () => {
    const fetchMock = installFetch((input: string, init?: RequestInit) => {
      if (input === DEVICES && init?.method === 'POST')
        return Promise.resolve(json({ device: { id: 'manual-router' } }))
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

    // An empty registry states that it holds nothing and offers the one act
    // that changes it, instead of opening a form nobody asked for.
    const add = screen.getByRole('button', {
      name: 'Add a connection',
      exact: true,
    })
    await expect
      .element(screen.getByRole('heading', { name: 'No connections yet' }))
      .toBeVisible()
    await expect
      .element(
        screen.getByRole('region', { name: 'Add a connection', exact: true }),
      )
      .not.toBeInTheDocument()

    await add.click()
    await expect
      .element(
        screen.getByRole('region', { name: 'Add a connection', exact: true }),
      )
      .toBeVisible()
    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    // The form leaves through its own cancel, and what it replaced is back.
    await screen.getByRole('button', { name: 'Cancel', exact: true }).click()
    await expect
      .element(screen.getByRole('heading', { name: 'No connections yet' }))
      .toHaveFocus()

    await add.click()
    await screen.getByLabelText('Format', { exact: true }).click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()
    await screen.getByLabelText('Connection name').fill('Manual router')
    await screen
      .getByLabelText('Router address', { exact: true })
      .fill('file:///router.conf')

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

  /**
   * The service stores this address and contacts it later, without the operator
   * present, so it refuses one whose scheme is left to be guessed. The form used
   * to take `10.255.0.1`, send it, and report only that the request had been
   * refused — naming no field and no reason.
   */
  it('states the address shape a stored connection needs before it is sent', async () => {
    const fetchMock = installFetch((input: string) => {
      if (input === DEVICES)
        return Promise.resolve(
          json({ devices: [], secret_store_available: true }),
        )
      if (input === LISTS) return Promise.resolve(json(CATALOG_PAYLOAD))
      if (input === TARGETS) return Promise.resolve(json(TARGETS_PAYLOAD))
      if (input === REQUIREMENTS)
        return Promise.resolve(json(REQUIREMENTS_PAYLOAD))
      return Promise.reject(new Error(`unexpected request ${input}`))
    })
    const screen = await render(DevicesView)
    await screen
      .getByRole('button', { name: 'Add a connection', exact: true })
      .click()
    await screen.getByLabelText('Format', { exact: true }).click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()

    // The rule is explained behind the informer beside the label, not under the
    // field, and the shape of a value is the placeholder: the label, the
    // informer and the placeholder carry the meaning, and only an error stands
    // under the field. The rule is still the field's description, so a reader
    // hears it without opening the informer.
    const field = screen.getByLabelText('Router address', { exact: true })
    await expect
      .element(field)
      .toHaveAccessibleDescription(expect.stringContaining(ADDRESS_RULE))
    // The informer's panel is portalled to the document, outside the view.
    await expect.element(page.getByRole('dialog')).not.toBeInTheDocument()
    await expect
      .element(field)
      .toHaveAttribute('placeholder', 'http://10.255.0.1')
    await expect
      .element(screen.getByLabelText('Interface for routes', { exact: true }))
      .toHaveAttribute('placeholder', 'Wireguard0')
    await screen
      .getByRole('button', { name: 'What the address field takes' })
      .click()
    await expect
      .element(page.getByRole('dialog'))
      .toHaveTextContent(`${ADDRESS_RULE}.`)
    await userEvent.keyboard('{Escape}')

    await screen.getByLabelText('Connection name').fill('Home router')
    await screen.getByLabelText('Router login').fill('admin')
    await screen.getByLabelText('Interface for routes').fill('Wireguard0')
    const address = screen.getByLabelText('Router address', { exact: true })
    await address.fill('10.255.0.1')
    // Leaving the field is what asks it to answer, as with a field left empty.
    await screen.getByLabelText('Connection name').click()

    await expect.element(screen.getByText(ADDRESS_EXPECTED)).toBeVisible()
    await screen.getByRole('button', { name: 'Save' }).click()
    await expect.element(address).toHaveFocus()

    await address.fill('http://10.255.0.1')
    await expect
      .element(screen.getByText(ADDRESS_EXPECTED))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: 'Save' }))
      .toBeEnabled()

    // Nothing the service would refuse was ever sent.
    expect(
      fetchMock.mock.calls.filter(
        ([input, request]) =>
          input === DEVICES &&
          (request as RequestInit | undefined)?.method === 'POST',
      ),
    ).toHaveLength(0)
  })

  // Which networks a router may live on is the service's policy, so the form
  // sends an address whose shape is right and states the service's refusal at
  // the address field, in the words of this format's own kind of address.
  it('states the service refusal of an address at the address field when registering', async () => {
    const fetchMock = installFetch((input: string, init?: RequestInit) => {
      if (input === DEVICES && init?.method === 'POST')
        return Promise.resolve(
          json({ error: 'address_rejected', field: 'address' }, 422),
        )
      if (input === DEVICES)
        return Promise.resolve(
          json({ devices: [], secret_store_available: true }),
        )
      if (input === LISTS) return Promise.resolve(json(CATALOG_PAYLOAD))
      if (input === TARGETS) return Promise.resolve(json(TARGETS_PAYLOAD))
      if (input === REQUIREMENTS)
        return Promise.resolve(json(REQUIREMENTS_PAYLOAD))
      return Promise.reject(new Error(`unexpected request ${input}`))
    })
    const screen = await render(DevicesView)
    await screen
      .getByRole('button', { name: 'Add a connection', exact: true })
      .click()
    await screen.getByLabelText('Format', { exact: true }).click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()
    await screen.getByLabelText('Connection name').fill('Home router')
    await screen.getByLabelText('Router login').fill('admin')
    await screen.getByLabelText('Interface for routes').fill('Wireguard0')
    const address = screen.getByLabelText('Router address', { exact: true })
    await address.fill('http://8.8.8.8')

    await screen.getByRole('button', { name: 'Save' }).click()

    const refusal =
      'Use a local network IP address, not a host name or a public address.'
    await expect.element(screen.getByText(refusal)).toBeVisible()
    await expect.element(address).toHaveFocus()
    await expect
      .element(screen.getByText('That did not go through'))
      .not.toBeInTheDocument()
    // The form sent the address: the shape was right and nothing here judged it.
    expect(
      fetchMock.mock.calls.filter(
        ([input, request]) =>
          input === DEVICES &&
          (request as RequestInit | undefined)?.method === 'POST',
      ),
    ).toHaveLength(1)

    await address.fill('http://10.255.0.2')
    await expect.element(screen.getByText(refusal)).not.toBeInTheDocument()
  })

  it('keeps a draft across list selection, and cancel reopens the replaced connection without it', async () => {
    const fetchMock = installFetch((input: string) => {
      if (input === DEVICES) return Promise.resolve(json(DEVICE_PAYLOAD))
      if (input === LISTS) return Promise.resolve(json(CATALOG_PAYLOAD))
      if (input === TARGETS) return Promise.resolve(json(TARGETS_PAYLOAD))
      if (input === REQUIREMENTS)
        return Promise.resolve(json(REQUIREMENTS_PAYLOAD))
      return Promise.reject(new Error(`unexpected request ${input}`))
    })
    const screen = await render(DevicesView)
    const connection = screen.getByRole('button', {
      name: 'Configure connection Home router',
    })
    await connection.click()
    await expect
      .element(screen.getByRole('heading', { name: 'Home router' }))
      .toHaveFocus()
    await screen.getByLabelText('Router password').fill('fixture-password')
    await screen
      .getByRole('button', { name: 'Add a connection', exact: true })
      .click()
    await screen.getByLabelText('Format', { exact: true }).click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()
    await screen.getByLabelText('Connection name').fill('Unfinished router')
    await connection.click()
    await expect
      .element(screen.getByLabelText('Router password'))
      .toHaveValue('')
    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    const add = screen.getByRole('button', {
      name: 'Add a connection',
      exact: true,
    })
    await add.click()
    await expect
      .element(screen.getByLabelText('Connection name'))
      .toHaveValue('Unfinished router')
    // The header action stays in place while the form is open, and the form
    // leaves through its own cancel: the connection it replaced is the work
    // area again, with focus on its heading, and nothing was written.
    await expect.element(add).toBeDisabled()
    await screen.getByRole('button', { name: 'Cancel', exact: true }).click()
    await expect
      .element(screen.getByRole('heading', { name: 'Home router' }))
      .toHaveFocus()
    await expect
      .element(screen.getByRole('region', { name: 'Home router', exact: true }))
      .toBeVisible()
    await expect
      .element(
        screen.getByRole('region', { name: 'Add a connection', exact: true }),
      )
      .not.toBeInTheDocument()
    await expect.element(add).toBeEnabled()
    // Cancel discarded the draft: the next form starts from the target choice.
    await add.click()
    await expect
      .element(screen.getByLabelText('Connection name'))
      .not.toBeInTheDocument()
    await screen.getByLabelText('Format', { exact: true }).click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()
    await expect
      .element(screen.getByLabelText('Connection name'))
      .toHaveValue('')
    expect(
      fetchMock.mock.calls.every(([, request]) => request?.method !== 'POST'),
    ).toBe(true)
  })
})

it.each([
  { remaining: [], nextHeading: 'No connections yet' },
  {
    remaining: [
      {
        ...DEVICE_PAYLOAD.devices[0],
        id: 'device-2',
        name: 'Office router',
        address: 'http://10.255.0.2',
      },
    ],
    nextHeading: 'Office router',
  },
])(
  'forgets the confirmed connection and opens $nextHeading while its registry read recovers',
  async ({ remaining, nextHeading }) => {
    useLocale().setLocale('en')
    const fetchMock = installFetch(async (input, init) => {
      if (input === '/v1/devices/device-1/forget')
        return json({ forgotten: 'device-1' })
      if (input === DEVICES) {
        const reads = callsTo(fetchMock, DEVICES).length
        if (reads === 2) return json({ error: 'unavailable' }, 503)
        return json({
          devices:
            reads === 1 ? [...DEVICE_PAYLOAD.devices, ...remaining] : remaining,
          secret_store_available: true,
        })
      }
      if (input === LISTS) return json(CATALOG_PAYLOAD)
      if (input === TARGETS) return json(TARGETS_PAYLOAD)
      if (input === REQUIREMENTS) return json(REQUIREMENTS_PAYLOAD)
      throw new Error(`unexpected request ${input} ${init?.method}`)
    })
    const screen = await render(DevicesView)
    await screen
      .getByRole('button', { name: 'Add a connection', exact: true })
      .click()
    await screen.getByLabelText('Format', { exact: true }).click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()
    await screen.getByLabelText('Connection name').fill('Unsaved connection')
    await screen
      .getByRole('button', {
        name: 'Configure connection Home router',
        exact: true,
      })
      .click()
    // Forgetting is the connection's own secondary action and it is confirmed:
    // the menu offers it, the dialog runs it.
    await screen
      .getByRole('button', { name: 'Actions for connection Home router' })
      .click()
    await screen
      .getByRole('menuitem', { name: 'Forget this connection' })
      .click()
    await expect
      .element(
        screen.getByRole('button', {
          name: 'Forget this connection',
          exact: true,
        }),
      )
      .toHaveClass('rv-button--danger')
    await screen
      .getByRole('button', { name: 'Forget this connection', exact: true })
      .click()
    await expect
      .element(screen.getByText('Connections could not be refreshed'))
      .toBeVisible()
    await expect
      .element(screen.getByRole('heading', { name: 'Home router' }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('heading', { name: nextHeading, exact: true }))
      .toBeVisible()
    await screen.getByRole('button', { name: 'Retry', exact: true }).click()
    await expect
      .element(screen.getByRole('heading', { name: 'Home router' }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByText('Connections could not be refreshed'))
      .not.toBeInTheDocument()
    // Neither selection recovery nor the registry retry discards a draft for
    // a different connection.
    await expect
      .element(screen.getByRole('heading', { name: nextHeading, exact: true }))
      .toBeVisible()
    await screen
      .getByRole('button', { name: 'Add a connection', exact: true })
      .click()
    await expect
      .element(screen.getByLabelText('Connection name'))
      .toHaveValue('Unsaved connection')
    expect(callsTo(fetchMock, '/v1/devices/device-1/forget')).toHaveLength(1)
    expect(callsTo(fetchMock, DEVICES)).toHaveLength(3)
    vi.unstubAllGlobals()
  },
)

it.each(['refused', 'unanswered'] as const)(
  'keeps the connection when forgetting is %s',
  async (outcome) => {
    useLocale().setLocale('en')
    const endpoint = '/v1/devices/device-1/forget'
    const fetchMock = installFetch(async (input, init) => {
      if (input === endpoint) {
        if (outcome === 'unanswered') throw new TypeError('connection lost')
        return json({ error: 'unavailable' }, 503)
      }
      if (input === DEVICES) return json(DEVICE_PAYLOAD)
      if (input === LISTS) return json(CATALOG_PAYLOAD)
      if (input === TARGETS) return json(TARGETS_PAYLOAD)
      if (input === REQUIREMENTS) return json(REQUIREMENTS_PAYLOAD)
      if (input === '/health') return json({ status: 'ok', version: 'dev' })
      throw new Error(`unexpected request ${input} ${init?.method}`)
    })
    const screen = await render(DevicesView)
    await screen
      .getByRole('button', { name: 'Actions for connection Home router' })
      .click()
    await screen
      .getByRole('menuitem', { name: 'Forget this connection' })
      .click()
    await screen
      .getByRole('button', { name: 'Forget this connection', exact: true })
      .click()

    await expect
      .element(
        screen.getByRole('heading', { name: 'Home router', exact: true }),
      )
      .toBeVisible()
    await expect
      .element(screen.getByText('That did not go through'))
      .toBeVisible()
    expect(callsTo(fetchMock, endpoint)).toHaveLength(1)
    expect(callsTo(fetchMock, DEVICES)).toHaveLength(1)
    vi.unstubAllGlobals()
  },
)

it('selects the created identity after a failed reread recovers without repeating registration', async () => {
  useLocale().setLocale('en')
  let registered = false
  let failed = false
  const created = {
    ...DEVICE_PAYLOAD.devices[0],
    id: 'created-id',
    name: 'Saved router',
    address: 'http://10.255.0.9',
  }
  const fetchMock = installFetch(async (input, init) => {
    if (input === DEVICES && init?.method === 'POST') {
      registered = true
      return json({ device: created })
    }
    if (input === DEVICES) {
      if (registered && !failed) {
        failed = true
        return json({ error: 'unavailable' }, 503)
      }
      return json({
        ...DEVICE_PAYLOAD,
        devices: registered
          ? [...DEVICE_PAYLOAD.devices, created]
          : DEVICE_PAYLOAD.devices,
      })
    }
    if (input === LISTS) return json(CATALOG_PAYLOAD)
    if (input === TARGETS) return json(TARGETS_PAYLOAD)
    if (input === REQUIREMENTS) return json(REQUIREMENTS_PAYLOAD)
    throw new Error(`unexpected request ${input}`)
  })
  const screen = await render(DevicesView)
  await screen
    .getByRole('button', { name: 'Add a connection', exact: true })
    .click()
  await screen.getByLabelText('Format', { exact: true }).click()
  await screen.getByRole('option', { name: /^Keenetic/ }).click()
  await screen.getByLabelText('Connection name').fill('Home router')
  await screen
    .getByLabelText('Router address', { exact: true })
    .fill('http://10.255.0.1')
  await screen.getByLabelText('Router login').fill('admin')
  await screen.getByLabelText('Interface for routes').fill('Wireguard0')
  await screen.getByRole('button', { name: 'Save', exact: true }).click()
  await expect
    .element(screen.getByText('Connections could not be refreshed'))
    .toBeVisible()
  await expect
    .element(screen.getByRole('heading', { name: 'Saved router' }))
    .toBeVisible()
  await expect
    .element(screen.getByLabelText('Router address', { exact: true }))
    .toHaveValue('http://10.255.0.9')
  await expect
    .element(screen.getByRole('button', { name: 'Save', exact: true }))
    .toBeDisabled()
  await screen.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect
    .element(
      screen.getByRole('button', {
        name: 'Configure connection Home router',
        exact: true,
      }),
    )
    .toHaveAttribute('aria-pressed', 'false')
  await expect
    .element(
      screen.getByRole('button', {
        name: 'Configure connection Saved router',
        exact: true,
      }),
    )
    .toHaveAttribute('aria-pressed', 'true')
  await expect
    .element(screen.getByRole('region', { name: 'Saved router', exact: true }))
    .toBeVisible()
  expect(
    fetchMock.mock.calls.filter(
      ([url, init]) => url === DEVICES && init?.method === 'POST',
    ),
  ).toHaveLength(1)
  vi.unstubAllGlobals()
})

// A connection that already opted in, complete in every field its target's
// deployer asks for. Editing it is what the cases below are about.
const AUTO_DEVICE = {
  id: 'device-1',
  target_id: 'keenetic',
  target_title: 'Keenetic',
  name: 'Home router',
  address: 'http://10.255.0.1',
  account: 'admin',
  interface: 'Wireguard0',
  credential_stored: true,
  auto_deliver: true,
  deployable: true,
}

const UPDATE = `${DEVICES}/device-1/update`
const FORGET = `${DEVICES}/device-1/forget`

describe('confirmed automatic-delivery changes', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it.each([{ enabled: true }, { enabled: false }])(
    'keeps the confirmed enabled=$enabled state when the registry reread fails',
    async ({ enabled }) => {
      const endpoint = `${DEVICES}/device-1/auto-delivery`
      // The password is saved either way: delivery only follows it.
      const before = { ...AUTO_DEVICE, auto_deliver: !enabled }
      const saved = { ...AUTO_DEVICE, auto_deliver: enabled }
      const fetchMock = installFetch(async (input, init) => {
        if (input === endpoint) return json({ device: saved })
        if (input === DEVICES) {
          const reads = callsTo(fetchMock, DEVICES).length
          if (reads === 2) return json({ error: 'unavailable' }, 503)
          return json({
            devices: [reads === 1 ? before : saved],
            secret_store_available: true,
          })
        }
        if (input === LISTS) return json(CATALOG_PAYLOAD)
        if (input === TARGETS) return json(TARGETS_PAYLOAD)
        if (input === REQUIREMENTS) return json(REQUIREMENTS_PAYLOAD)
        throw new Error(`unexpected request ${input} ${init?.method}`)
      })
      const screen = await render(DevicesView)
      const toggle = screen.getByRole('switch', {
        name: 'Send new versions automatically',
      })
      await expect
        .element(toggle)
        .toHaveAttribute('aria-checked', String(!enabled))
      await toggle.click()

      // The write was confirmed, so the switch says so, even though the read
      // that follows it failed; it stays unavailable until the list is read.
      await expect
        .element(screen.getByText('Connections could not be refreshed'))
        .toBeVisible()
      await expect
        .element(toggle)
        .toHaveAttribute('aria-checked', String(enabled))
      await expect.element(toggle).toBeDisabled()

      await screen.getByRole('button', { name: 'Retry', exact: true }).click()
      await expect.element(toggle).toBeEnabled()
      await expect
        .element(toggle)
        .toHaveAttribute('aria-checked', String(enabled))
      await expect.element(screen.getByText('Password saved')).toBeVisible()
      const written = callsTo(fetchMock, endpoint)
      expect(written).toHaveLength(1)
      expect(JSON.parse(String((written[0]?.[1] as RequestInit).body))).toEqual(
        { enabled },
      )
      expect(callsTo(fetchMock, DEVICES)).toHaveLength(3)
    },
  )
})

it('saves a renamed connection through the update endpoint and says nothing about consent', async () => {
  useLocale().setLocale('en')
  const fetchMock = installFetch(async (input, init) => {
    if (input === UPDATE) return json({ device: { id: 'device-1' } })
    if (input === DEVICES)
      return json({
        devices: [
          {
            ...AUTO_DEVICE,
            name:
              callsTo(fetchMock, UPDATE).length === 0
                ? 'Home router'
                : 'Study router',
          },
        ],
        secret_store_available: true,
      })
    if (input === LISTS) return json(CATALOG_PAYLOAD)
    if (input === TARGETS) return json(TARGETS_PAYLOAD)
    if (input === REQUIREMENTS) return json(REQUIREMENTS_PAYLOAD)
    throw new Error(`unexpected request ${input} ${init?.method}`)
  })
  const screen = await render(DevicesView)
  const save = screen.getByRole('button', { name: 'Save', exact: true })
  // Nothing has changed yet, so there is nothing to save.
  await expect.element(save).toBeDisabled()

  await screen.getByLabelText('Connection name').fill('Study router')
  // The consent this connection gave names a destination and an account, and
  // its own name is neither of them.
  await expect
    .element(
      screen.getByText('Saving ends the previous setup of this connection'),
    )
    .not.toBeInTheDocument()
  await expect.element(save).toBeEnabled()
  await save.click()

  await expect.element(screen.getByText('Parameters saved.')).toBeVisible()
  await expect
    .element(screen.getByRole('heading', { name: 'Study router' }))
    .toBeVisible()
  const written = callsTo(fetchMock, UPDATE)
  expect(written).toHaveLength(1)
  expect(JSON.parse(String((written[0]?.[1] as RequestInit).body))).toEqual({
    account: 'admin',
    address: 'http://10.255.0.1',
    interface: 'Wireguard0',
    name: 'Study router',
  })
  vi.unstubAllGlobals()
})

it('keeps an updated same-device card through a failed reread and retry', async () => {
  const saved = {
    ...AUTO_DEVICE,
    name: 'Study router',
    address: 'http://10.255.0.9',
  }
  const fetchMock = installFetch(async (input, init) => {
    if (input === UPDATE) return json({ device: saved })
    if (input === DEVICES) {
      const reads = callsTo(fetchMock, DEVICES).length
      if (reads === 2) return json({ error: 'unavailable' }, 503)
      return json({
        devices: [reads === 1 ? AUTO_DEVICE : saved],
        secret_store_available: true,
      })
    }
    if (input === LISTS) return json(CATALOG_PAYLOAD)
    if (input === TARGETS) return json(TARGETS_PAYLOAD)
    if (input === REQUIREMENTS) return json(REQUIREMENTS_PAYLOAD)
    throw new Error(`unexpected request ${input} ${init?.method}`)
  })
  const screen = await render(DevicesView)
  await screen.getByLabelText('Connection name').fill('Study router')
  await screen
    .getByLabelText('Router address', { exact: true })
    .fill('http://10.255.0.9')
  await screen.getByLabelText('Router login').fill('admin')
  await screen.getByLabelText('Interface for routes').fill('Wireguard0')
  await screen.getByRole('button', { name: 'Save', exact: true }).click()

  await expect
    .element(screen.getByText('Connections could not be refreshed'))
    .toBeVisible()
  await expect
    .element(screen.getByRole('heading', { name: 'Study router' }))
    .toBeVisible()
  await expect
    .element(screen.getByLabelText('Router address', { exact: true }))
    .toHaveValue('http://10.255.0.9')
  await expect
    .element(screen.getByRole('button', { name: 'Save', exact: true }))
    .toBeDisabled()

  await screen.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect
    .element(screen.getByRole('heading', { name: 'Study router' }))
    .toBeVisible()
  expect(callsTo(fetchMock, UPDATE)).toHaveLength(1)
  expect(callsTo(fetchMock, DEVICES)).toHaveLength(3)
  vi.unstubAllGlobals()
})

it('states that a changed destination ends the setup, then reports the state the service returned', async () => {
  useLocale().setLocale('en')
  const fetchMock = installFetch(async (input, init) => {
    if (input === UPDATE) return json({ device: { id: 'device-1' } })
    if (input === DEVICES) {
      // The service answers a changed destination by deleting the password and
      // revoking the opt-in, so the registry read that follows the write says
      // so.
      const written = callsTo(fetchMock, UPDATE).length === 1
      return json({
        devices: [
          {
            ...AUTO_DEVICE,
            address: written ? 'http://10.255.0.2' : 'http://10.255.0.1',
            auto_deliver: !written,
            credential_stored: !written,
          },
        ],
        secret_store_available: true,
      })
    }
    if (input === LISTS) return json(CATALOG_PAYLOAD)
    if (input === TARGETS) return json(TARGETS_PAYLOAD)
    if (input === REQUIREMENTS) return json(REQUIREMENTS_PAYLOAD)
    throw new Error(`unexpected request ${input} ${init?.method}`)
  })
  const screen = await render(DevicesView)
  const toggle = screen.getByRole('switch', {
    name: 'Send new versions automatically',
  })
  await expect.element(toggle).toHaveAttribute('aria-checked', 'true')

  await screen
    .getByLabelText('Router address', { exact: true })
    .fill('http://10.255.0.2')
  // The warning stands beside Save, before it is pressed.
  await expect
    .element(
      screen.getByText('Saving ends the previous setup of this connection'),
    )
    .toBeVisible()
  await screen.getByRole('button', { name: 'Save', exact: true }).click()

  await expect
    .element(
      screen.getByText(
        'Parameters saved. The saved password was deleted. Automatic delivery was turned off.',
      ),
    )
    .toBeVisible()
  await expect.element(screen.getByLabelText('Router password')).toBeVisible()
  await expect.element(toggle).toHaveAttribute('aria-checked', 'false')
  await expect.element(toggle).toBeDisabled()
  expect(callsTo(fetchMock, UPDATE)).toHaveLength(1)
  vi.unstubAllGlobals()
})

it('opens each connection at its stored parameters while the creation draft survives', async () => {
  useLocale().setLocale('en')
  const fetchMock = installFetch(async (input, init) => {
    if (input === DEVICES)
      return json({
        devices: [
          AUTO_DEVICE,
          { ...AUTO_DEVICE, id: 'device-2', name: 'Office router' },
        ],
        secret_store_available: true,
      })
    if (input === LISTS) return json(CATALOG_PAYLOAD)
    if (input === TARGETS) return json(TARGETS_PAYLOAD)
    if (input === REQUIREMENTS) return json(REQUIREMENTS_PAYLOAD)
    throw new Error(`unexpected request ${input} ${init?.method}`)
  })
  const screen = await render(DevicesView)
  await screen.getByLabelText('Connection name').fill('Renamed router')

  await screen
    .getByRole('button', { name: 'Add a connection', exact: true })
    .click()
  await screen.getByLabelText('Format', { exact: true }).click()
  await screen.getByRole('option', { name: /^Keenetic/ }).click()
  await screen.getByLabelText('Connection name').fill('Unfinished router')

  await screen
    .getByRole('button', {
      name: 'Configure connection Office router',
      exact: true,
    })
    .click()
  await screen
    .getByRole('button', {
      name: 'Configure connection Home router',
      exact: true,
    })
    .click()
  // A connection opens at what is stored for it, not at what was typed into
  // its form and never saved.
  await expect
    .element(screen.getByLabelText('Connection name'))
    .toHaveValue('Home router')

  await screen
    .getByRole('button', { name: 'Add a connection', exact: true })
    .click()
  await expect
    .element(screen.getByLabelText('Connection name'))
    .toHaveValue('Unfinished router')
  expect(
    fetchMock.mock.calls.every(([, request]) => request?.method !== 'POST'),
  ).toBe(true)
  vi.unstubAllGlobals()
})

it('confirms forgetting a connection and writes nothing when that confirmation is cancelled', async () => {
  useLocale().setLocale('en')
  const fetchMock = installFetch(async (input, init) => {
    if (input === FORGET) return json({ forgotten: 'device-1' })
    if (input === DEVICES)
      return json({
        devices: callsTo(fetchMock, FORGET).length === 0 ? [AUTO_DEVICE] : [],
        secret_store_available: true,
      })
    if (input === LISTS) return json(CATALOG_PAYLOAD)
    if (input === TARGETS) return json(TARGETS_PAYLOAD)
    if (input === REQUIREMENTS) return json(REQUIREMENTS_PAYLOAD)
    throw new Error(`unexpected request ${input} ${init?.method}`)
  })
  const screen = await render(DevicesView)
  const actions = screen.getByRole('button', {
    name: 'Actions for connection Home router',
  })
  await actions.click()
  await screen.getByRole('menuitem', { name: 'Forget this connection' }).click()
  await expect
    .element(
      screen
        .getByRole('dialog')
        .getByText('The saved password will be deleted.'),
    )
    .toBeVisible()
  await expect
    .element(
      screen
        .getByRole('dialog')
        .getByText('Installed files stay where they are.'),
    )
    .toBeVisible()
  await screen
    .getByRole('dialog')
    .getByRole('button', { name: 'Cancel', exact: true })
    .click()
  await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
  expect(callsTo(fetchMock, FORGET)).toHaveLength(0)

  await actions.click()
  await screen.getByRole('menuitem', { name: 'Forget this connection' }).click()
  await screen
    .getByRole('dialog')
    .getByRole('button', { name: 'Forget this connection', exact: true })
    .click()
  await expect
    .element(screen.getByRole('heading', { name: 'No connections yet' }))
    .toBeVisible()
  expect(callsTo(fetchMock, FORGET)).toHaveLength(1)
  vi.unstubAllGlobals()
})
