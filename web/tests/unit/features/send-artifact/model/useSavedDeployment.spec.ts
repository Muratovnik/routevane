import { afterEach, expect, it, onTestFinished, vi } from 'vitest'
import { effectScope } from 'vue'

import { useDeployment } from '@/features/send-artifact/model/useDeployment'

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), { status })

const devicesPayload = () => ({
  devices: [
    {
      id: 'saved',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      name: 'Home',
      address: 'http://10.255.0.1',
      account: 'admin',
      interface: 'Wireguard0',
      auto_deliver: true,
      credential_stored: true,
      deployable: true,
    },
    {
      id: 'other',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      name: 'Office',
      address: 'http://10.255.0.2',
      account: 'operator',
      interface: 'Wireguard1',
      auto_deliver: true,
      credential_stored: true,
      deployable: true,
    },
  ],
  secret_store_available: true,
})

const targetsPayload = () => ({
  targets: [
    {
      target_id: 'keenetic',
      title: 'Keenetic',
      deployer_id: 'keenetic-route-bat',
      requirements: {
        address_label: 'Address',
        address_example: 'http://10.255.0.1',
        needs_credential: true,
        needs_interface: true,
        interface_label: 'Interface',
      },
    },
  ],
})

const planPayload = () => ({
  plan: {
    artifact_id: 'artifact',
    artifact_hash: 'f'.repeat(64),
    target_id: 'keenetic',
    title: 'Keenetic',
    deployer_id: 'keenetic-route-bat',
    size_bytes: 10,
  },
})

const harness = () => {
  const scope = effectScope()
  onTestFinished(() => scope.stop())
  const deployment = scope.run(() =>
    useDeployment(
      () => 'artifact',
      () => 'keenetic',
      () => 'saved',
    ),
  )!
  return { deployment, scope }
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

it.each(['success', 'failure'] as const)(
  'discards a late preview %s after selecting another access',
  async (response) => {
    const pending = Promise.withResolvers<Response>()
    const fetchMock = vi.fn((input: unknown): Promise<Response> => {
      if (String(input) === '/v1/devices')
        return Promise.resolve(json(devicesPayload()))
      if (String(input) === '/v1/deployments/targets')
        return Promise.resolve(json(targetsPayload()))
      return pending.promise
    })
    vi.stubGlobal('fetch', fetchMock)
    const { deployment } = harness()
    await deployment.initialize()
    deployment.password.value = 'one-use-secret'
    const preview = deployment.review()
    expect(deployment.state.value).toBe('planning')
    deployment.connections.selectedID.value = 'other'
    expect(deployment.password.value).toBe('')
    expect(deployment.state.value).toBe('idle')
    if (response === 'success') pending.resolve(json(planPayload()))
    else pending.reject(new TypeError('late failure'))
    await preview
    expect(deployment.plan.value).toBeNull()
    expect(deployment.errorKey.value).toBeNull()
    expect(deployment.state.value).toBe('idle')
    await deployment.apply()
    expect(
      fetchMock.mock.calls.filter(([input]) =>
        String(input).endsWith('/deploy'),
      ),
    ).toHaveLength(1)
  },
)

it('invalidates every completed plan when its credential or access changes', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: unknown) => {
      if (String(input) === '/v1/devices') return json(devicesPayload())
      if (String(input) === '/v1/deployments/targets')
        return json(targetsPayload())
      return json(planPayload())
    }),
  )
  const { deployment } = harness()
  await deployment.initialize()
  await deployment.review()
  expect(deployment.state.value).toBe('planned')
  deployment.password.value = 'override'
  expect(deployment.plan.value).toBeNull()
  expect(deployment.state.value).toBe('idle')
  await deployment.review()
  deployment.connections.selectedID.value = 'other'
  expect(deployment.plan.value).toBeNull()
  expect(deployment.password.value).toBe('')
})

