import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useSettings } from '@/features/settings/model/useSettings'

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

describe('useSettings prerequisite reads', () => {
  it('keeps the server rule unknown after a failed first read and refuses a write', async () => {
    fetchMock.mockRejectedValue(new TypeError('unavailable'))
    const settings = useSettings()

    await settings.initialize()

    expect(settings.readState.value).toBe('failed')
    expect(settings.refreshInterval.value).toBeNull()
    await expect(settings.setRefreshInterval('daily')).resolves.toBe(false)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/v1/settings')
  })

  it('retries the read and applies a known value, while suppressing duplicate writes', async () => {
    let read = 0
    let release!: () => void
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    fetchMock.mockImplementation((input: string) => {
      if (input === '/v1/settings') {
        read += 1
        return Promise.resolve(
          read === 1
            ? json({ error: 'unavailable' }, 503)
            : json({ refresh_interval: 'daily' }),
        )
      }
      if (input === '/v1/settings/update') {
        return held.then(() => json({ refresh_interval: 'weekly' }))
      }
      return Promise.reject(new Error(`unexpected request ${input}`))
    })
    const settings = useSettings()

    await settings.initialize()
    await expect(settings.retry()).resolves.toBe(true)
    expect(settings.refreshInterval.value).toBe('daily')

    const firstWrite = settings.setRefreshInterval('weekly')
    await Promise.resolve()
    expect(settings.writeState.value).toBe('saving')
    await expect(settings.setRefreshInterval('off')).resolves.toBe(false)
    release()
    await expect(firstWrite).resolves.toBe(true)

    expect(settings.refreshInterval.value).toBe('weekly')
    expect(
      fetchMock.mock.calls.filter(([input]) => input === '/v1/settings/update'),
    ).toHaveLength(1)
  })

  it('keeps the last confirmed value and reports a write failure', async () => {
    let failWrite = true
    fetchMock.mockImplementation((input: string) => {
      if (input === '/v1/settings')
        return Promise.resolve(json({ refresh_interval: 'off' }))
      if (input === '/v1/settings/update')
        return failWrite
          ? Promise.reject(new TypeError('offline'))
          : Promise.resolve(json({ refresh_interval: 'daily' }))
      return Promise.reject(new Error(`unexpected request ${input}`))
    })
    const settings = useSettings()

    await settings.initialize()
    await expect(settings.setRefreshInterval('weekly')).resolves.toBe(false)

    expect(settings.refreshInterval.value).toBe('off')
    expect(settings.readState.value).toBe('ready')
    expect(settings.writeState.value).toBe('failed')

    failWrite = false
    await expect(settings.setRefreshInterval('daily')).resolves.toBe(true)
    expect(settings.refreshInterval.value).toBe('daily')
    expect(settings.writeState.value).toBe('idle')
  })

  it('keeps a confirmed rule visible when a later read fails', async () => {
    let reads = 0
    fetchMock.mockImplementation((input: string) => {
      if (input !== '/v1/settings')
        return Promise.reject(new Error(`unexpected request ${input}`))
      reads += 1
      return reads === 1
        ? Promise.resolve(json({ refresh_interval: 'weekly' }))
        : Promise.reject(new TypeError('offline'))
    })
    const settings = useSettings()

    await settings.initialize()
    await expect(settings.retry()).resolves.toBe(false)

    expect(settings.readState.value).toBe('failed')
    expect(settings.refreshInterval.value).toBe('weekly')
  })
})
