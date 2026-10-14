import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import DevicesView from '@/features/devices/ui/DevicesView.vue'
import { useLocale } from '@/shared/i18n/useLocale'

const DEVICES = '/v1/devices'
const REQUIREMENTS = '/v1/deployments/targets'
const TYPED = 'dummy-router-value'

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

type Wire = {
  id: string
  target_id: string
  target_title: string
  name: string
  address: string
  account: string
  interface: string
  credential_stored: boolean
  auto_deliver: boolean
  deployable: boolean
}

const router = (overrides: Partial<Wire> = {}): Wire => ({
  id: 'device-1',
  target_id: 'keenetic',
  target_title: 'Keenetic',
  name: 'Home router',
  address: 'http://10.255.0.1',
  account: 'admin',
  interface: 'Wireguard0',
  credential_stored: false,
  auto_deliver: false,
  deployable: true,
  ...overrides,
})

const localFile = (): Wire =>
  router({
    id: 'device-2',
    target_id: 'singbox',
    target_title: 'sing-box',
    name: 'Local client',
    address: 'file:///C:/sing-box/config.json',
    account: '',
    interface: '',
  })

const TARGETS = {
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
    {
      id: 'singbox',
      title: 'sing-box',
      kind: 'app',
      format_key: 'singbox-json-v1',
      renderer_id: 'singbox-json',
      file_extension: 'json',
      manual_installation_hint: 'Place the file.',
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
    {
      target_id: 'singbox',
      title: 'sing-box',
      deployer_id: 'singbox',
      requirements: {
        address_label: 'Configuration path',
        address_example: 'file:///C:/sing-box/config.json',
        needs_credential: false,
        needs_interface: false,
        interface_label: '',
      },
    },
  ],
}

type Registry = {
  devices: Wire[]
  secretStore: boolean
  /** Settles a write after the test releases it, so a pending state can be read. */
  hold?: Promise<unknown>
  writes: { path: string; body: unknown }[]
  refuseUpdate: boolean
}

/** The registry's routes as ADR 0050 states them, kept in memory per case. */
const installRegistry = (seed: Wire[], secretStore = true): Registry => {
  const state: Registry = {
    devices: seed.map((device) => ({ ...device })),
    secretStore,
    writes: [],
    refuseUpdate: false,
  }
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: string, init?: RequestInit) => {
      if (input === '/v1/lists')
        return json({ lists: [], list_details: [], categories: [] })
      if (input === '/v1/targets') return json(TARGETS)
      if (input === REQUIREMENTS) return json(REQUIREMENTS_PAYLOAD)
      if (input === DEVICES && init?.method === 'GET')
        return json({
          devices: state.devices,
          secret_store_available: state.secretStore,
        })
      const match = /^\/v1\/devices\/([^/]+)\/(.+)$/.exec(input)
      if (match === null) throw new Error(`unexpected request ${input}`)
      const [, id, action] = match
      const body = JSON.parse(String(init?.body ?? '{}')) as Record<
        string,
        unknown
      >
      state.writes.push({ path: `${id}/${action}`, body })
      await state.hold
      const device = state.devices.find((candidate) => candidate.id === id)!
      const needsPassword = device.target_id === 'keenetic'
      if (action === 'credential') device.credential_stored = true
      else if (action === 'credential/remove') {
        device.credential_stored = false
        if (needsPassword) device.auto_deliver = false
      } else if (action === 'auto-delivery')
        device.auto_deliver = body.enabled === true
      else if (action === 'update') {
        if (state.refuseUpdate) return json({ error: 'operation failed' }, 422)
        device.name = String(body.name)
        device.address = String(body.address)
      } else throw new Error(`unexpected write ${input}`)
      return json({ device })
    }),
  )
  return state
}

