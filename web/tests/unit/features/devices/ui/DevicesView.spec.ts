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

    await expect
      .element(
        screen.getByRole('region', { name: 'Add a connection', exact: true }),
      )
      .toBeVisible()
    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    // With nothing saved the form is the whole work area: no header action
    // opens it, and there is nothing a cancel could return to.
    await expect
      .element(
        screen.getByRole('button', { name: 'Add a connection', exact: true }),
      )
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: 'Cancel', exact: true }))
      .not.toBeInTheDocument()
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
    await screen
      .getByLabelText('Password', { exact: true })
      .fill('fixture-password')
    await screen
      .getByRole('button', { name: 'Add a connection', exact: true })
      .click()
    await screen.getByLabelText('Device or application').click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()
    await screen.getByLabelText('Connection name').fill('Unfinished router')
    await connection.click()
    await expect
      .element(screen.getByLabelText('Password', { exact: true }))
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
    await screen.getByLabelText('Device or application').click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()
    await expect
      .element(screen.getByLabelText('Connection name'))
      .toHaveValue('')
    expect(
      fetchMock.mock.calls.every(([, request]) => request?.method !== 'POST'),
    ).toBe(true)
  })
})

it('marks a saved device change stale and retries only the read while preserving drafts', async () => {
  useLocale().setLocale('en')
  const fetchMock = installFetch(async (input, init) => {
    if (input === '/v1/devices/device-1/forget') return json({})
    if (input === DEVICES) {
      const reads = callsTo(fetchMock, DEVICES).length
      if (reads === 2) return json({ error: 'unavailable' }, 503)
      return json(
        reads === 1
          ? DEVICE_PAYLOAD
          : { devices: [], secret_store_available: true },
      )
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
  await screen.getByLabelText('Device or application').click()
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
  await screen.getByRole('menuitem', { name: 'Forget this connection' }).click()
  await screen
    .getByRole('button', { name: 'Forget this connection', exact: true })
    .click()
  await expect
    .element(screen.getByText('Connections could not be refreshed'))
    .toBeVisible()
  await expect
    .element(screen.getByRole('heading', { name: 'Home router' }))
    .toBeVisible()
  // A registry this tab could not re-read is not one to write to again.
  await expect
    .element(
      screen.getByRole('button', {
        name: 'Actions for connection Home router',
      }),
    )
    .toBeDisabled()
  await screen.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect
    .element(screen.getByRole('heading', { name: 'Home router' }))
    .not.toBeInTheDocument()
  await expect
    .element(screen.getByText('Connections could not be refreshed'))
    .not.toBeInTheDocument()
  await expect
    .element(screen.getByLabelText('Connection name'))
    .toHaveValue('Unsaved connection')
  expect(callsTo(fetchMock, '/v1/devices/device-1/forget')).toHaveLength(1)
  expect(callsTo(fetchMock, DEVICES)).toHaveLength(3)
  vi.unstubAllGlobals()
})

it('selects the created identity after a failed reread recovers without repeating registration', async () => {
  useLocale().setLocale('en')
  let registered = false
  let failed = false
  const created = {
    ...DEVICE_PAYLOAD.devices[0],
    id: 'created-id',
    name: 'Home router',
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
  await screen.getByLabelText('Device or application').click()
  await screen.getByRole('option', { name: /^Keenetic/ }).click()
  await screen.getByLabelText('Connection name').fill('Home router')
  await screen.getByLabelText('Device address').fill('http://192.168.1.1')
  await screen.getByLabelText('Router login').fill('admin')
  await screen.getByLabelText('Interface for routes').fill('Wireguard0')
  await screen.getByRole('button', { name: 'Save', exact: true }).click()
  await expect
    .element(screen.getByText('Connections could not be refreshed'))
    .toBeVisible()
  await expect
    .element(screen.getByRole('button', { name: 'Save', exact: true }))
    .toBeDisabled()
  await screen.getByRole('button', { name: 'Retry', exact: true }).click()
  const connections = screen.getByRole('button', {
    name: 'Configure connection Home router',
    exact: true,
  })
  await expect
    .element(connections.nth(0))
    .toHaveAttribute('aria-pressed', 'false')
  await expect
    .element(connections.nth(1))
    .toHaveAttribute('aria-pressed', 'true')
  await expect
    .element(screen.getByRole('region', { name: 'Home router', exact: true }))
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
  address: 'http://192.168.1.1',
  account: 'admin',
  interface: 'Wireguard0',
  auto_deliver: true,
  deployable: true,
}

const UPDATE = `${DEVICES}/device-1/update`
const FORGET = `${DEVICES}/device-1/forget`

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
    .element(screen.getByText('Saving turns automatic delivery off'))
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
    address: 'http://192.168.1.1',
    interface: 'Wireguard0',
    name: 'Study router',
  })
  vi.unstubAllGlobals()
})

it('states that a changed destination withdraws consent, then reports the state the service returned', async () => {
  useLocale().setLocale('en')
  const fetchMock = installFetch(async (input, init) => {
    if (input === UPDATE) return json({ device: { id: 'device-1' } })
    if (input === DEVICES) {
      // The service answers a changed destination by revoking the opt-in, so
      // the registry read that follows the write says so.
      const written = callsTo(fetchMock, UPDATE).length === 1
      return json({
        devices: [
          {
            ...AUTO_DEVICE,
            address: written ? 'http://192.168.1.2' : 'http://192.168.1.1',
            auto_deliver: !written,
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
  await expect
    .element(
      screen.getByRole('button', { name: 'Turn off automatic delivery' }),
    )
    .toBeVisible()

  await screen.getByLabelText('Device address').fill('http://192.168.1.2')
  // The warning stands beside Save, before it is pressed.
  await expect
    .element(screen.getByText('Saving turns automatic delivery off'))
    .toBeVisible()
  await screen.getByRole('button', { name: 'Save', exact: true }).click()

  await expect
    .element(
      screen.getByText(
        'Parameters saved and automatic delivery is off. Turn it on again when you are ready.',
      ),
    )
    .toBeVisible()
  await expect
    .element(screen.getByLabelText('Password', { exact: true }))
    .toBeVisible()
  await expect
    .element(screen.getByRole('button', { name: 'Turn on automatic delivery' }))
    .toBeVisible()
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
  await screen.getByLabelText('Device or application').click()
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
        .getByText(
          'The stored password is removed and automatic delivery is turned off. Files already installed on the device stay there.',
        ),
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
    .element(
      screen.getByRole('region', { name: 'Add a connection', exact: true }),
    )
    .toBeVisible()
  expect(callsTo(fetchMock, FORGET)).toHaveLength(1)
  vi.unstubAllGlobals()
})
