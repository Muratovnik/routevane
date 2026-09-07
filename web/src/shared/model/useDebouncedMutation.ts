import {
  computed,
  getCurrentInstance,
  onUnmounted,
  shallowReactive,
  type ComputedRef,
} from 'vue'

export type DebouncedMutationStatus = 'idle' | 'queued' | 'in-flight' | 'error'

export type DebouncedMutationState<Value> = {
  confirmed: Value | undefined
  error: unknown | null
  inFlight: boolean
  queued: boolean
  status: DebouncedMutationStatus
  value: Value | undefined
}

type Entry<Key, Value> = {
  confirmed: Value | undefined
  error: unknown | null
  hasConfirmed: boolean
  hasOptimistic: boolean
  inFlight: boolean
  inFlightVersion: number | null
  intentVersion: number
  key: Key
  optimistic: Value | undefined
  queued: boolean
  retryValue: Value | undefined
  timer: ReturnType<typeof setTimeout> | undefined
}

export type DebouncedMutationOptions<Value, Result = Value> = {
  debounce?: number
  equals?: (left: Value, right: Value) => boolean
  /** Convert the transport result into the value this key confirms. */
  resolve?: (result: Result | undefined, attempted: Value, key: string) => Value
  /** Observe a successful response without changing the mutation state API. */
  onSuccess?: (event: {
    attempted: Value
    confirmed: Value
    key: string
    result: Result | undefined
    superseded: boolean
  }) => void
}

export type DebouncedMutation<Value> = {
  cancel: (key?: string) => void
  errorFor: (key: string) => ComputedRef<unknown | null>
  flush: (key?: string) => void
  getState: (key: string) => DebouncedMutationState<Value>
  getValue: (key: string) => Value | undefined
  hasPending: ComputedRef<boolean>
  isPending: (key: string) => boolean
  mutate: (key: string, value: Value) => void
  optimistic: (key: string, fallback?: Value) => ComputedRef<Value | undefined>
  pending: ComputedRef<boolean>
  pendingCount: ComputedRef<number>
  pendingFor: (key: string) => ComputedRef<boolean>
  retry: (key: string) => void
  seed: (key: string, value: Value) => void
  set: (key: string, value: Value) => void
  stateFor: (key: string) => ComputedRef<DebouncedMutationState<Value>>
  states: ComputedRef<ReadonlyMap<string, DebouncedMutationState<Value>>>
  valueFor: (key: string, fallback?: Value) => ComputedRef<Value | undefined>
}

const defaultDebounce = 250

/**
 * Keep small, independent optimistic writes from making an entire card wait.
 *
 * Each key has one confirmed value and one latest intent. A timer coalesces
 * changes before the request starts; while a request is in flight, the latest
 * intent stays visible and is sent after the older request settles. A response
 * therefore can only confirm the request that produced it and never overwrite a
 * newer intent.
 */
