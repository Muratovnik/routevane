import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'
import { defineComponent, h } from 'vue'

import {
  useDebouncedMutation,
  type DebouncedMutation,
} from '@/shared/model/useDebouncedMutation'

// The mutation cancels its unsent work when its owner unmounts, which only a
// mounted component has, so the subject is exercised through a host of its own.
const harness = <T>(
  build: () => DebouncedMutation<T>,
  captured: { mutation?: DebouncedMutation<T> },
) =>
  defineComponent({
    setup() {
      captured.mutation = build()
      return () => h('div')
    },
  })

type Harness<T> = {
  mutation: DebouncedMutation<T>
  unmount: () => Promise<void>
}

const mount = async <T>(
  build: () => DebouncedMutation<T>,
): Promise<Harness<T>> => {
  const captured: { mutation?: DebouncedMutation<T> } = {}
  const screen = await render(harness(build, captured))
  return { mutation: captured.mutation!, unmount: screen.unmount }
}

const setup = (
  request: (key: string, value: boolean) => Promise<boolean | undefined>,
): Promise<Harness<boolean>> => mount(() => useDebouncedMutation(request))

// Every case below drives the clock itself, so settling the promise chain has to
// go through the same fake clock rather than through a real task queue.
const settle = async (): Promise<void> => {
  await vi.advanceTimersByTimeAsync(0)
}

afterEach(() => {
  vi.useRealTimers()
})

