import { afterEach, expect, it, vi } from 'vitest'

import {
  applyDeployment,
  deploymentNotStarted,
  planDeployment,
} from '@/shared/api/deploy'
import { RoutevaneAPIError } from '@/shared/api/http'

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it.each([false, true])(
  'sends the registered identity and displayed access without raw fields (confirm=%s)',
  async (confirm) => {
    const attemptID = 'e'.repeat(32)
    const artifactID = 'a'.repeat(32)
    const credential = crypto.randomUUID()
    const selected = {
      deviceID: 'd'.repeat(32),
      password: credential,
      expectedConnection: {
        device: 'http://192.0.2.1',
        username: 'admin',
        interfaceName: 'Wireguard0',
      },
    }
    const payload = confirm
      ? {
          attempt_id: attemptID,
          artifact_id: artifactID,
          status: 'succeeded',
          result: { applied: true, rolled_back: false, device: {} },
        }
      : {
          plan: {
            artifact_id: artifactID,
            artifact_hash: 'f'.repeat(64),
            target_id: 'keenetic',
            title: 'Keenetic',
            deployer_id: 'keenetic-route-bat',
            size_bytes: 10,
          },
        }
    const fetchMock = vi.fn(
      async (_input: unknown, _init?: RequestInit) =>
        new Response(JSON.stringify(payload), { status: 200 }),
    )
    vi.stubGlobal('fetch', fetchMock)

    if (confirm) await applyDeployment(artifactID, selected, attemptID)
    else await planDeployment(artifactID, selected)

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [path, request] = fetchMock.mock.calls[0] ?? []
    expect(path).toBe(`/v1/artifacts/${artifactID}/deploy`)
    expect(JSON.parse(String(request?.body))).toEqual({
      device_id: selected.deviceID,
      expected_connection: {
        device: selected.expectedConnection.device,
        username: selected.expectedConnection.username,
        interface: selected.expectedConnection.interfaceName,
      },
      password: selected.password,
      confirm,
      ...(confirm ? { attempt_id: attemptID } : {}),
    })
  },
)

it.each([
  { label: 'explicit refusal', marker: false, expected: true },
  { label: 'accepted attempt', marker: true, expected: false },
  { label: 'absent marker', marker: undefined, expected: false },
  { label: 'string false', marker: 'false', expected: false },
  { label: 'null marker', marker: null, expected: false },
  { label: 'numeric zero', marker: 0, expected: false },
])('keeps retry authority for $label', async ({ marker, expected }) => {
  const credential = crypto.randomUUID()
  const fetchMock = vi.fn(
    async () =>
      new Response(
        JSON.stringify({
          error: 'connection_invalid',
          attempt_started: marker,
        }),
        { status: 400 },
      ),
  )
  vi.stubGlobal('fetch', fetchMock)
  const failure: unknown = await applyDeployment(
    'a'.repeat(32),
    {
      device: 'http://192.0.2.1',
      username: 'admin',
      password: credential,
      interfaceName: 'Wireguard0',
    },
    'e'.repeat(32),
  ).catch((error: unknown) => error)

  expect(failure).toBeInstanceOf(RoutevaneAPIError)
  expect(deploymentNotStarted(failure)).toBe(expected)
})

it('does not treat a transport failure, array, or successful malformed body as a refusal guarantee', () => {
  expect(deploymentNotStarted(new TypeError('failed to fetch'))).toBe(false)
  expect(
    deploymentNotStarted(new RoutevaneAPIError('connection_invalid', 400)),
  ).toBe(false)
  expect(
    deploymentNotStarted(
      new RoutevaneAPIError('connection_invalid', 400, '', {}, [
        { attempt_started: false },
      ]),
    ),
  ).toBe(false)
  expect(
    deploymentNotStarted(
      new RoutevaneAPIError(
        'connection_invalid',
        200,
        '',
        {},
        { attempt_started: false },
      ),
    ),
  ).toBe(false)
})
