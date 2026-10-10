import { afterEach, expect, it, onTestFinished, vi } from 'vitest'
import { effectScope, ref } from 'vue'

import {
  MANUAL_CONNECTION,
  useDeploymentConnections,
} from '@/features/send-artifact/model/useDeploymentConnections'

const registry = () => ({
  devices: [
    {
      id: 'first',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      name: 'First router',
      address: 'http://192.168.1.1',
      account: 'admin',
      interface: 'Wireguard0',
      auto_deliver: false,
      deployable: true,
    },
    {
      id: 'bound',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      name: 'Bound router',
      address: 'http://192.168.1.2',
      account: 'operator',
      interface: 'Wireguard1',
      auto_deliver: true,
      deployable: true,
    },
    {
      id: 'other-target',
      target_id: 'singbox',
      target_title: 'sing-box',
      name: 'Local rules',
      address: 'file:///tmp/config.json',
      account: '',
      interface: '',
      auto_deliver: true,
      deployable: true,
    },
    {
      id: 'unsupported',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      name: 'Unavailable adapter',
      address: 'http://192.168.1.3',
      account: 'admin',
      interface: 'Wireguard0',
      auto_deliver: true,
      deployable: false,
    },
  ],
  secret_store_available: true,
})

const json = (payload: unknown): Response =>
  new Response(JSON.stringify(payload), { status: 200 })

const selection = (
  preferredID = '',
  targetID: () => string = () => 'keenetic',
) => {
  const scope = effectScope()
  onTestFinished(() => scope.stop())
  return scope.run(() => useDeploymentConnections(targetID, () => preferredID))!
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it('selects the bound compatible access and only reads the registry', async () => {
  const fetchMock = vi.fn(async () => json(registry()))
  vi.stubGlobal('fetch', fetchMock)
  const connections = selection('bound')

  await connections.initialize()

  expect(connections.compatible.value.map((device) => device.id)).toEqual([
    'first',
    'bound',
  ])
  expect(connections.selected.value).toMatchObject({
    id: 'bound',
    address: 'http://192.168.1.2',
    account: 'operator',
    interfaceName: 'Wireguard1',
  })
  expect(connections.manual.value).toBe(false)
  expect(connections.storedCredential.value).toBe(true)
  expect(fetchMock).toHaveBeenCalledExactlyOnceWith('/v1/devices', {
    method: 'GET',
  })
  connections.selectedID.value = 'first'
  expect(connections.storedCredential.value).toBe(false)
})

it.each(['', 'deleted', 'other-target', 'unsupported'])(
  'keeps manual entry when the preferred registration is %s',
  async (preferredID) => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => json(registry())),
    )
    const connections = selection(preferredID)
    await connections.initialize()
    expect(connections.manual.value).toBe(true)
    expect(connections.selected.value).toBeNull()
    expect(connections.preferredUnavailable.value).toBe(preferredID !== '')
  },
)

it('retains manual entry after a registry failure and does not infer an available credential', async () => {
  const fetchMock = vi.fn(async (): Promise<Response> => {
    throw new TypeError('registry unavailable')
  })
  vi.stubGlobal('fetch', fetchMock)
  const connections = selection('bound')
  await connections.initialize()
  expect(connections.state.value).toBe('failed')
  expect(connections.manual.value).toBe(true)
  expect(connections.storedCredential.value).toBe(false)
  fetchMock.mockImplementation(async () =>
    json({ ...registry(), secret_store_available: false }),
  )
  await connections.initialize()
  expect(connections.manual.value).toBe(true)
  connections.selectedID.value = 'bound'
  expect(connections.selectedID.value).toBe('bound')
  expect(connections.storedCredential.value).toBe(false)
})

it('does not replace an explicit selection with a late default or an older registry read', async () => {
  const older = Promise.withResolvers<Response>()
  const newer = Promise.withResolvers<Response>()
  const fetchMock = vi
    .fn()
    .mockReturnValueOnce(older.promise)
    .mockReturnValueOnce(newer.promise)
  vi.stubGlobal('fetch', fetchMock)
  const connections = selection('bound')
  const firstLoad = connections.initialize()
  const secondLoad = connections.initialize()
  connections.selectedID.value = 'first'
  newer.resolve(json(registry()))
  await secondLoad
  expect(connections.selectedID.value).toBe('first')
  older.resolve(json({ devices: [], secret_store_available: false }))
  await firstLoad
  expect(connections.compatible.value).toHaveLength(2)
  expect(connections.selectedID.value).toBe('first')

  connections.selectedID.value = MANUAL_CONNECTION
  fetchMock.mockResolvedValue(json(registry()))
  await connections.initialize()
  expect(connections.manual.value).toBe(true)
})

it('ignores a reply for a target that left the screen', async () => {
  const pending = Promise.withResolvers<Response>()
  vi.stubGlobal(
    'fetch',
    vi.fn(() => pending.promise),
  )
  const targetID = ref('keenetic')
  const connections = selection('bound', () => targetID.value)
  const loading = connections.initialize()
  targetID.value = 'singbox'
  pending.resolve(json(registry()))
  await loading
  expect(connections.compatible.value).toEqual([])
  expect(connections.selected.value).toBeNull()
})

it('ignores a registry reply after disposal', async () => {
  const pending = Promise.withResolvers<Response>()
  vi.stubGlobal(
    'fetch',
    vi.fn(() => pending.promise),
  )
  const scope = effectScope()
  const connections = scope.run(() =>
    useDeploymentConnections(
      () => 'keenetic',
      () => 'bound',
    ),
  )!
  const loading = connections.initialize()
  scope.stop()
  pending.resolve(json(registry()))
  await loading
  expect(connections.compatible.value).toEqual([])
  expect(connections.selectedID.value).toBe(MANUAL_CONNECTION)
})