describe('useDebouncedMutation', () => {
  it('coalesces a key within the debounce window and keeps the last intent', async () => {
    vi.useFakeTimers()
    const request = vi.fn(async (_key: string, value: boolean) => value)
    const { mutation, unmount } = await setup(request)
    mutation.seed('entry', true)

    mutation.mutate('entry', false)
    expect(mutation.getValue('entry')).toBe(false)
    mutation.mutate('entry', true)
    mutation.mutate('entry', false)
    await vi.advanceTimersByTimeAsync(249)
    expect(request).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1)
    await settle()
    expect(request).toHaveBeenCalledTimes(1)
    expect(request).toHaveBeenCalledWith('entry', false)
    expect(mutation.getState('entry')).toMatchObject({
      confirmed: false,
      status: 'idle',
      value: false,
    })
    await unmount()
  })

  it('keeps independent keys operable while one request is in flight', async () => {
    vi.useFakeTimers()
    const first = Promise.withResolvers<boolean>()
    const request = vi.fn((key: string, value: boolean) =>
      key === 'first' ? first.promise : Promise.resolve(value),
    )
    const { mutation, unmount } = await setup(request)
    mutation.seed('first', false)
    mutation.seed('second', false)

    mutation.mutate('first', true)
    await vi.advanceTimersByTimeAsync(250)
    expect(request).toHaveBeenCalledWith('first', true)
    expect(mutation.isPending('first')).toBe(true)

    mutation.mutate('second', true)
    expect(mutation.getValue('second')).toBe(true)
    await vi.advanceTimersByTimeAsync(250)
    await settle()
    expect(request).toHaveBeenCalledWith('second', true)

    first.resolve(true)
    await settle()
    expect(mutation.pending.value).toBe(false)
    await unmount()
  })

  it('does not let an older response overwrite a newer in-flight intent', async () => {
    vi.useFakeTimers()
    const first = Promise.withResolvers<boolean>()
    const request = vi.fn((_: string, value: boolean) =>
      request.mock.calls.length === 1 ? first.promise : Promise.resolve(value),
    )
    const { mutation, unmount } = await setup(request)
    mutation.seed('entry', false)

    mutation.mutate('entry', true)
    await vi.advanceTimersByTimeAsync(250)
    mutation.mutate('entry', false)
    expect(mutation.getValue('entry')).toBe(false)

    first.resolve(true)
    await settle()
    expect(mutation.getValue('entry')).toBe(false)
    expect(request).toHaveBeenCalledTimes(2)
    expect(request).toHaveBeenLastCalledWith('entry', false)

    await settle()
    expect(mutation.getState('entry')).toMatchObject({
      confirmed: false,
      status: 'idle',
      value: false,
    })
    await unmount()
  })

  it('adapts a richer response and reports the superseded success before retrying the latest intent', async () => {
    vi.useFakeTimers()
    const first = Promise.withResolvers<{ enabled: boolean }>()
    const second = Promise.withResolvers<{ enabled: boolean }>()
    const events: { confirmed: boolean; superseded: boolean }[] = []
    const request = vi.fn((_: string, _value: boolean) =>
      request.mock.calls.length === 1 ? first.promise : second.promise,
    )
    const { mutation, unmount } = await mount(() =>
      useDebouncedMutation<boolean, { enabled: boolean }>(request, {
        onSuccess: ({ confirmed, superseded }) =>
          events.push({ confirmed, superseded }),
        resolve: (result, attempted) => result?.enabled ?? attempted,
      }),
    )
    mutation.seed('entry', false)

    mutation.mutate('entry', true)
    await vi.advanceTimersByTimeAsync(250)
    mutation.mutate('entry', false)
    first.resolve({ enabled: true })
    await settle()

    expect(events).toEqual([{ confirmed: true, superseded: true }])
    expect(mutation.getValue('entry')).toBe(false)
    expect(request).toHaveBeenCalledTimes(2)

    second.resolve({ enabled: false })
    await settle()
    expect(events).toEqual([
      { confirmed: true, superseded: true },
      { confirmed: false, superseded: false },
    ])
    expect(mutation.getState('entry')).toMatchObject({
      confirmed: false,
      status: 'idle',
      value: false,
    })
    await unmount()
  })

  it('rolls back one failed key and retries it without affecting another key', async () => {
    vi.useFakeTimers()
    const offline = Promise.withResolvers<boolean>()
    const request = vi.fn((_: string, value: boolean) =>
      request.mock.calls.length === 1
        ? offline.promise
        : Promise.resolve(value),
    )
    const { mutation, unmount } = await setup(request)
    mutation.seed('failed', false)
    mutation.seed('untouched', true)
    mutation.mutate('failed', true)
    await vi.advanceTimersByTimeAsync(250)
    offline.reject(new Error('offline'))
    await settle()

    expect(mutation.getValue('failed')).toBe(false)
    expect(mutation.getState('failed')).toMatchObject({ status: 'error' })
    expect(mutation.getValue('untouched')).toBe(true)

    mutation.retry('failed')
    expect(mutation.getValue('failed')).toBe(true)
    await vi.advanceTimersByTimeAsync(250)
    await settle()
    expect(mutation.getState('failed')).toMatchObject({
      confirmed: true,
      error: null,
      status: 'idle',
      value: true,
    })
    await unmount()
  })

  it('cancels unsent work when its owner unmounts', async () => {
    vi.useFakeTimers()
    const request = vi.fn(async (_key: string, value: boolean) => value)
    const { mutation, unmount } = await setup(request)
    mutation.seed('entry', false)
    mutation.mutate('entry', true)
    await unmount()

    await vi.advanceTimersByTimeAsync(250)
    await settle()
    expect(request).not.toHaveBeenCalled()
    expect(mutation.pending.value).toBe(false)
  })

  it('flushes one queued key and cancels another without crossing their state', async () => {
    vi.useFakeTimers()
    const request = vi.fn(async (_key: string, value: boolean) => value)
    const { mutation, unmount } = await setup(request)
    mutation.seed('flushed', false)
    mutation.seed('cancelled', false)
    mutation.mutate('flushed', true)
    mutation.mutate('cancelled', true)

    mutation.flush('flushed')
    await settle()
    expect(request).toHaveBeenCalledTimes(1)
    expect(request).toHaveBeenCalledWith('flushed', true)

    mutation.cancel('cancelled')
    await vi.advanceTimersByTimeAsync(250)
    await settle()
    expect(request).toHaveBeenCalledTimes(1)
    await unmount()
  })
})
