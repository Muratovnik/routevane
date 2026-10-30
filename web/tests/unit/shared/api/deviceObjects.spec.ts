/**
 * What a format left on routers, as the retirement question reads it, and the
 * opt-in a retirement carries to remove it (ADR 0060).
 */
import { beforeEach, describe, expect, it } from 'vitest'

import {
  loadOutputDeviceObjects,
  loadProfileDeviceObjects,
} from '@/shared/api/deviceObjects'
import { removeOutput } from '@/shared/api/outputs'
import { deleteProfile } from '@/shared/api/profiles'

import { answer, fetchMock, stubFetch } from './support/fixtures'

beforeEach(stubFetch)

const sentBody = (call: number): unknown =>
  JSON.parse(String((fetchMock.mock.calls[call]?.[1] as RequestInit).body))

describe('device objects', () => {
  it('names each router, what would go from it, and why one cannot be cleaned', async () => {
    answer({
      device_objects: [
        {
          output_id: 'output-1',
          target_id: 'keenetic-dns',
          endpoint: 'http://192.0.2.1',
          interface: 'Wireguard0',
          connection: {
            id: 'device-1',
            name: 'Home router',
            credential_stored: true,
          },
          cleanable: true,
          fqdn_groups: ['routevane-a', 'routevane-b'],
        },
        {
          output_id: 'output-2',
          target_id: 'keenetic',
          endpoint: 'http://192.0.2.2',
          interface: 'Wireguard0',
          cleanable: false,
          blocked_by: 'connection_missing',
          routes: { claimed: 5, removable: 3, shared: 1, preexisting: 1 },
        },
      ],
      cleanable: false,
    })

    await expect(loadProfileDeviceObjects('profile-1')).resolves.toEqual({
      cleanable: false,
      surfaces: [
        {
          outputID: 'output-1',
          targetID: 'keenetic-dns',
          endpoint: 'http://192.0.2.1',
          interfaceName: 'Wireguard0',
          connectionName: 'Home router',
          cleanable: true,
          blockedBy: '',
          fqdnGroups: 2,
          routes: 0,
        },
        {
          outputID: 'output-2',
          targetID: 'keenetic',
          endpoint: 'http://192.0.2.2',
          interfaceName: 'Wireguard0',
          connectionName: '',
          cleanable: false,
          blockedBy: 'connection_missing',
          fqdnGroups: 0,
          routes: 3,
        },
      ],
    })
    expect(String(fetchMock.mock.calls[0]?.[0])).toBe(
      '/v1/profiles/profile-1/device-objects',
    )
  })

  it('refuses a reason this build does not know rather than guessing one', async () => {
    answer({
      device_objects: [
        {
          output_id: 'output-1',
          target_id: 'keenetic',
          endpoint: 'http://192.0.2.1',
          cleanable: false,
          blocked_by: 'moon_phase',
        },
      ],
      cleanable: false,
    })
    await expect(loadOutputDeviceObjects('output-1')).rejects.toThrow()
  })

  it('carries the opt-in only when it was chosen', async () => {
    answer({ id: 'output-1', removed_at: '2026-10-30T10:00:00Z' })
    await removeOutput('output-1')
    expect(sentBody(0)).toEqual({})

    answer({ id: 'output-1', removed_at: '2026-10-30T10:00:00Z' })
    await removeOutput('output-1', true)
    expect(sentBody(1)).toEqual({ remove_from_devices: true })

    answer({ id: 'profile-1', deleted_at: '2026-10-30T10:00:00Z' })
    await deleteProfile('profile-1', true)
    expect(sentBody(2)).toEqual({ remove_from_devices: true })
  })
})