it.each([
  {
    name: 'proven pre-attempt refusal',
    marker: false,
    state: 'failed',
    retry: true,
    error: 'connection_invalid',
  },
  {
    name: 'absent guarantee',
    marker: undefined,
    state: 'outcome_unknown',
    retry: false,
    error: 'connection_invalid',
  },
  {
    name: 'string marker',
    marker: 'false',
    state: 'outcome_unknown',
    retry: false,
    error: 'connection_invalid',
  },
  {
    name: 'accepted attempt',
    marker: true,
    state: 'outcome_unknown',
    retry: false,
    error: 'connection_invalid',
  },
  {
    name: 'old mismatch without a guarantee',
    marker: undefined,
    state: 'outcome_unknown',
    retry: false,
    error: 'attempt_mismatch',
  },
])(
  'allows another plan only for $name',
  async ({ marker, state, retry, error }) => {
    const fetchMock = vi.fn(async (input: unknown, init?: RequestInit) => {
      const path = String(input)
      if (path === '/v1/devices') return json(devicesPayload())
      if (path === '/v1/deployments/targets') return json(targetsPayload())
      if (path.startsWith('/v1/deployment-attempts/'))
        return json({ error: 'not found' }, 404)
      const body = JSON.parse(String(init?.body)) as { confirm: boolean }
      return body.confirm
        ? json({ error, attempt_started: marker }, 400)
        : json(planPayload())
    })
    vi.stubGlobal('fetch', fetchMock)
    const { deployment } = harness()
    await deployment.initialize()
    deployment.password.value = 'one-use-secret'
    await deployment.review()
    await deployment.apply()
    expect(deployment.state.value).toBe(state)
    expect(deployment.password.value).toBe('')
    expect(deployment.canSubmit.value).toBe(retry)
    expect(
      window.sessionStorage.getItem('rv.deploy.attempt.artifact') === null,
    ).toBe(retry)
    expect(
      window.sessionStorage.getItem('rv.deploy.recipient.artifact') === null,
    ).toBe(retry)
    expect(JSON.stringify({ ...window.sessionStorage })).not.toContain(
      'one-use-secret',
    )
    await deployment.review()
    expect(deployment.state.value).toBe(retry ? 'planned' : 'outcome_unknown')
    const writes = fetchMock.mock.calls.filter(
      ([input, init]) =>
        String(input).endsWith('/deploy') &&
        (JSON.parse(String(init?.body)) as { confirm: boolean }).confirm,
    )
    expect(writes).toHaveLength(1)
  },
)

it('drops a one-use password and an outstanding preview when its scope closes', async () => {
  const pending = Promise.withResolvers<Response>()
  vi.stubGlobal(
    'fetch',
    vi.fn((input: unknown): Promise<Response> => {
      if (String(input) === '/v1/devices')
        return Promise.resolve(json(devicesPayload()))
      if (String(input) === '/v1/deployments/targets')
        return Promise.resolve(json(targetsPayload()))
      return pending.promise
    }),
  )
  const { deployment, scope } = harness()
  await deployment.initialize()
  deployment.password.value = 'one-use-secret'
  const preview = deployment.review()
  scope.stop()
  expect(deployment.password.value).toBe('')
  pending.resolve(json(planPayload()))
  await preview
  expect(deployment.plan.value).toBeNull()
})

it('clears the one-use password when the same saved identity acquires different access metadata', async () => {
  const registry = devicesPayload()
  const fetchMock = vi.fn(async (input: unknown) => {
    if (String(input) === '/v1/devices') return json(registry)
    if (String(input) === '/v1/deployments/targets')
      return json(targetsPayload())
    return json(planPayload())
  })
  vi.stubGlobal('fetch', fetchMock)
  const { deployment } = harness()
  await deployment.initialize()
  deployment.password.value = 'one-use-secret'
  await deployment.review()
  registry.devices[0] = {
    ...registry.devices[0]!,
    address: 'http://10.255.0.9',
    // A changed address removes the saved password with the consent (ADR 0050).
    auto_deliver: false,
    credential_stored: false,
  }
  await deployment.connections.initialize()
  expect(deployment.connections.selectedID.value).toBe('saved')
  expect(deployment.connections.selected.value?.address).toBe(
    'http://10.255.0.9',
  )
  expect(deployment.password.value).toBe('')
  expect(deployment.plan.value).toBeNull()
  expect(deployment.canSubmit.value).toBe(false)
})

