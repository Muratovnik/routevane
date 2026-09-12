/** What a deployment asks the operator for, and what it reports afterwards. */
import { beforeEach, describe, expect, it } from 'vitest'

import {
  applyDeployment,
  loadDeploymentAttempt,
  loadDeployableTargets,
  planDeployment,
} from '@/shared/api/deploy'
import { RoutevaneAPIError } from '@/shared/api/http'

import {
  answer,
  artifactID,
  connection,
  deploymentAttemptID,
  fetchMock,
  outcomePayload,
  requirementsPayload,
  stubFetch,
} from './support/fixtures'

beforeEach(stubFetch)

describe('a deployment form asks only for what it can label', () => {
  it('requires an interface label exactly when an interface is required', async () => {
    answer(requirementsPayload({ needs_interface: false }))
    await expect(loadDeployableTargets()).resolves.toMatchObject([
      { requirements: { needsInterface: false, interfaceLabel: '' } },
    ])

    answer(
      requirementsPayload({
        needs_interface: true,
        interface_label: 'Interface',
      }),
    )
    await expect(loadDeployableTargets()).resolves.toMatchObject([
      { requirements: { needsInterface: true, interfaceLabel: 'Interface' } },
    ])

    answer(requirementsPayload({ needs_interface: true }))
    await expect(loadDeployableTargets()).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )

    answer(requirementsPayload({ needs_interface: true, interface_label: '' }))
    await expect(loadDeployableTargets()).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })

  it('reads a plan under its own envelope', async () => {
    answer({
      plan: {
        artifact_id: artifactID,
        artifact_hash: 'd'.repeat(64),
        target_id: 'keenetic',
        title: 'Keenetic',
        deployer_id: 'keenetic-telnet',
        size_bytes: 0,
      },
    })
    await expect(planDeployment(artifactID, connection)).resolves.toEqual({
      artifactID,
      artifactHash: 'd'.repeat(64),
      targetID: 'keenetic',
      title: 'Keenetic',
      deployerID: 'keenetic-telnet',
      sizeBytes: 0,
    })
  })
})

describe('a refused deployment still describes itself', () => {
  it('reads the audit trail out of a non-2xx reply', async () => {
    answer(
      {
        attempt_id: deploymentAttemptID,
        artifact_id: artifactID,
        status: 'failed',
        result: {
          applied: false,
          rolled_back: true,
          device: { deployer_id: 'keenetic-telnet', interface: 'wan' },
          backup: { hash: 'e'.repeat(64) },
          events: [
            { step: 'connect', outcome: 'ok' },
            { step: 'apply', outcome: 'failed', detail: 'refused' },
          ],
        },
        error: 'device refused the change',
      },
      502,
    )
    await expect(
      applyDeployment(artifactID, connection, deploymentAttemptID),
    ).resolves.toEqual({
      status: 'failed',
      attemptID: deploymentAttemptID,
      artifactID,
      applied: false,
      rolledBack: true,
      deployerID: 'keenetic-telnet',
      vendor: '',
      firmwareVersion: '',
      interfaceName: 'wan',
      backupHash: 'e'.repeat(64),
      events: [
        { step: 'connect', outcome: 'ok', detail: '' },
        { step: 'apply', outcome: 'failed', detail: 'refused' },
      ],
      error: 'device refused the change',
    })
    const request = fetchMock.mock.calls[0]
    expect(JSON.parse(String(request?.[1]?.body))).toMatchObject({
      attempt_id: deploymentAttemptID,
      confirm: true,
    })
  })

  it('reads an unresolved durable attempt without inventing a failure', async () => {
    answer({
      attempt_id: deploymentAttemptID,
      artifact_id: artifactID,
      status: 'outcome_unknown',
      result: { applied: false, rolled_back: false, device: {} },
    })
    await expect(
      loadDeploymentAttempt(deploymentAttemptID),
    ).resolves.toMatchObject({
      attemptID: deploymentAttemptID,
      artifactID,
      status: 'outcome_unknown',
      applied: false,
    })
  })

  it('reports the server words when the reply is not an outcome', async () => {
    answer({ error: 'artifact unavailable' }, 503)
    await expect(
      applyDeployment(artifactID, connection, deploymentAttemptID),
    ).rejects.toThrow('artifact unavailable')

    answer({ result: {} }, 200)
    await expect(
      applyDeployment(artifactID, connection, deploymentAttemptID),
    ).rejects.toThrow('Применение не выполнено.')
  })

  // A deployment refused before it started took no step, and that is not a
  // malformed reply. A step that does not say what it was is.
  it('reads no steps as none and a nameless step as malformed', async () => {
    answer(outcomePayload({}))
    await expect(
      applyDeployment(artifactID, connection, deploymentAttemptID),
    ).resolves.toMatchObject({ events: [], backupHash: '' })

    answer(outcomePayload({ events: 'none' }))
    await expect(
      applyDeployment(artifactID, connection, deploymentAttemptID),
    ).resolves.toMatchObject({ events: [] })

    answer(outcomePayload({ events: [{ step: 'connect' }] }))
    await expect(
      applyDeployment(artifactID, connection, deploymentAttemptID),
    ).rejects.toThrow('Применение не выполнено.')

    answer(outcomePayload({ backup: 'gone' }))
    await expect(
      applyDeployment(artifactID, connection, deploymentAttemptID),
    ).resolves.toMatchObject({ backupHash: '' })
  })
})
