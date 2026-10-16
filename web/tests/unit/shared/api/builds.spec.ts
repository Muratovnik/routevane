/** What a published build and each attempt at one are allowed to state. */
import { beforeEach, describe, expect, it } from 'vitest'

import { RoutevaneAPIError } from '@/shared/api/http'
import {
  addOutput,
  buildOutput,
  loadOutput,
  setOutputKeepAddresses,
} from '@/shared/api/outputs'
import { loadProfiles } from '@/shared/api/profiles'

import {
  answer,
  artifactID,
  attemptPayload,
  buildPayload,
  fetchMock,
  outputID,
  profileID,
  profilePayload,
  snapshotID,
  stubFetch,
  subscription,
} from './support/fixtures'

beforeEach(stubFetch)

describe('a build reply describes one published fact', () => {
  it('accepts a summary that matches its artifact field for field', async () => {
    answer(buildPayload())
    await expect(buildOutput(outputID)).resolves.toMatchObject({
      snapshotID,
      artifactID,
      validationStatus: 'valid',
      status: 'published',
      contentCreatedAt: '2026-08-20T12:00:00Z',
      degradedSources: [],
      subscriptionURL: subscription,
    })
  })

  it('refuses a summary that disagrees with its artifact', async () => {
    for (const [field, value] of [
      ['validation_status', 'invalid'],
      ['status', 'draft'],
      ['content_created_at', '2026-08-20T13:00:00Z'],
    ] as const) {
      const payload = buildPayload()
      Object.assign(payload.summary as Record<string, unknown>, {
        [field]: value,
      })
      answer(payload)
      await expect(buildOutput(outputID)).rejects.toBeInstanceOf(
        RoutevaneAPIError,
      )
    }
  })

  // The routing plan is diagnostic material an operator asks for by name. A
  // build reply that smuggles one in is refused rather than read.
  it('refuses a snapshot that carries a routing plan', async () => {
    const payload = buildPayload()
    Object.assign(payload.snapshot as Record<string, unknown>, {
      routing_plan: { canary: 'must-never-render' },
    })
    answer(payload)
    await expect(buildOutput(outputID)).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })

  // A subscription address is a bearer: this origin's own plain-HTTP
  // subscription path, or nothing at all.
  it('admits only a same-origin subscription address', async () => {
    const accepted = ['', subscription]
    for (const value of accepted) {
      const payload = buildPayload()
      payload.subscription_url = value
      answer(payload)
      await expect(buildOutput(outputID)).resolves.toMatchObject({
        subscriptionURL: value,
      })
    }

    const refused = [
      'http://other.test/v1/subscriptions/rv1.invalid',
      `https://${window.location.host}/v1/subscriptions/rv1.invalid`,
      `${window.location.origin}/v1/profiles/${profileID}/export`,
      'javascript:alert(1)',
      'not a url',
    ]
    for (const value of refused) {
      const payload = buildPayload()
      payload.subscription_url = value
      answer(payload)
      await expect(buildOutput(outputID)).rejects.toBeInstanceOf(
        RoutevaneAPIError,
      )
    }
  })

  // The bearer used to travel back with the output that had just been created.
  it('refuses a created output that still carries a subscription', async () => {
    answer({
      output: { id: outputID, list_id: profileID, target_id: 'keenetic' },
    })
    await expect(addOutput(profileID, 'keenetic')).resolves.toEqual({
      output: {
        id: outputID,
        profileID,
        targetID: 'keenetic',
        deviceID: '',
        fqdnGroupPrefix: '',
        keepAddresses: false,
      },
    })

    answer({
      output: { id: outputID, list_id: profileID, target_id: 'keenetic' },
      subscription_url: subscription,
    })
    await expect(addOutput(profileID, 'keenetic')).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })

  it('reads a stored output and its latest file', async () => {
    answer({
      id: outputID,
      list_id: profileID,
      target_id: 'keenetic',
      latest_artifact_id: artifactID,
    })
    await expect(loadOutput(outputID)).resolves.toEqual({
      id: outputID,
      profileID,
      targetID: 'keenetic',
      deviceID: '',
      fqdnGroupPrefix: '',
      keepAddresses: false,
      latestArtifactID: artifactID,
    })
  })
})