describe('a connection password', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('is saved apart from automatic delivery, and the switch follows the server', async () => {
    const registry = installRegistry([router()])
    const screen = await render(DevicesView)
    const toggle = screen.getByRole('switch', {
      name: 'Send new versions automatically',
    })

    // Nothing is saved yet, so the switch cannot be turned on and says why.
    await expect.element(toggle).toBeDisabled()
    await expect
      .element(screen.getByText('Save the password first'))
      .toBeVisible()
    await expect
      .element(screen.getByText('Kept in the credential store of this system'))
      .toBeVisible()

    await screen.getByLabelText('Router password').fill(TYPED)
    await screen.getByRole('button', { name: 'Save password' }).click()
    await expect.element(screen.getByText('Password saved')).toBeVisible()
    expect(registry.writes).toEqual([
      { path: 'device-1/credential', body: { credential: TYPED } },
    ])
    // The password is held for the one request and not kept in the page.
    await expect
      .element(screen.getByLabelText('Router password'))
      .not.toBeInTheDocument()

    // The switch states what the server holds, never what was clicked: while
    // the write is pending it still reads the old state.
    const gate = Promise.withResolvers<null>()
    registry.hold = gate.promise
    await expect.element(toggle).toBeEnabled()
    await toggle.click()
    await expect.element(toggle).toHaveAttribute('aria-checked', 'false')
    gate.resolve(null)
    await expect.element(toggle).toHaveAttribute('aria-checked', 'true')
    expect(registry.writes.at(-1)).toEqual({
      path: 'device-1/auto-delivery',
      body: { enabled: true },
    })

    // Removing the password says beside the act that delivery goes with it.
    await expect
      .element(
        screen.getByText(
          'Removing the password also turns automatic delivery off.',
        ),
      )
      .toBeVisible()
    registry.hold = undefined
    await screen.getByRole('button', { name: 'Remove password' }).click()
    await expect
      .element(screen.getByText('Password saved'))
      .not.toBeInTheDocument()
    await expect.element(toggle).toHaveAttribute('aria-checked', 'false')
    await expect.element(toggle).toBeDisabled()
  })

  it('stays when automatic delivery is turned off', async () => {
    installRegistry([router({ credential_stored: true, auto_deliver: true })])
    const screen = await render(DevicesView)
    await screen
      .getByRole('switch', { name: 'Send new versions automatically' })
      .click()
    await expect
      .element(
        screen.getByRole('switch', { name: 'Send new versions automatically' }),
      )
      .toHaveAttribute('aria-checked', 'false')
    await expect.element(screen.getByText('Password saved')).toBeVisible()
  })

  it('replaces a saved password without showing it', async () => {
    const registry = installRegistry([router({ credential_stored: true })])
    const screen = await render(DevicesView)
    await expect
      .element(screen.getByLabelText('Router password'))
      .not.toBeInTheDocument()
    await screen.getByRole('button', { name: 'Replace' }).click()
    await screen.getByLabelText('Router password').fill(TYPED)
    await screen.getByRole('button', { name: 'Save password' }).click()
    await expect.element(screen.getByText('Password saved')).toBeVisible()
    expect(registry.writes).toEqual([
      { path: 'device-1/credential', body: { credential: TYPED } },
    ])
  })

  it('cannot be saved where the system has no credential store, and the switch says so', async () => {
    installRegistry([router()], false)
    const screen = await render(DevicesView)
    await expect
      .element(
        screen.getByText(
          'This system has no credential store, so a password cannot be saved.',
        ),
      )
      .toBeVisible()
    await expect
      .element(screen.getByLabelText('Router password'))
      .not.toBeInTheDocument()
    await expect
      .element(
        screen.getByText('This system has no credential store', {
          exact: true,
        }),
      )
      .toBeVisible()
    await expect
      .element(
        screen.getByRole('switch', { name: 'Send new versions automatically' }),
      )
      .toBeDisabled()
  })

  it('has no section at all for a format that takes none', async () => {
    installRegistry([localFile()])
    const screen = await render(DevicesView)
    const toggle = screen.getByRole('switch', {
      name: 'Send new versions automatically',
    })
    await expect.element(toggle).toBeEnabled()
    await expect
      .element(screen.getByRole('heading', { name: 'Password', exact: true }))
      .not.toBeInTheDocument()
    await toggle.click()
    await expect.element(toggle).toHaveAttribute('aria-checked', 'true')
  })
})