const outcomePayload = (
  attemptID: string,
  status: 'succeeded' | 'failed' | 'outcome_unknown',
) => ({
  attempt_id: attemptID,
  artifact_id: 'artifact',
  status,
  result: {
    applied: status === 'succeeded',
    rolled_back: false,
    device: {},
    events: [],
  },
  ...(status === 'failed' ? { error: 'deploy_failed' } : {}),
})

it.each(['succeeded', 'failed', 'outcome_unknown'] as const)(
  'keeps the %s attempt recipient when the next selection, registry and page change',
  async (status) => {
    const registry = devicesPayload()
    let attemptID = ''
    const fetchMock = vi.fn(async (input: unknown, init?: RequestInit) => {
      const path = String(input)
      if (path === '/v1/devices') return json(registry)
      if (path === '/v1/deployments/targets') return json(targetsPayload())
      if (path.startsWith('/v1/deployment-attempts/'))
        return json(outcomePayload(attemptID, status))
      const body = JSON.parse(String(init?.body)) as {
        confirm: boolean
        attempt_id: string
      }
      if (!body.confirm) return json(planPayload())
      attemptID = body.attempt_id
      return json(outcomePayload(attemptID, status))
    })
    vi.stubGlobal('fetch', fetchMock)
    const { deployment, scope } = harness()
    await deployment.initialize()
    deployment.connections.selectedID.value = 'other'
    deployment.password.value = 'one-use-secret'
    await deployment.review()
    await deployment.apply()
    const recipient = {
      device: 'http://10.255.0.2',
      username: 'operator',
      interfaceName: 'Wireguard1',
    }
    expect(deployment.attemptRecipient.value).toEqual(recipient)
    expect(
      JSON.parse(
        window.sessionStorage.getItem('rv.deploy.recipient.artifact') ?? 'null',
      ),
    ).toEqual({ attemptID, ...recipient })
    expect(JSON.stringify({ ...window.sessionStorage })).not.toContain(
      'one-use-secret',
    )
    expect(deployment.password.value).toBe('')

    deployment.connections.selectedID.value = 'saved'
    registry.devices[1] = {
      ...registry.devices[1]!,
      address: 'http://10.255.0.9',
    }
    await deployment.connections.initialize()
    expect(deployment.attemptRecipient.value).toEqual(recipient)
    expect(deployment.canSubmit.value).toBe(status !== 'outcome_unknown')
    if (status === 'outcome_unknown') {
      await deployment.review()
      await deployment.apply()
      await deployment.checkOutcome()
    }
    expect(deployment.state.value).toBe(
      status === 'succeeded' ? 'applied' : status,
    )
    expect(deployment.attemptRecipient.value).toEqual(recipient)

    scope.stop()
    const reloaded = harness().deployment
    await reloaded.initialize()
    expect(reloaded.connections.selectedID.value).toBe('saved')
    expect(reloaded.attemptRecipient.value).toEqual(recipient)
    expect(reloaded.state.value).toBe(
      status === 'succeeded' ? 'applied' : status,
    )
    expect(reloaded.canSubmit.value).toBe(status !== 'outcome_unknown')
    expect(
      fetchMock.mock.calls.filter(
        ([input, init]) =>
          String(input).endsWith('/deploy') &&
          (JSON.parse(String(init?.body)) as { confirm: boolean }).confirm,
      ),
    ).toHaveLength(1)
  },
)

