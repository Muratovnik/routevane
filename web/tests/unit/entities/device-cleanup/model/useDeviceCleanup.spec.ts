import { beforeEach, describe, expect, it } from 'vitest'

import {
  cleanupRefusalKey,
  useDeviceCleanup,
} from '@/entities/device-cleanup/model/useDeviceCleanup'

import { answer, stubFetch } from '../../../shared/api/support/fixtures'

beforeEach(stubFetch)

const surface = (cleanable: boolean) => ({
  output_id: 'output-1',
  target_id: 'keenetic-dns',
  endpoint: 'http://192.0.2.1',
  cleanable,
  ...(cleanable ? {} : { blocked_by: 'credential_missing' }),
  fqdn_groups: ['routevane-a'],
})

const settled = async (state: { value: string }): Promise<void> => {
  await expect.poll(() => state.value).not.toBe('loading')
}

describe('useDeviceCleanup', () => {
  // A choice the routers can no longer honour does not stay switched on: the
  // act would quietly leave them as they are while the switch said otherwise.
  it('drops the choice once a read shows a router that cannot be cleaned', async () => {
    answer({ device_objects: [surface(true)], cleanable: true })
    const cleanup = useDeviceCleanup()
    cleanup.open({ kind: 'output', id: 'output-1' })
    await settled(cleanup.state)
    cleanup.chosen.value = true
    expect(cleanup.requested.value).toBe(true)

    answer({ device_objects: [surface(false)], cleanable: false })
    await cleanup.reread()

    expect(cleanup.chosen.value).toBe(false)
    expect(cleanup.requested.value).toBe(false)
  })

  it('asks nothing of the routers when what they hold could not be read', async () => {
    answer({ error: 'operation failed' }, 500)
    const cleanup = useDeviceCleanup()
    cleanup.open({ kind: 'profile', id: 'profile-1' })
    await settled(cleanup.state)
    cleanup.chosen.value = true

    expect(cleanup.state.value).toBe('failed')
    expect(cleanup.requested.value).toBe(false)
  })
})

describe('cleanupRefusalKey', () => {
  it.each([
    ['device_cleanup_unavailable', 'cleanup.error.unavailable'],
    ['device_cleanup_failed', 'cleanup.error.failed'],
    ['rollback_failed', 'error.rollback_failed'],
    ['backup_unavailable', 'error.backup_unavailable'],
    ['device_auth_rejected', 'error.device_auth_rejected'],
    ['profile_not_archived', ''],
  ])('names %s as %s', (code, key) => {
    expect(cleanupRefusalKey(code)).toBe(key)
  })
})