describe('changing a saved connection', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('keeps the form mounted and focus in it while the registry is read again', async () => {
    const registry = installRegistry([router()])
    const screen = await render(DevicesView)
    const name = screen.getByLabelText('Connection name')
    await expect.element(name).toBeVisible()
    const field = name.element() as HTMLInputElement

    await name.fill('Study router')
    await screen.getByRole('button', { name: 'Save', exact: true }).click()
    await expect.element(screen.getByText('Parameters saved.')).toBeVisible()
    expect(registry.writes.map((write) => write.path)).toEqual([
      'device-1/update',
    ])

    // The same input element is still the one on screen, and it holds focus:
    // nothing between the write and the confirmation rebuilt the card.
    expect(field.isConnected).toBe(true)
    expect(document.activeElement).toBe(field)
  })

  it('says beside Save what saving a changed address ends, and only then', async () => {
    installRegistry([router({ credential_stored: true, auto_deliver: true })])
    const screen = await render(DevicesView)
    const name = screen.getByLabelText('Connection name')
    await name.fill('Study router')
    await expect
      .element(
        screen.getByText('Saving ends the previous setup of this connection'),
      )
      .not.toBeInTheDocument()

    await screen
      .getByLabelText('Router address', { exact: true })
      .fill('http://10.255.0.50')
    await expect
      .element(
        screen.getByText('Saving ends the previous setup of this connection'),
      )
      .toBeVisible()
    await expect
      .element(screen.getByText(/The saved password will be deleted\./))
      .toBeVisible()
    await expect
      .element(screen.getByText(/Automatic delivery will be turned off\./))
      .toBeVisible()
    await expect
      .element(
        screen.getByText(
          /stop managing the routes and FQDN groups it created on the router/,
        ),
      )
      .toBeVisible()
  })

  it('names the address beside its field when a refused save changed the address', async () => {
    const registry = installRegistry([router()])
    registry.refuseUpdate = true
    const screen = await render(DevicesView)
    const address = screen.getByLabelText('Router address', { exact: true })

    await address.fill('http://10.255.0.60')
    await screen.getByRole('button', { name: 'Save', exact: true }).click()
    await expect
      .element(screen.getByText('The service did not accept this address.'))
      .toBeVisible()
    expect(document.activeElement).toBe(address.element())

    // Only the name changed: nothing points at the address.
    await address.fill('http://10.255.0.1')
    await screen.getByLabelText('Connection name').fill('Study router')
    await screen.getByRole('button', { name: 'Save', exact: true }).click()
    await expect
      .element(screen.getByText('That did not go through'))
      .toBeVisible()
    await expect
      .element(screen.getByText('The service did not accept this address.'))
      .not.toBeInTheDocument()
  })

  it('lists what forgetting ends, from what the connection actually holds', async () => {
    installRegistry([
      router({ credential_stored: true, auto_deliver: true }),
      localFile(),
    ])
    const screen = await render(DevicesView)
    await screen
      .getByRole('button', { name: 'Actions for connection Home router' })
      .click()
    await screen
      .getByRole('menuitem', { name: 'Forget this connection' })
      .click()
    const dialog = screen.getByRole('dialog')
    await expect
      .element(dialog.getByText('The saved password will be deleted.'))
      .toBeVisible()
    await expect
      .element(dialog.getByText('Automatic delivery will be turned off.'))
      .toBeVisible()
    await expect
      .element(
        dialog.getByText(
          /stop managing the routes and FQDN groups it created on the router/,
        ),
      )
      .toBeVisible()
    await expect
      .element(dialog.getByText('Installed files stay where they are.'))
      .toBeVisible()
  })
})

describe('the connection list', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('has a heading of its own beside the page title', async () => {
    installRegistry([router()])
    const screen = await render(DevicesView)
    await expect
      .element(screen.getByRole('heading', { name: 'Saved connections' }))
      .toBeVisible()
    await expect
      .element(
        screen.getByRole('heading', { name: 'Connections', exact: true }),
      )
      .toBeVisible()
    expect(
      screen
        .getByRole('heading', { name: 'Connections', exact: true })
        .elements(),
    ).toHaveLength(1)
  })
})

describe('submitting an incomplete connection form', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('states each missing field and moves focus to the first one', async () => {
    const registry = installRegistry([])
    const screen = await render(DevicesView)
    await screen
      .getByRole('button', { name: 'Add a connection', exact: true })
      .click()
    await screen.getByLabelText('Format', { exact: true }).click()
    await screen.getByRole('option', { name: /^Keenetic/ }).click()

    const save = screen.getByRole('button', { name: 'Save', exact: true })
    await expect.element(save).toBeEnabled()
    await save.click()
    expect(screen.getByText('Fill in this field').elements()).toHaveLength(4)
    expect(document.activeElement).toBe(
      screen.getByLabelText('Connection name').element(),
    )
    await expect
      .element(screen.getByLabelText('Connection name'))
      .toHaveAttribute('aria-required', 'true')
    expect(registry.writes).toEqual([])
  })
})