describe('a build attempt names either a file or a reason', () => {
  it('accepts the two shapes an attempt can honestly have', async () => {
    answer(
      attemptPayload({
        status: 'success',
        artifact_id: artifactID,
        completed_at: '2026-08-20T12:01:00Z',
      }),
    )
    await expect(loadProfiles()).resolves.toMatchObject([
      {
        outputs: [
          {
            lastAttempt: {
              status: 'success',
              code: '',
              artifactID,
              projectedRules: 0,
              maximumRules: 0,
            },
          },
        ],
      },
    ])

    answer(
      attemptPayload({
        status: 'failed',
        code: 'rule_limit',
        projected_rules: 1740,
        maximum_rules: 1024,
        completed_at: '2026-08-20T12:01:00Z',
      }),
    )
    await expect(loadProfiles()).resolves.toMatchObject([
      {
        outputs: [
          {
            lastAttempt: {
              status: 'failed',
              code: 'rule_limit',
              artifactID: '',
              projectedRules: 1740,
              maximumRules: 1024,
            },
          },
        ],
      },
    ])
  })

  // A coverage failure names the lists it could not cover; every other attempt,
  // and one recorded before the service named them, names none.
  it('reads the lists a coverage failure names', async () => {
    answer(
      attemptPayload({
        status: 'failed',
        code: 'partial_coverage',
        uncovered_lists: [
          { id: 'delta', title: 'Дельта' },
          { id: 'gamma', title: 'Гамма' },
        ],
        completed_at: '2026-08-20T12:01:00Z',
      }),
    )
    await expect(loadProfiles()).resolves.toMatchObject([
      {
        outputs: [
          {
            lastAttempt: {
              code: 'partial_coverage',
              uncoveredLists: [
                { id: 'delta', title: 'Дельта' },
                { id: 'gamma', title: 'Гамма' },
              ],
            },
          },
        ],
      },
    ])

    answer(
      attemptPayload({
        status: 'failed',
        code: 'partial_coverage',
        completed_at: '2026-08-20T12:01:00Z',
      }),
    )
    await expect(loadProfiles()).resolves.toMatchObject([
      { outputs: [{ lastAttempt: { uncoveredLists: [] } }] },
    ])

    // A list that names no title is not a list a screen can show.
    answer(
      attemptPayload({
        status: 'failed',
        code: 'partial_coverage',
        uncovered_lists: [{ id: 'delta' }],
        completed_at: '2026-08-20T12:01:00Z',
      }),
    )
    await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)
  })

  it('refuses an attempt that claims both or neither', async () => {
    const refused = [
      // A success that also names a failure code.
      {
        status: 'success',
        code: 'rule_limit',
        artifact_id: artifactID,
        completed_at: '2026-08-20T12:01:00Z',
      },
      // A success that published nothing.
      { status: 'success', completed_at: '2026-08-20T12:01:00Z' },
      // A failure with no reason.
      { status: 'failed', completed_at: '2026-08-20T12:01:00Z' },
      // A failure that nonetheless points at a file.
      {
        status: 'failed',
        code: 'rule_limit',
        artifact_id: artifactID,
        completed_at: '2026-08-20T12:01:00Z',
      },
      // An outcome outside the closed set, and one with no time.
      { status: 'partial', code: 'x', completed_at: '2026-08-20T12:01:00Z' },
      { status: 'failed', code: 'rule_limit' },
    ]
    for (const attempt of refused) {
      answer(attemptPayload(attempt))
      await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)
    }
  })

  // An absent attempt is a card that honestly has none; a malformed one
  // refuses the row.
  it('separates an absent attempt from a malformed one', async () => {
    answer(attemptPayload({}))
    await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)

    answer(
      profilePayload({
        outputs: [
          {
            id: outputID,
            target_id: 'keenetic',
            target_title: 'Keenetic',
            created_at: '2026-08-20T12:00:00Z',
            last_attempt: null,
            latest: null,
          },
        ],
      }),
    )
    await expect(loadProfiles()).resolves.toMatchObject([
      { outputs: [{ lastAttempt: null, latest: null, targetKind: '' }] },
    ])
  })
})

