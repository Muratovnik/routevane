/** What a published build and each attempt at one are allowed to state. */
import { beforeEach, describe, expect, it } from 'vitest'

import { RoutevaneAPIError } from '@/shared/api/http'
import { addOutput, buildOutput, loadOutput } from '@/shared/api/outputs'
import { loadProfiles } from '@/shared/api/profiles'

import {
  answer,
  artifactID,
  attemptPayload,
  buildPayload,
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
