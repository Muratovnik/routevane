import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import DevicesView from '@/features/devices/ui/DevicesView.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import { useNotify } from '@/shared/model/useNotify'

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
      manual_installation_steps: ['Upload the file.'],
    },
    {
      id: 'singbox',
      title: 'sing-box',
      kind: 'app',
      format_key: 'singbox-json-v1',
      renderer_id: 'singbox-json',
      file_extension: 'json',
      manual_installation_hint: 'Place the file.',
      manual_installation_steps: ['Place the file.'],
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
  /** The answer the service gives to one action instead of performing it. */
  refusal: { action: string; status: number; body: unknown } | null
  /** What saving a password reports its sign-in found (ADR 0059). */
  verification?: unknown
}

/** The registry's routes as ADR 0050 states them, kept in memory per case. */
const installRegistry = (seed: Wire[], secretStore = true): Registry => {
  const state: Registry = {
    devices: seed.map((device) => ({ ...device })),
    secretStore,
    writes: [],
    refusal: null,
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
      const { refusal } = state
      if (refusal !== null && refusal.action === action)
        return json(refusal.body, refusal.status)
      const device = state.devices.find((candidate) => candidate.id === id)!
      const needsPassword = device.target_id === 'keenetic'
      if (action === 'credential') device.credential_stored = true
      else if (action === 'credential/remove') {
        device.credential_stored = false
        if (needsPassword) device.auto_deliver = false
      } else if (action === 'auto-delivery')
        device.auto_deliver = body.enabled === true
      else if (action === 'update') {
        device.name = String(body.name)
        device.address = String(body.address)
      } else throw new Error(`unexpected write ${input}`)
      if (action === 'credential' && state.verification !== undefined)
        return json({ device, verification: state.verification })
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
      .element(screen.getByText('Kept in the system password store.'))
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

    // Removing the password asks first, and the question says that delivery
    // goes with it.
    registry.hold = undefined
    await screen.getByRole('button', { name: 'Remove', exact: true }).click()
    const question = screen.getByRole('dialog', {
      name: 'Remove the password of “Home router”?',
    })
    await expect
      .element(
        question.getByText(
          'Removing the password also turns automatic delivery off.',
        ),
      )
      .toBeVisible()
    await question.getByRole('button', { name: 'Remove', exact: true }).click()
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
          /creates its routes and FQDN groups on the router again\. The earlier ones stay on the router; remove them by hand\./,
        ),
      )
      .toBeVisible()
  })

  // Saving signs in with the password first (ADR 0059): a password the router
  // refuses is not kept and is named at its field, and one it accepts is kept
  // and said to work.
  it('names a password the router refused at the field and keeps none', async () => {
    const registry = installRegistry([router()])
    registry.refusal = {
      action: 'credential',
      status: 422,
      body: { error: 'device_auth_rejected', field: 'credential' },
    }
    const screen = await render(DevicesView)
    const field = screen.getByLabelText('Router password')

    await field.fill(TYPED)
    await screen.getByRole('button', { name: 'Save password' }).click()

    await expect
      .element(
        screen.getByText(
          'The router did not accept this password, so it was not saved.',
        ),
      )
      .toBeVisible()
    expect(document.activeElement).toBe(field.element())
    await expect
      .element(screen.getByText('Password saved'))
      .not.toBeInTheDocument()
  })

  it.each([
    [
      { result: 'verified' },
      'Password saved',
      'The router accepted a sign-in with it.',
    ],
    [
      { result: 'unverified', code: 'device_unreachable' },
      'Password saved without a check',
      'The router does not answer. Check the address and that this computer is on its network.',
    ],
    [
      { result: 'unverified', code: 'check_failed' },
      'Password saved without a check',
      'The sign-in could not be checked.',
    ],
  ])(
    'says what the sign-in on saving showed: %o',
    async (verification, title, body) => {
      useNotify().notices.value.forEach((notice) =>
        useNotify().dismiss(notice.id),
      )
      const registry = installRegistry([router()])
      registry.verification = verification
      const screen = await render(DevicesView)

      await screen.getByLabelText('Router password').fill(TYPED)
      await screen.getByRole('button', { name: 'Save password' }).click()

      await expect.element(screen.getByText('Password saved')).toBeVisible()
      await vi.waitFor(() => {
        expect(useNotify().notices.value.at(-1)).toMatchObject({ title, body })
      })
    },
  )

  it('says beside the address that the service refused it, and keeps the notice for what has no field', async () => {
    const registry = installRegistry([router()])
    registry.refusal = {
      action: 'update',
      status: 422,
      body: { error: 'address_rejected', field: 'address' },
    }
    const screen = await render(DevicesView)
    const address = screen.getByLabelText('Router address', { exact: true })

    await address.fill('http://10.255.0.9')
    await screen.getByRole('button', { name: 'Save', exact: true }).click()
    await expect
      .element(
        screen.getByText(
          'Use a local network IP address, not a host name or a public address.',
        ),
      )
      .toBeVisible()
    expect(document.activeElement).toBe(address.element())
    // A refusal that is stated at its field is not stated a second time.
    await expect
      .element(screen.getByText('That did not go through'))
      .not.toBeInTheDocument()

    // Another address is another question.
    await address.fill('http://10.255.0.2')
    await expect
      .element(
        screen.getByText(
          'Use a local network IP address, not a host name or a public address.',
        ),
      )
      .not.toBeInTheDocument()
  })

  // The old screen guessed the field from the status and from what had changed,
  // so a 422 that names nothing, met with a changed address, was pinned on the
  // address.
  it('does not pin an unnamed refusal on the address', async () => {
    const registry = installRegistry([router()])
    registry.refusal = {
      action: 'update',
      status: 422,
      body: { error: 'operation failed' },
    }
    const screen = await render(DevicesView)

    await screen
      .getByLabelText('Router address', { exact: true })
      .fill('http://10.255.0.2')
    await screen.getByRole('button', { name: 'Save', exact: true }).click()
    await expect
      .element(screen.getByText('That did not go through'))
      .toBeVisible()
    await expect
      .element(
        screen.getByText(
          'Use a local network IP address, not a host name or a public address.',
        ),
      )
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByText('The service did not accept this address.'))
      .not.toBeInTheDocument()
  })

  it('says beside the password field that the service could not take the password', async () => {
    const registry = installRegistry([router()])
    registry.refusal = {
      action: 'credential',
      status: 422,
      body: { error: 'credential_invalid', field: 'credential' },
    }
    const screen = await render(DevicesView)
    const field = screen.getByLabelText('Router password')

    await field.fill(TYPED)
    await screen.getByRole('button', { name: 'Save password' }).click()
    await expect
      .element(screen.getByText('The password is empty or too long.'))
      .toBeVisible()
    await expect
      .element(screen.getByText('That did not go through'))
      .not.toBeInTheDocument()
    // Typing another is a new attempt.
    await field.fill(`${TYPED}2`)
    await expect
      .element(screen.getByText('The password is empty or too long.'))
      .not.toBeInTheDocument()
  })

  // The refusal answers one attempt. Replace, refuse, cancel and replace again
  // must open an empty field that has nothing to say yet.
  it('does not bring a refusal back on the field of a later replacement', async () => {
    const registry = installRegistry([router({ credential_stored: true })])
    registry.refusal = {
      action: 'credential',
      status: 422,
      body: { error: 'credential_invalid', field: 'credential' },
    }
    const screen = await render(DevicesView)

    await screen.getByRole('button', { name: 'Replace' }).click()
    await screen.getByLabelText('Router password').fill(TYPED)
    await screen.getByRole('button', { name: 'Save password' }).click()
    await expect
      .element(screen.getByText('The password is empty or too long.'))
      .toBeVisible()

    // The parameters have a Cancel of their own; this one is the password's.
    await screen
      .getByRole('region', { name: 'Password', exact: true })
      .getByRole('button', { name: 'Cancel' })
      .click()
    await screen.getByRole('button', { name: 'Replace' }).click()

    await expect
      .element(screen.getByLabelText('Router password'))
      .toHaveValue('')
    await expect
      .element(screen.getByText('The password is empty or too long.'))
      .not.toBeInTheDocument()
  })

  it.each([
    {
      name: 'a platform without a credential store',
      action: 'credential',
      status: 503,
      body: { error: 'secret_store_unavailable' },
      shown: 'This system has no credential store',
    },
    {
      name: 'automatic delivery asked for without a saved password',
      action: 'auto-delivery',
      status: 409,
      body: { error: 'credential_required' },
      shown: 'Automatic delivery needs a saved password.',
    },
  ])('states $name in words of its own', async (refused) => {
    const registry = installRegistry([
      router({ credential_stored: refused.action === 'auto-delivery' }),
    ])
    registry.refusal = {
      action: refused.action,
      status: refused.status,
      body: refused.body,
    }
    const screen = await render(DevicesView)

    if (refused.action === 'credential') {
      await screen.getByLabelText('Router password').fill(TYPED)
      await screen.getByRole('button', { name: 'Save password' }).click()
    } else {
      await screen
        .getByRole('switch', { name: 'Send new versions automatically' })
        .click()
    }

    await expect
      .element(screen.getByText('That did not go through'))
      .toBeVisible()
    await expect
      .element(screen.getByText(refused.shown, { exact: false }))
      .toBeVisible()
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
