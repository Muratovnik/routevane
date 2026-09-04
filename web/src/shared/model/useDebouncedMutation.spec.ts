import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'

import {
  useDebouncedMutation,
  type DebouncedMutation,
} from './useDebouncedMutation'

type Harness = {
  mutation: DebouncedMutation<boolean>
  wrapper: ReturnType<typeof mount>
}

function setup(
  request: (key: string, value: boolean) => Promise<boolean | undefined>,
): Harness {
  let mutation!: DebouncedMutation<boolean>
  const component = defineComponent({
    setup() {
      mutation = useDebouncedMutation(request)
      return () => h('div')
    },
  })
  const wrapper = mount(component)
  return { mutation, wrapper }
}

afterEach(() => {
  vi.useRealTimers()
})

describe('useDebouncedMutation', () => {
  it('coalesces a key within the debounce window and keeps the last intent', async () => {
    vi.useFakeTimers()
    const request = vi.fn(async (_key: string, value: boolean) => value)
    const { mutation, wrapper } = setup(request)
    mutation.seed('entry', true)

    mutation.mutate('entry', false)
    expect(mutation.getValue('entry')).toBe(false)
    mutation.mutate('entry', true)
    mutation.mutate('entry', false)
    await vi.advanceTimersByTimeAsync(249)
    expect(request).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(request).toHaveBeenCalledTimes(1)
    expect(request).toHaveBeenCalledWith('entry', false)
    expect(mutation.getState('entry')).toMatchObject({
      confirmed: false,
      status: 'idle',
      value: false,
    })
    wrapper.unmount()
  })

  it('keeps independent keys operable while one request is in flight', async () => {
    vi.useFakeTimers()
    let release!: (value: boolean) => void
    const first = new Promise<boolean>((resolve) => {
      release = resolve
    })
    const request = vi.fn((key: string, value: boolean) =>
      key === 'first' ? first : Promise.resolve(value),
    )
    const { mutation, wrapper } = setup(request)
    mutation.seed('first', false)
    mutation.seed('second', false)

    mutation.mutate('first', true)
    await vi.advanceTimersByTimeAsync(250)
    expect(request).toHaveBeenCalledWith('first', true)
    expect(mutation.isPending('first')).toBe(true)

    mutation.mutate('second', true)
    expect(mutation.getValue('second')).toBe(true)
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    expect(request).toHaveBeenCalledWith('second', true)

    release(true)
    await flushPromises()
    expect(mutation.pending.value).toBe(false)
    wrapper.unmount()
  })

  it('does not let an older response overwrite a newer in-flight intent', async () => {
    vi.useFakeTimers()
    let releaseFirst!: (value: boolean) => void
    const first = new Promise<boolean>((resolve) => {
      releaseFirst = resolve
    })
    const request = vi.fn((_: string, value: boolean) =>
      request.mock.calls.length === 1 ? first : Promise.resolve(value),
    )
    const { mutation, wrapper } = setup(request)
    mutation.seed('entry', false)

    mutation.mutate('entry', true)
    await vi.advanceTimersByTimeAsync(250)
    mutation.mutate('entry', false)
    expect(mutation.getValue('entry')).toBe(false)

    releaseFirst(true)
    await flushPromises()
    expect(mutation.getValue('entry')).toBe(false)
    expect(request).toHaveBeenCalledTimes(2)
    expect(request).toHaveBeenLastCalledWith('entry', false)

    await flushPromises()
    expect(mutation.getState('entry')).toMatchObject({
      confirmed: false,
      status: 'idle',
      value: false,
    })
    wrapper.unmount()
  })

  it('adapts a richer response and reports the superseded success before retrying the latest intent', async () => {
    vi.useFakeTimers()
    let releaseFirst!: (result: { enabled: boolean }) => void
    const first = new Promise<{ enabled: boolean }>((resolve) => {
      releaseFirst = resolve
    })
    let releaseSecond!: (result: { enabled: boolean }) => void
    const second = new Promise<{ enabled: boolean }>((resolve) => {
      releaseSecond = resolve
    })
    const events: { confirmed: boolean; superseded: boolean }[] = []
    const request = vi.fn((_: string, _value: boolean) =>
      request.mock.calls.length === 1 ? first : second,
    )
    let mutation!: DebouncedMutation<boolean>
    const component = defineComponent({
      setup() {
        mutation = useDebouncedMutation<boolean, { enabled: boolean }>(
          request,
          {
            onSuccess: ({ confirmed, superseded }) =>
              events.push({ confirmed, superseded }),
            resolve: (result, attempted) => result?.enabled ?? attempted,
          },
        )
        return () => h('div')
      },
    })
    const wrapper = mount(component)
    mutation.seed('entry', false)

    mutation.mutate('entry', true)
    await vi.advanceTimersByTimeAsync(250)
    mutation.mutate('entry', false)
    releaseFirst({ enabled: true })
    await flushPromises()

    expect(events).toEqual([{ confirmed: true, superseded: true }])
    expect(mutation.getValue('entry')).toBe(false)
    expect(request).toHaveBeenCalledTimes(2)

    releaseSecond({ enabled: false })
    await flushPromises()
    expect(events).toEqual([
      { confirmed: true, superseded: true },
      { confirmed: false, superseded: false },
    ])
    expect(mutation.getState('entry')).toMatchObject({
      confirmed: false,
      status: 'idle',
      value: false,
    })
    wrapper.unmount()
  })

  it('rolls back one failed key and retries it without affecting another key', async () => {
    vi.useFakeTimers()
    let reject!: (reason: unknown) => void
    let attempt = 0
    const request = vi.fn((_: string, value: boolean) => {
      attempt += 1
      if (attempt === 1)
        return new Promise<boolean>((_, fail) => {
          reject = fail
        })
      return Promise.resolve(value)
    })
    const { mutation, wrapper } = setup(request)
    mutation.seed('failed', false)
    mutation.seed('untouched', true)
    mutation.mutate('failed', true)
    await vi.advanceTimersByTimeAsync(250)
    reject(new Error('offline'))
    await flushPromises()

    expect(mutation.getValue('failed')).toBe(false)
    expect(mutation.getState('failed')).toMatchObject({ status: 'error' })
    expect(mutation.getValue('untouched')).toBe(true)

    mutation.retry('failed')
    expect(mutation.getValue('failed')).toBe(true)
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    expect(mutation.getState('failed')).toMatchObject({
      confirmed: true,
      error: null,
      status: 'idle',
      value: true,
    })
    wrapper.unmount()
  })

  it('cancels unsent work when its owner unmounts', async () => {
    vi.useFakeTimers()
    const request = vi.fn(async (_key: string, value: boolean) => value)
    const { mutation, wrapper } = setup(request)
    mutation.seed('entry', false)
    mutation.mutate('entry', true)
    wrapper.unmount()

    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    expect(request).not.toHaveBeenCalled()
    expect(mutation.pending.value).toBe(false)
  })

  it('flushes one queued key and cancels another without crossing their state', async () => {
    vi.useFakeTimers()
    const request = vi.fn(async (_key: string, value: boolean) => value)
    const { mutation, wrapper } = setup(request)
    mutation.seed('flushed', false)
    mutation.seed('cancelled', false)
    mutation.mutate('flushed', true)
    mutation.mutate('cancelled', true)

    mutation.flush('flushed')
    await flushPromises()
    expect(request).toHaveBeenCalledTimes(1)
    expect(request).toHaveBeenCalledWith('flushed', true)

    mutation.cancel('cancelled')
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    expect(request).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
