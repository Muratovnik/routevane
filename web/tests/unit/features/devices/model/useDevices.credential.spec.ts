import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useDevices } from '@/features/devices/model/useDevices'

type FetchInput = string | URL | Request

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const wire = (overrides: Record<string, unknown> = {}) => ({
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

const REQUIREMENTS = {
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

const TARGETS = {
  targets: [
    {
      id: 'keenetic',
      title: 'Keenetic',
      kind: 'router',
      format_key: 'keenetic-bat-ipv4-v1', // betterleaks:allow -- public fixture identifier
      renderer_id: 'keenetic-route-bat',
      file_extension: 'bat',
      manual_installation_hint: 'Upload the file.',
    },
  ],
}

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

const writesTo = (path: string): unknown[][] =>
  fetchMock.mock.calls.filter(([input]) => String(input) === path)

describe('useDevices saved password', () => {
  const install = (
    registry: () => Response,
    write: (url: string, body: unknown) => Response,
  ) =>
    fetchMock.mockImplementation(
      async (input: FetchInput, init?: RequestInit) => {
        const url = String(input)
        if (url === '/v1/devices' && init?.method === 'GET') return registry()
        if (url.startsWith('/v1/devices/'))
          return write(url, JSON.parse(String(init?.body ?? '{}')))
        if (url === '/v1/lists')
          return json({ lists: [], list_details: [], categories: [] })
        if (url === '/v1/targets') return json(TARGETS)
        if (url === '/v1/deployments/targets') return json(REQUIREMENTS)
        throw new Error(`unexpected request ${url}`)
      },
    )

  it('saves a password through its own route and records only the fact', async () => {
    let stored = false
    install(
      () =>
        json({
          devices: [wire({ credential_stored: stored })],
          secret_store_available: true,
        }),
      () => {
        stored = true
        return json({ device: wire({ credential_stored: true }) })
      },
    )
    const devices = useDevices()
    await devices.initialize()
    expect(devices.devices.value[0]?.credentialStored).toBe(false)

    await expect(
      devices.saveCredential('device-1', 'dummy-secret'),
    ).resolves.toBe(true)

    const [write] = writesTo('/v1/devices/device-1/credential')
    expect(JSON.parse(String((write?.[1] as RequestInit).body))).toEqual({
      credential: 'dummy-secret',
    })
    expect(devices.devices.value[0]?.credentialStored).toBe(true)
    expect(JSON.stringify(devices.devices.value)).not.toContain('dummy-secret')
  })

  it('removes it through its own route and adopts the delivery state the server returned', async () => {
    let removed = false
    install(
      () =>
        json({
          devices: [
            removed
              ? wire()
              : wire({ credential_stored: true, auto_deliver: true }),
          ],
          secret_store_available: true,
        }),
      () => {
        removed = true
        return json({ device: wire() })
      },
    )
    const devices = useDevices()
    await devices.initialize()

    await expect(devices.removeCredential('device-1')).resolves.toBe(true)

    const [write] = writesTo('/v1/devices/device-1/credential/remove')
    expect(JSON.parse(String((write?.[1] as RequestInit).body))).toEqual({})
    expect(devices.devices.value[0]).toMatchObject({
      credentialStored: false,
      autoDeliver: false,
    })
  })

  it('turns automatic delivery on without carrying a password', async () => {
    install(
      () =>
        json({
          devices: [wire({ credential_stored: true })],
          secret_store_available: true,
        }),
      () =>
        json({ device: wire({ credential_stored: true, auto_deliver: true }) }),
    )
    const devices = useDevices()
    await devices.initialize()

    await expect(devices.enable('device-1')).resolves.toBe(true)

    const [write] = writesTo('/v1/devices/device-1/auto-delivery')
    expect(JSON.parse(String((write?.[1] as RequestInit).body))).toEqual({
      enabled: true,
    })
  })

  it('reports what a refused write was, so a screen can say where it hurt', async () => {
    install(
      () => json({ devices: [wire()], secret_store_available: true }),
      () => json({ error: 'operation failed' }, 422),
    )
    const devices = useDevices()
    await devices.initialize()
    expect(devices.failure.value).toBeNull()

    await expect(
      devices.update(
        'device-1',
        'Home router',
        'http://10.255.0.2',
        'admin',
        'Wireguard0',
      ),
    ).resolves.toBe(false)

    expect(devices.work.value).toBe('failed')
    expect(devices.failure.value).toEqual({
      status: 422,
      code: '',
      field: '',
    })
  })

  it.each([
    {
      status: 422,
      body: { error: 'address_rejected', field: 'address' },
      want: { status: 422, code: 'address_rejected', field: 'address' },
    },
    {
      status: 422,
      body: { error: 'credential_invalid', field: 'credential' },
      want: { status: 422, code: 'credential_invalid', field: 'credential' },
    },
    {
      status: 503,
      body: { error: 'secret_store_unavailable' },
      want: { status: 503, code: 'secret_store_unavailable', field: '' },
    },
    {
      status: 409,
      body: { error: 'credential_required' },
      want: { status: 409, code: 'credential_required', field: '' },
    },
  ])(
    'reads the code $want.code and the field it names (ADR 0050)',
    async (refusal) => {
      install(
        () => json({ devices: [wire()], secret_store_available: true }),
        () => json(refusal.body, refusal.status),
      )
      const devices = useDevices()
      await devices.initialize()

      await expect(
        devices.update('device-1', 'Home router', 'x', 'a', 'b'),
      ).resolves.toBe(false)

      expect(devices.failure.value).toEqual(refusal.want)
    },
  )

  it('reads the registry again when delivery was refused for a missing password', async () => {
    let reads = 0
    install(
      () => {
        reads += 1
        return json({ devices: [wire()], secret_store_available: true })
      },
      () => json({ error: 'credential_required' }, 409),
    )
    const devices = useDevices()
    await devices.initialize()

    await expect(devices.enable('device-1')).resolves.toBe(false)

    expect(devices.failure.value?.status).toBe(409)
    await vi.waitFor(() => expect(reads).toBe(2))
  })
})

describe('useDevices requirements', () => {
  it('keeps requirements ready while a write reads the registry again', async () => {
    const reread = Promise.withResolvers<Response>()
    let requirementReads = 0
    fetchMock.mockImplementation(
      async (input: FetchInput, init?: RequestInit) => {
        const url = String(input)
        if (url === '/v1/devices' && init?.method === 'GET')
          return json({ devices: [wire()], secret_store_available: true })
        if (url.startsWith('/v1/devices/'))
          return json({ device: wire({ name: 'Study router' }) })
        if (url === '/v1/lists')
          return json({ lists: [], list_details: [], categories: [] })
        if (url === '/v1/targets') return json(TARGETS)
        if (url === '/v1/deployments/targets') {
          requirementReads += 1
          return requirementReads === 1 ? json(REQUIREMENTS) : reread.promise
        }
        throw new Error(`unexpected request ${url}`)
      },
    )
    const devices = useDevices()
    await devices.initialize()
    expect(devices.requirementsState.value).toBe('ready')

    const written = devices.update(
      'device-1',
      'Study router',
      'http://10.255.0.1',
      'admin',
      'Wireguard0',
    )
    await vi.waitFor(() => expect(requirementReads).toBe(2))

    // The re-read is in flight; the answer already held is still the answer.
    expect(devices.requirementsState.value).toBe('ready')
    reread.resolve(json(REQUIREMENTS))
    await written
    expect(devices.requirementsState.value).toBe('ready')
  })

  it('does not leave requirements loading when a registry retry overtakes the first read', async () => {
    const requirements = Promise.withResolvers<Response>()
    fetchMock.mockImplementation(
      async (input: FetchInput, init?: RequestInit) => {
        const url = String(input)
        if (url === '/v1/devices' && init?.method === 'GET')
          return json({ devices: [wire()], secret_store_available: true })
        if (url === '/v1/lists')
          return json({ lists: [], list_details: [], categories: [] })
        if (url === '/v1/targets') return json(TARGETS)
        if (url === '/v1/deployments/targets') return requirements.promise
        throw new Error(`unexpected request ${url}`)
      },
    )
    const devices = useDevices()
    const first = devices.initialize()
    const retry = devices.retryDevices()
    requirements.resolve(json(REQUIREMENTS))
    await Promise.all([first, retry])

    // `loading` belongs to a read that is still running; none is.
    expect(devices.requirementsState.value).not.toBe('loading')
  })
})