describe('the last delivery of an output', () => {
  const card = (delivery?: unknown) =>
    profilePayload({
      outputs: [
        {
          id: outputID,
          target_id: 'keenetic',
          target_title: 'Keenetic',
          created_at: '2026-08-20T12:00:00Z',
          ...(delivery === undefined ? {} : { last_delivery: delivery }),
        },
      ],
    })

  it('reads the connection and the file that last arrived', async () => {
    answer(
      card({
        device_id: 'd'.repeat(32),
        artifact_id: artifactID,
        completed_at: '2026-08-21T08:30:00Z',
      }),
    )
    await expect(loadProfiles()).resolves.toMatchObject([
      {
        outputs: [
          {
            lastDelivery: {
              deviceID: 'd'.repeat(32),
              artifactID,
              completedAt: '2026-08-21T08:30:00Z',
            },
          },
        ],
      },
    ])
  })

  it('reads an output that was never delivered as having none', async () => {
    for (const absent of [undefined, null]) {
      answer(card(absent))
      await expect(loadProfiles()).resolves.toMatchObject([
        { outputs: [{ lastDelivery: null }] },
      ])
    }
  })

  it('refuses a delivery that names no connection, file or time', async () => {
    const whole = {
      device_id: 'd'.repeat(32),
      artifact_id: artifactID,
      completed_at: '2026-08-21T08:30:00Z',
    }
    for (const field of Object.keys(whole)) {
      answer(card({ ...whole, [field]: undefined }))
      await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)
    }
    answer(card({ ...whole, completed_at: 'yesterday' }))
    await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)
  })
})

describe('an output states whether it keeps addresses beside its domains', () => {
  it('reads the setting and whether the format can hold it from a card', async () => {
    answer(
      profilePayload({
        outputs: [
          {
            id: outputID,
            target_id: 'mikrotik',
            target_title: 'MikroTik',
            created_at: '2026-08-20T12:00:00Z',
            keep_addresses: true,
            keep_addresses_available: true,
          },
          {
            id: 'e'.repeat(32),
            target_id: 'openwrt',
            target_title: 'OpenWrt',
            created_at: '2026-08-20T12:00:00Z',
            keep_addresses: false,
            keep_addresses_available: false,
          },
        ],
      }),
    )
    await expect(loadProfiles()).resolves.toMatchObject([
      {
        outputs: [
          { keepAddresses: true, keepAddressesAvailable: true },
          { keepAddresses: false, keepAddressesAvailable: false },
        ],
      },
    ])
  })

  it('reads a reply that states neither as off and unavailable', async () => {
    answer(
      profilePayload({
        outputs: [
          {
            id: outputID,
            target_id: 'keenetic',
            target_title: 'Keenetic',
            created_at: '2026-08-20T12:00:00Z',
          },
        ],
      }),
    )
    await expect(loadProfiles()).resolves.toMatchObject([
      { outputs: [{ keepAddresses: false, keepAddressesAvailable: false }] },
    ])
  })

  it('sends the choice as a guarded PUT and reads the stored value back', async () => {
    answer({
      output: {
        id: outputID,
        list_id: profileID,
        target_id: 'singbox',
        keep_addresses: true,
      },
    })
    await expect(setOutputKeepAddresses(outputID, true)).resolves.toEqual({
      output: {
        id: outputID,
        profileID,
        targetID: 'singbox',
        deviceID: '',
        fqdnGroupPrefix: '',
        keepAddresses: true,
      },
    })
    expect(fetchMock).toHaveBeenLastCalledWith(
      `/v1/outputs/${outputID}/keep-addresses`,
      {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          'X-Routevane-Request': '1',
        },
        body: JSON.stringify({ keep_addresses: true }),
      },
    )
  })

  it('refuses the setting as an error carrying the server code', async () => {
    answer({ error: 'invalid_keep_addresses' }, 400)
    await expect(setOutputKeepAddresses(outputID, true)).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })
})