it.each([
  undefined,
  '{broken',
  JSON.stringify([]),
  JSON.stringify({
    attemptID: 'another-attempt',
    device: 'http://10.255.0.2',
    username: 'operator',
    interfaceName: 'Wireguard1',
  }),
  JSON.stringify({
    attemptID: 'e'.repeat(32),
    device: 42,
    username: 'operator',
    interfaceName: 'Wireguard1',
  }),
])(
  'does not infer a recipient from the form when remembered metadata is unusable: %s',
  async (stored) => {
    const attemptID = 'e'.repeat(32)
    window.sessionStorage.setItem('rv.deploy.attempt.artifact', attemptID)
    if (stored !== undefined)
      window.sessionStorage.setItem('rv.deploy.recipient.artifact', stored)
    const fetchMock = vi.fn(async (input: unknown) => {
      const path = String(input)
      if (path === '/v1/devices') return json(devicesPayload())
      if (path === '/v1/deployments/targets') return json(targetsPayload())
      return json(outcomePayload(attemptID, 'outcome_unknown'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const { deployment } = harness()
    await deployment.initialize()
    expect(deployment.connections.selectedID.value).toBe('saved')
    expect(deployment.attemptRecipient.value).toBeNull()
    expect(deployment.state.value).toBe('outcome_unknown')
    expect(deployment.canSubmit.value).toBe(false)
    deployment.connections.selectedID.value = 'other'
    await deployment.review()
    await deployment.apply()
    expect(deployment.attemptRecipient.value).toBeNull()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).endsWith('/deploy')),
    ).toBe(false)
  },
)

it('remembers only the canonical non-secret fields of a manual attempt', async () => {
  const fetchMock = vi.fn(async (input: unknown, init?: RequestInit) => {
    const path = String(input)
    if (path === '/v1/devices') return json(devicesPayload())
    if (path === '/v1/deployments/targets') return json(targetsPayload())
    const body = JSON.parse(String(init?.body)) as {
      confirm: boolean
      attempt_id: string
    }
    return body.confirm
      ? json(outcomePayload(body.attempt_id, 'succeeded'))
      : json(planPayload())
  })
  vi.stubGlobal('fetch', fetchMock)
  const { deployment } = harness()
  await deployment.initialize()
  deployment.connections.selectedID.value = 'manual'
  deployment.device.value = ' http://10.255.0.8 '
  deployment.username.value = ' manual-admin '
  deployment.interfaceName.value = ' Wireguard8 '
  deployment.password.value = 'manual-secret'
  await deployment.review()
  await deployment.apply()
  expect(deployment.attemptRecipient.value).toEqual({
    device: 'http://10.255.0.8',
    username: 'manual-admin',
    interfaceName: 'Wireguard8',
  })
  expect(JSON.stringify({ ...window.sessionStorage })).not.toContain(
    'manual-secret',
  )
  deployment.device.value = 'http://10.255.0.9'
  expect(deployment.attemptRecipient.value?.device).toBe('http://10.255.0.8')
})

const unsavedPayload = () => {
  const payload = devicesPayload()
  payload.devices[0] = {
    ...payload.devices[0]!,
    auto_deliver: false,
    credential_stored: false,
  }
  return payload
}

// The operator typed a password and asked to keep it in the connection; the
// preview is answered with the given status and the send with the given result.
const reviewKeepingPassword = async (
  planStatus: number,
  sent: 'succeeded' | 'failed' = 'succeeded',
) => {
  const fetchMock = vi.fn(
    async (input: unknown, init?: RequestInit): Promise<Response> => {
      const path = String(input)
      if (path === '/v1/devices') return json(unsavedPayload())
      if (path === '/v1/deployments/targets') return json(targetsPayload())
      if (path === '/v1/devices/saved/credential')
        return json({
          device: { id: 'saved', credential_stored: true },
          echo: JSON.parse(String(init?.body)),
        })
      if (planStatus !== 200)
        return json({ error: 'operation failed' }, planStatus)
      const body = JSON.parse(String(init?.body)) as {
        confirm: boolean
        attempt_id: string
      }
      return body.confirm
        ? json(outcomePayload(body.attempt_id, sent))
        : json(planPayload())
    },
  )
  vi.stubGlobal('fetch', fetchMock)
  const { deployment } = harness()
  await deployment.initialize()
  expect(deployment.connections.storedCredential.value).toBe(false)
  expect(deployment.passwordNeeded.value).toBe(true)
  deployment.password.value = 'router-secret'
  deployment.savePassword.value = true

  await deployment.review()
  const writes = () =>
    fetchMock.mock.calls.filter(
      ([input]) => String(input) === '/v1/devices/saved/credential',
    )
  return { deployment, writes }
}

// A review of a router does not contact it, so a password that only got a
// preview is unproven: nothing is stored until a send has worked with it.
it('saves the password the operator asked to keep only once the send succeeded', async () => {
  const { deployment, writes } = await reviewKeepingPassword(200)

  expect(deployment.state.value).toBe('planned')
  expect(writes()).toHaveLength(0)
  expect(deployment.connections.storedCredential.value).toBe(false)

  await deployment.apply()

  expect(writes()).toHaveLength(1)
  expect(JSON.parse(String(writes()[0]![1]?.body))).toEqual({
    credential: 'router-secret',
  })
  expect(deployment.state.value).toBe('applied')
  expect(deployment.connections.storedCredential.value).toBe(true)
  expect(deployment.savePassword.value).toBe(false)
  expect(deployment.passwordNeeded.value).toBe(false)
})

it('keeps no password when the send failed', async () => {
  const { deployment, writes } = await reviewKeepingPassword(200, 'failed')

  await deployment.apply()

  expect(deployment.state.value).toBe('failed')
  expect(writes()).toHaveLength(0)
  expect(deployment.connections.storedCredential.value).toBe(false)
})

it('keeps no password when the preview was refused', async () => {
  const { deployment, writes } = await reviewKeepingPassword(422)

  expect(writes()).toHaveLength(0)
  expect(deployment.connections.storedCredential.value).toBe(false)
})

it('never saves a password the operator did not ask to keep, and states a refused save', async () => {
  let refuse = false
  const fetchMock = vi.fn(
    async (input: unknown, init?: RequestInit): Promise<Response> => {
      const path = String(input)
      if (path === '/v1/devices') return json(unsavedPayload())
      if (path === '/v1/deployments/targets') return json(targetsPayload())
      if (path === '/v1/devices/saved/credential')
        return refuse
          ? json({ error: 'credential store unavailable' }, 503)
          : json({ device: { id: 'saved', credential_stored: true } })
      const body = JSON.parse(String(init?.body)) as {
        confirm: boolean
        attempt_id: string
      }
      return body.confirm
        ? json(outcomePayload(body.attempt_id, 'succeeded'))
        : json(planPayload())
    },
  )
  vi.stubGlobal('fetch', fetchMock)
  const { deployment } = harness()
  await deployment.initialize()
  deployment.password.value = 'router-secret'
  await deployment.review()
  await deployment.apply()
  expect(
    fetchMock.mock.calls.some(([input]) =>
      String(input).endsWith('/credential'),
    ),
  ).toBe(false)

  refuse = true
  deployment.savePassword.value = true
  deployment.password.value = 'router-secret'
  await deployment.review()
  await deployment.apply()
  expect(deployment.state.value).toBe('applied')
  expect(deployment.passwordSaveFailed.value).toBe(true)
  expect(deployment.connections.storedCredential.value).toBe(false)
})

it('names the first missing field instead of sending, and keeps the review available', async () => {
  const fetchMock = vi.fn(async (input: unknown): Promise<Response> => {
    const path = String(input)
    if (path === '/v1/devices') return json(unsavedPayload())
    if (path === '/v1/deployments/targets') return json(targetsPayload())
    return json(planPayload())
  })
  vi.stubGlobal('fetch', fetchMock)
  const { deployment } = harness()
  await deployment.initialize()
  expect(deployment.available.value).toBe(true)
  expect(deployment.fieldErrors.value.password).toBeNull()

  await expect(deployment.review()).resolves.toBe('password')

  expect(deployment.fieldErrors.value.password).toBe('send.field.required')
  expect(deployment.available.value).toBe(true)
  expect(
    fetchMock.mock.calls.some(([input]) => String(input).endsWith('/deploy')),
  ).toBe(false)
})

// The service owns the destination policy and names the field it refused
// (ADR 0050). The send says it at the address typed for this send, and a send
// that was refused for its address has not started anything.
const refusingAddress = () =>
  vi.fn(async (input: unknown): Promise<Response> => {
    const path = String(input)
    if (path === '/v1/devices') return json(devicesPayload())
    if (path === '/v1/deployments/targets') return json(targetsPayload())
    return json({ error: 'address_rejected', field: 'address' }, 422)
  })

it('states the refusal of a typed address at the address and starts nothing', async () => {
  vi.stubGlobal('fetch', refusingAddress())
  const { deployment } = harness()
  await deployment.initialize()
  deployment.connections.selectedID.value = 'manual'
  deployment.device.value = 'http://8.8.8.8'
  deployment.username.value = 'admin'
  deployment.interfaceName.value = 'Wireguard0'
  deployment.password.value = 'one-use-secret'

  await expect(deployment.review()).resolves.toBe('address')

  expect(deployment.fieldErrors.value.address).toBe('send.address.rejected')
  expect(deployment.state.value).toBe('idle')
  expect(deployment.errorKey.value).toBeNull()
  expect(deployment.plan.value).toBeNull()
  // The refusal was about the address as typed; another one is a new question.
  deployment.device.value = 'http://10.255.0.2'
  expect(deployment.fieldErrors.value.address).toBeNull()
})

it('does not blame a field the operator did not type when a saved connection is refused', async () => {
  vi.stubGlobal('fetch', refusingAddress())
  const { deployment } = harness()
  await deployment.initialize()

  await expect(deployment.review()).resolves.toBeNull()

  expect(deployment.fieldErrors.value.address).toBeNull()
  expect(deployment.state.value).toBe('failed')
  expect(deployment.errorKey.value).toBe('error.address_rejected')
})

it('states the refusal at the address when a confirmed send stopped before any attempt', async () => {
  let planned = false
  const fetchMock = vi.fn(
    async (input: unknown, init?: RequestInit): Promise<Response> => {
      const path = String(input)
      if (path === '/v1/devices') return json(devicesPayload())
      if (path === '/v1/deployments/targets') return json(targetsPayload())
      const body = JSON.parse(String(init?.body)) as { confirm: boolean }
      if (!body.confirm) {
        planned = true
        return json(planPayload())
      }
      return json(
        { error: 'address_rejected', field: 'address', attempt_started: false },
        422,
      )
    },
  )
  vi.stubGlobal('fetch', fetchMock)
  const { deployment } = harness()
  await deployment.initialize()
  deployment.connections.selectedID.value = 'manual'
  deployment.device.value = 'http://10.255.0.2'
  deployment.username.value = 'admin'
  deployment.interfaceName.value = 'Wireguard0'
  deployment.password.value = 'one-use-secret'
  await deployment.review()
  expect(planned).toBe(true)

  await deployment.apply()

  expect(deployment.fieldErrors.value.address).toBe('send.address.rejected')
  expect(deployment.state.value).toBe('idle')
  expect(deployment.errorKey.value).toBeNull()
})

it.each([
  ['secret_store_unavailable', 503, 'devices.credential.unavailable'],
  ['credential_invalid', 422, 'devices.credential.invalid'],
  ['something_else', 422, 'send.password.save.failed'],
])(
  'reads the refusal %s of a password kept after a send',
  async (code, status, shown) => {
    const fetchMock = vi.fn(
      async (input: unknown, init?: RequestInit): Promise<Response> => {
        const path = String(input)
        if (path === '/v1/devices') return json(unsavedPayload())
        if (path === '/v1/deployments/targets') return json(targetsPayload())
        if (path === '/v1/devices/saved/credential')
          return json({ error: code }, status)
        const body = JSON.parse(String(init?.body)) as {
          confirm: boolean
          attempt_id: string
        }
        return body.confirm
          ? json(outcomePayload(body.attempt_id, 'succeeded'))
          : json(planPayload())
      },
    )
    vi.stubGlobal('fetch', fetchMock)
    const { deployment } = harness()
    await deployment.initialize()
    deployment.savePassword.value = true
    deployment.password.value = 'router-secret'
    await deployment.review()
    await deployment.apply()

    expect(deployment.state.value).toBe('applied')
    expect(deployment.passwordSaveFailure.value).toBe(shown)
  },
)
