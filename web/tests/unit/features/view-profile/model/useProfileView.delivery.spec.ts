import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  onTestFinished,
  vi,
} from 'vitest'
import { effectScope } from 'vue'

import { usePublishedProfile } from '@/entities/profile-build/model/publishedProfile'
import { useProfileView } from '@/features/view-profile/model/useProfileView'
import { invalidateCatalogCache } from '@/shared/api/catalog'
import type { DeviceCard } from '@/shared/api/devices'

const TIMESTAMP = '2026-09-03T09:00:00Z'

type Scenario = {
  autoDeliver: boolean
  holdRequirements: Promise<void> | undefined
  needsCredential: boolean
  registryStatus: number
  requirementsStatus: number
  secretStoreAvailable: boolean
}

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const deviceCard = (autoDeliver = true): DeviceCard => ({
  id: 'device-1',
  targetID: 'target-1',
  targetTitle: 'A configured target',
  name: 'A saved connection',
  address: 'http://192.0.2.1',
  account: 'operator',
  interfaceName: '',
  autoDeliver,
  deployable: true,
})

const installScenario = (overrides: Partial<Scenario> = {}) => {
  const scenario: Scenario = {
    autoDeliver: true,
    holdRequirements: undefined,
    needsCredential: true,
    registryStatus: 200,
    requirementsStatus: 200,
    secretStoreAvailable: true,
    ...overrides,
  }
  const fetchMock = vi.fn(async (input: unknown) => {
    switch (String(input)) {
      case '/v1/profiles/profile-1':
        return json({
          profile: {
            id: 'profile-1',
            name: 'Saved profile',
            lists: [],
            categories: [],
            exclusions: [],
            created_at: TIMESTAMP,
            updated_at: TIMESTAMP,
          },
          outputs: [
            {
              id: 'output-1',
              target_id: 'target-1',
              target_title: 'A configured target',
              device_id: 'device-1',
              created_at: TIMESTAMP,
              latest: {
                id: 'artifact-1',
                snapshot_id: 'snapshot-1',
                size_bytes: 14,
                content_type: 'text/plain',
                content_created_at: TIMESTAMP,
              },
            },
          ],
          resolved: [],
          missing_categories: [],
          schedule: { interval: 'daily', effective: 'daily' },
        })
      case '/v1/lists':
        return json({ lists: [], list_details: [], categories: [] })
      case '/v1/targets':
        return json({ targets: [] })
      case '/v1/export-formats':
        return json({ formats: [] })
      case '/v1/deployments/targets':
        await scenario.holdRequirements
        return json(
          {
            targets: [
              {
                target_id: 'target-1',
                title: 'A configured target',
                deployer_id: 'test-deployer',
                requirements: {
                  address_label: 'Destination',
                  address_example: 'http://192.0.2.1',
                  needs_credential: scenario.needsCredential,
                  needs_interface: false,
                },
              },
            ],
          },
          scenario.requirementsStatus,
        )
      case '/v1/devices':
        return json(
          {
            devices: [
              {
                id: 'device-1',
                target_id: 'target-1',
                target_title: 'A configured target',
                name: 'A saved connection',
                address: 'http://192.0.2.1',
                account: scenario.needsCredential ? 'operator' : '',
                auto_deliver: scenario.autoDeliver,
                deployable: true,
              },
            ],
            secret_store_available: scenario.secretStoreAvailable,
          },
          scenario.registryStatus,
        )
      default:
        throw new Error(`Unexpected request ${String(input)}`)
    }
  })
  vi.stubGlobal('fetch', fetchMock)
  const scope = effectScope()
  const view = scope.run(() => useProfileView(() => 'profile-1'))!
  onTestFinished(() => scope.stop())
  return { scenario, view }
}

beforeEach(() => {
  invalidateCatalogCache()
  usePublishedProfile().clear()
})

afterEach(() => vi.unstubAllGlobals())

describe('profile automatic delivery capability', () => {
  it.each([
    {
      needsCredential: true,
      secretStoreAvailable: false,
      autoDeliver: false,
      expected: 'unavailable',
    },
    {
      needsCredential: true,
      secretStoreAvailable: false,
      autoDeliver: true,
      expected: 'unavailable',
    },
    {
      needsCredential: true,
      secretStoreAvailable: true,
      autoDeliver: false,
      expected: 'available',
    },
    {
      needsCredential: true,
      secretStoreAvailable: true,
      autoDeliver: true,
      expected: 'available',
    },
    {
      needsCredential: false,
      secretStoreAvailable: false,
      autoDeliver: false,
      expected: 'available',
    },
    {
      needsCredential: false,
      secretStoreAvailable: false,
      autoDeliver: true,
      expected: 'available',
    },
  ])(
    'uses requirements and platform support independently of stored consent: %j',
    async (test) => {
      const { view } = installScenario(test)
      await view.initialize()

      expect(view.automaticDelivery(view.devices.value[0]!)).toBe(test.expected)
      expect(view.deployable(view.outputs.value[0]!)).toBe(true)
    },
  )

  it('does not assume a password-free target when requirements were not read', async () => {
    const { view } = installScenario({ requirementsStatus: 503 })
    await view.initialize()

    expect(view.automaticDelivery(view.devices.value[0]!)).toBe('unknown')
  })

  it('forgets previously confirmed requirements when the next read fails', async () => {
    const { scenario, view } = installScenario()
    await view.initialize()
    expect(view.automaticDelivery(deviceCard())).toBe('available')

    scenario.requirementsStatus = 503
    await view.initialize()

    expect(view.automaticDelivery(deviceCard())).toBe('unknown')
  })

  it('does not reuse a previous credential-store claim after a registry failure', async () => {
    const { scenario, view } = installScenario()
    await view.initialize()
    expect(view.automaticDelivery(deviceCard())).toBe('available')

    scenario.registryStatus = 503
    await view.initialize()

    expect(view.automaticDelivery(deviceCard())).toBe('unknown')
  })

  it('does not let a late registry restore superseded platform support', async () => {
    const held = Promise.withResolvers<undefined>()
    const { scenario, view } = installScenario({
      holdRequirements: held.promise,
    })
    const earlier = view.initialize()
    scenario.holdRequirements = undefined
    scenario.secretStoreAvailable = false
    await view.initialize()
    expect(view.automaticDelivery(deviceCard())).toBe('unavailable')

    held.resolve(undefined)
    await earlier

    expect(view.automaticDelivery(deviceCard())).toBe('unavailable')
  })
})