export function useDebouncedMutation<Value, Result = Value>(
  mutateRequest: (key: string, value: Value) => Promise<Result | undefined>,
  options: DebouncedMutationOptions<Value, Result> = {},
): DebouncedMutation<Value> {
  const entries = shallowReactive(new Map<string, Entry<string, Value>>())
  const debounce = Math.max(0, options.debounce ?? defaultDebounce)
  const equals = options.equals ?? Object.is
  let disposed = false

  function createEntry(key: string, value?: Value): Entry<string, Value> {
    return shallowReactive({
      confirmed: value,
      error: null,
      hasConfirmed: value !== undefined,
      hasOptimistic: value !== undefined,
      inFlight: false,
      inFlightVersion: null,
      intentVersion: 0,
      key,
      optimistic: value,
      queued: false,
      retryValue: undefined,
      timer: undefined,
    }) as Entry<string, Value>
  }

  function ensure(key: string): Entry<string, Value> {
    const existing = entries.get(key)
    if (existing !== undefined) return existing
    const created = createEntry(key)
    entries.set(key, created)
    return created
  }

  function statusOf(entry: Entry<string, Value>): DebouncedMutationStatus {
    if (entry.error !== null && !entry.queued && !entry.inFlight) return 'error'
    if (entry.inFlight) return 'in-flight'
    if (entry.queued) return 'queued'
    return 'idle'
  }

  function snapshot(
    entry: Entry<string, Value>,
  ): DebouncedMutationState<Value> {
    return {
      confirmed: entry.hasConfirmed ? entry.confirmed : undefined,
      error: entry.error,
      inFlight: entry.inFlight,
      queued: entry.queued,
      status: statusOf(entry),
      value: entry.hasOptimistic ? entry.optimistic : undefined,
    }
  }

  function clearTimer(entry: Entry<string, Value>): void {
    if (entry.timer === undefined) return
    clearTimeout(entry.timer)
    entry.timer = undefined
  }

  function shouldContinue(key: string, entry: Entry<string, Value>): boolean {
    return !disposed && entries.get(key) === entry
  }

  function dispatch(key: string, entry: Entry<string, Value>): void {
    if (
      !shouldContinue(key, entry) ||
      !entry.queued ||
      entry.inFlight ||
      !entry.hasOptimistic
    )
      return

    const attempted = entry.optimistic as Value
    const version = entry.intentVersion
    entry.queued = false
    entry.inFlight = true
    entry.inFlightVersion = version

    let request: Promise<Result | undefined>
    try {
      // Call the request synchronously when the debounce timer fires. This makes
      // the transport boundary observable without waiting for a microtask.
      request = mutateRequest(key, attempted)
    } catch (error) {
      settleFailure(key, entry, version, attempted, error)
      return
    }

    void Promise.resolve(request).then(
      (result) => settleSuccess(key, entry, version, attempted, result),
      (error: unknown) => settleFailure(key, entry, version, attempted, error),
    )
  }

  function schedule(key: string, entry: Entry<string, Value>): void {
    clearTimer(entry)
    entry.timer = setTimeout(() => {
      entry.timer = undefined
      dispatch(key, entry)
    }, debounce)
  }

  function queueLatest(key: string, entry: Entry<string, Value>): void {
    entry.queued = true
    if (!entry.inFlight) schedule(key, entry)
  }

  function settleSuccess(
    key: string,
    entry: Entry<string, Value>,
    version: number,
    attempted: Value,
    result: Result | undefined,
  ): void {
    if (!shouldContinue(key, entry)) return
    let confirmed: Value
    if (options.resolve) confirmed = options.resolve(result, attempted, key)
    else if (result === undefined) confirmed = attempted
    else confirmed = result as unknown as Value
    const superseded = entry.intentVersion !== version
    entry.inFlight = false
    entry.inFlightVersion = null
    entry.confirmed = confirmed
    entry.hasConfirmed = true
    entry.error = null
    entry.retryValue = undefined

    // A newer intent may already be visible. It is safe to drop it only when
    // the older response happens to establish the same confirmed value.
    const keepsWhatIsVisible =
      !superseded ||
      (entry.hasOptimistic && equals(entry.optimistic as Value, confirmed))
    if (keepsWhatIsVisible) {
      entry.optimistic = confirmed
      entry.hasOptimistic = true
      entry.queued = false
    } else {
      entry.queued = true
    }

    options.onSuccess?.({
      attempted,
      confirmed,
      key,
      result,
      superseded,
    })

    if (!entry.queued) return
    // The first request has already paid the debounce cost. A queued intent is
    // sent as soon as that request settles, while still coalescing any later
    // intents that arrive before it does.
    dispatch(key, entry)
  }

  function settleFailure(
    key: string,
    entry: Entry<string, Value>,
    version: number,
    attempted: Value,
    error: unknown,
  ): void {
    if (!shouldContinue(key, entry)) return
    entry.inFlight = false
    entry.inFlightVersion = null

    if (entry.intentVersion === version) {
      entry.optimistic = entry.hasConfirmed ? entry.confirmed : undefined
      entry.hasOptimistic = entry.hasConfirmed
      entry.queued = false
      entry.error = error
      entry.retryValue = attempted
      return
    }

    // The failed request was stale. Keep the latest intent visible and retry it
    // only when it still differs from what the server confirmed.
    entry.error = null
    entry.retryValue = undefined
    if (
      entry.hasOptimistic &&
      entry.hasConfirmed &&
      equals(entry.optimistic as Value, entry.confirmed as Value)
    ) {
      entry.optimistic = entry.confirmed
      entry.queued = false
      return
    }
    entry.queued = true
    dispatch(key, entry)
  }

  function mutate(key: string, value: Value): void {
    if (disposed) return
    const entry = ensure(key)
    if (!entry.hasConfirmed) {
      entry.confirmed = value
      entry.hasConfirmed = true
    }
    entry.intentVersion += 1
    entry.optimistic = value
    entry.hasOptimistic = true
    entry.error = null
    entry.retryValue = undefined
    queueLatest(key, entry)
  }

  function seed(key: string, value: Value): void {
    if (disposed) return
    const entry = ensure(key)
    if (entry.inFlight || entry.queued) return
    clearTimer(entry)
    entry.confirmed = value
    entry.hasConfirmed = true
    entry.optimistic = value
    entry.hasOptimistic = true
    entry.error = null
    entry.retryValue = undefined
    entry.intentVersion += 1
  }

  function retry(key: string): void {
    if (disposed) return
    const entry = entries.get(key)
    if (entry === undefined || entry.retryValue === undefined) return
    const value = entry.retryValue
    entry.intentVersion += 1
    entry.optimistic = value
    entry.hasOptimistic = true
    entry.error = null
    entry.retryValue = undefined
    queueLatest(key, entry)
  }

  function flush(key?: string): void {
    const keys = key === undefined ? [...entries.keys()] : [key]
    for (const currentKey of keys) {
      const entry = entries.get(currentKey)
      if (entry === undefined || !entry.queued || entry.inFlight) continue
      clearTimer(entry)
      dispatch(currentKey, entry)
    }
  }

  function cancel(key?: string): void {
    const keys = key === undefined ? [...entries.keys()] : [key]
    for (const currentKey of keys) {
      const entry = entries.get(currentKey)
      if (entry === undefined) continue
      clearTimer(entry)
      // Removing the entry also makes a late in-flight response inert. The
      // transport cannot be aborted through the generic callback, but no stale
      // answer can repopulate a screen that moved on.
      entries.delete(currentKey)
    }
  }

  function getValue(key: string): Value | undefined {
    const entry = entries.get(key)
    return entry?.hasOptimistic ? entry.optimistic : undefined
  }

  function getState(key: string): DebouncedMutationState<Value> {
    const entry = entries.get(key)
    return entry === undefined
      ? {
          confirmed: undefined,
          error: null,
          inFlight: false,
          queued: false,
          status: 'idle',
          value: undefined,
        }
      : snapshot(entry)
  }

  function stateFor(key: string): ComputedRef<DebouncedMutationState<Value>> {
    return computed(() => getState(key))
  }

  function valueFor(
    key: string,
    fallback?: Value,
  ): ComputedRef<Value | undefined> {
    return computed(() => getValue(key) ?? fallback)
  }

  function optimistic(
    key: string,
    fallback?: Value,
  ): ComputedRef<Value | undefined> {
    return valueFor(key, fallback)
  }

  function pendingFor(key: string): ComputedRef<boolean> {
    return computed(() => isPending(key))
  }

  function errorFor(key: string): ComputedRef<unknown | null> {
    return computed(() => getState(key).error)
  }

  function isPending(key: string): boolean {
    const entry = entries.get(key)
    return entry?.queued === true || entry?.inFlight === true
  }

  const pendingCount = computed(() => {
    let count = 0
    for (const entry of entries.values()) {
      if (entry.queued || entry.inFlight) count += 1
    }
    return count
  })
  const pending = computed(() => pendingCount.value > 0)
  const states = computed(() => {
    const result = new Map<string, DebouncedMutationState<Value>>()
    for (const [key, entry] of entries) result.set(key, snapshot(entry))
    return result as ReadonlyMap<string, DebouncedMutationState<Value>>
  })

  if (getCurrentInstance() !== null) {
    onUnmounted(() => {
      disposed = true
      cancel()
      entries.clear()
    })
  }

  return {
    cancel,
    errorFor,
    flush,
    getState,
    getValue,
    hasPending: pending,
    isPending,
    mutate,
    optimistic,
    pending,
    pendingCount,
    pendingFor,
    retry,
    seed,
    set: mutate,
    stateFor,
    states,
    valueFor,
  }
}
