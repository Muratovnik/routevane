import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useDevices } from '@/features/devices/model/useDevices'

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

const servicesPayload = {
  services: [],
  service_details: [],
  categories: [],
}

const targetsPayload = {
  targets: [
    {
      id: 'keenetic',
      title: 'Keenetic',
      kind: 'router',
      profile_key: 'keenetic-bat-ipv4-v1', // betterleaks:allow -- public fixture identifier
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
    fetchMock.mockImplementation((input: string | URL | Request) => {
      const url = String(input)
      if (url === '/v1/devices') return Promise.resolve(json(devicePayload))
      if (url === '/v1/services') return Promise.resolve(json(servicesPayload))
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
    fetchMock.mockImplementation((input: string | URL | Request) => {
      const url = String(input)
      if (url === '/v1/devices') return Promise.resolve(json(devicePayload))
      if (url === '/v1/services')
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
})
