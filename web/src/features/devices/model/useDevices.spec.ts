import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useDevices } from '@/features/devices/model/useDevices'

type FetchInput = string | URL | Request

function json(payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

const devicePayload = {
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

const listsPayload = {
  lists: [],
  list_details: [],
  categories: [],
}

const targetsPayload = {
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

describe('useDevices degraded dependencies', () => {
  it('keeps registered devices and the catalog when deployer requirements fail', async () => {
    fetchMock.mockImplementation((input: FetchInput) => {
      const url = String(input)
      if (url === '/v1/devices') return Promise.resolve(json(devicePayload))
      if (url === '/v1/lists') return Promise.resolve(json(listsPayload))
      if (url === '/v1/targets') return Promise.resolve(json(targetsPayload))
      if (url === '/v1/deployments/targets')
        return Promise.reject(new TypeError('unavailable'))
      return Promise.reject(new Error(`unexpected request ${url}`))
    })
    const devices = useDevices()

    await devices.initialize()

    expect(devices.state.value).toBe('ready')
    expect(devices.devices.value).toHaveLength(1)
    expect(devices.catalogAvailable.value).toBe(true)
    expect(devices.deploymentCatalogAvailable.value).toBe(false)
    expect(devices.targets.value.map((target) => target.id)).toEqual([
      'keenetic',
    ])
    expect(devices.deployableTargets.value).toEqual([])
  })

  it('keeps registered devices readable but withdraws registration when the catalog fails', async () => {
    fetchMock.mockImplementation((input: FetchInput) => {
      const url = String(input)
      if (url === '/v1/devices') return Promise.resolve(json(devicePayload))
      if (url === '/v1/lists')
        return Promise.reject(new TypeError('unavailable'))
      if (url === '/v1/deployments/targets')
        return Promise.resolve(json({ targets: [] }))
      return Promise.reject(new Error(`unexpected request ${url}`))
    })
    const devices = useDevices()

    await devices.initialize()

    expect(devices.state.value).toBe('ready')
    expect(devices.devices.value).toHaveLength(1)
    expect(devices.catalogAvailable.value).toBe(false)
    expect(devices.targets.value).toEqual([])
  })

  it('does not register or enable while deployment requirements are unknown', async () => {
    fetchMock.mockImplementation((input: FetchInput, init?: RequestInit) => {
      const url = String(input)
      if (url === '/v1/devices') return Promise.resolve(json(devicePayload))
      if (url === '/v1/lists') return Promise.resolve(json(listsPayload))
      if (url === '/v1/targets') return Promise.resolve(json(targetsPayload))
      if (url === '/v1/deployments/targets')
        return Promise.reject(new TypeError('unavailable'))
      if (url === '/v1/devices' && init?.method === 'POST')
        return Promise.resolve(json({}))
      return Promise.reject(new Error(`unexpected request ${url}`))
    })
    const devices = useDevices()

    await devices.initialize()

    await expect(
      devices.register(
        'keenetic',
        'Another router',
        'http://192.168.1.2',
        'admin',
        'Wireguard0',
      ),
    ).resolves.toBe(false)
    await expect(devices.enable('device-1', 'secret')).resolves.toBe(false)

    expect(
      fetchMock.mock.calls.filter(
        ([input, request]) =>
          String(input) === '/v1/devices' &&
          (request as RequestInit | undefined)?.method === 'POST',
      ),
    ).toHaveLength(0)
    expect(
      fetchMock.mock.calls.filter(([input]) =>
        String(input).includes('/auto-delivery'),
      ),
    ).toHaveLength(0)
  })

  it('retries only requirements and keeps known devices', async () => {
    let requirementsReads = 0
    fetchMock.mockImplementation((input: FetchInput) => {
      const url = String(input)
      if (url === '/v1/devices') return Promise.resolve(json(devicePayload))
      if (url === '/v1/lists') return Promise.resolve(json(listsPayload))
      if (url === '/v1/targets') return Promise.resolve(json(targetsPayload))
      if (url === '/v1/deployments/targets') {
        requirementsReads += 1
        return requirementsReads === 1
          ? Promise.reject(new TypeError('unavailable'))
          : Promise.resolve(
              json({
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
              }),
            )
      }
      return Promise.reject(new Error(`unexpected request ${url}`))
    })
    const devices = useDevices()

    await devices.initialize()
    expect(devices.requirementsState.value).toBe('failed')
    expect(devices.devices.value[0]?.name).toBe('Home router')

    await expect(devices.retryRequirements()).resolves.toBe(true)

    expect(devices.requirementsState.value).toBe('ready')
    expect(devices.devices.value[0]?.name).toBe('Home router')
    expect(requirementsReads).toBe(2)
    expect(
      fetchMock.mock.calls.filter(([input]) => String(input) === '/v1/devices'),
    ).toHaveLength(1)
    expect(
      fetchMock.mock.calls.filter(([input]) => String(input) === '/v1/lists'),
    ).toHaveLength(1)
  })

  it('keeps registration available for a target with no deployer after a successful read', async () => {
    fetchMock.mockImplementation((input: FetchInput, init?: RequestInit) => {
      const url = String(input)
      if (url === '/v1/devices' && init?.method === 'POST')
        return Promise.resolve(json({}))
      if (url === '/v1/devices')
        return Promise.resolve(
          json({ devices: [], secret_store_available: true }),
        )
      if (url === '/v1/lists') return Promise.resolve(json(listsPayload))
      if (url === '/v1/targets') return Promise.resolve(json(targetsPayload))
      if (url === '/v1/deployments/targets')
        return Promise.resolve(json({ targets: [] }))
      return Promise.reject(new Error(`unexpected request ${url}`))
    })
    const devices = useDevices()

    await devices.initialize()

    await expect(
      devices.register(
        'keenetic',
        'Manual router',
        'http://192.168.1.2',
        '',
        '',
      ),
    ).resolves.toBe(true)
    expect(
      fetchMock.mock.calls.filter(
        ([input, request]) =>
          String(input) === '/v1/devices' &&
          (request as RequestInit | undefined)?.method === 'POST',
      ),
    ).toHaveLength(1)
  })

  it('keeps disabling automatic delivery independent of a failed requirements read', async () => {
    fetchMock.mockImplementation((input: FetchInput, init?: RequestInit) => {
      const url = String(input)
      if (
        url === '/v1/devices/device-1/auto-delivery' &&
        init?.method === 'POST'
      )
        return Promise.resolve(json({}))
      if (url === '/v1/devices')
        return Promise.resolve(
          json({
            ...devicePayload,
            devices: [{ ...devicePayload.devices[0], auto_deliver: true }],
          }),
        )
      if (url === '/v1/lists') return Promise.resolve(json(listsPayload))
      if (url === '/v1/targets') return Promise.resolve(json(targetsPayload))
      if (url === '/v1/deployments/targets')
        return Promise.reject(new TypeError('unavailable'))
      return Promise.reject(new Error(`unexpected request ${url}`))
    })
    const devices = useDevices()

    await devices.initialize()
    await expect(devices.disable('device-1')).resolves.toBe(true)

    expect(
      fetchMock.mock.calls.filter(
        ([input, request]) =>
          String(input) === '/v1/devices/device-1/auto-delivery' &&
          (request as RequestInit | undefined)?.method === 'POST',
      ),
    ).toHaveLength(1)
  })
})
