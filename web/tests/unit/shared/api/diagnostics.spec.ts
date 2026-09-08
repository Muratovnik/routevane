/** Which level of a routing plan owns the reason a rule was kept or dropped. */
import { beforeEach, describe, expect, it } from 'vitest'

import { loadDiagnostics } from '@/shared/api/artifacts'
import { RoutevaneAPIError } from '@/shared/api/http'

import { answer, snapshotID, stubFetch } from './support/fixtures'

beforeEach(stubFetch)

describe('diagnostics name the reason at the level that owns it', () => {
  it('reads a kept rule and an excluded candidate out of one plan', async () => {
    answer({
      routing_plan: {
        rules: [
          {
            service_id: 'discord',
            value: 'discord.com',
            reason_codes: ['catalog_domain'],
          },
        ],
        excluded: [
          {
            candidate: { service_id: 'discord', value: '0.0.0.0/0' },
            reason_codes: ['too_broad'],
          },
        ],
      },
    })
    await expect(loadDiagnostics(snapshotID)).resolves.toEqual([
      {
        listID: 'discord',
        value: 'discord.com',
        reasons: ['catalog_domain'],
        excluded: false,
      },
      {
        listID: 'discord',
        value: '0.0.0.0/0',
        reasons: ['too_broad'],
        excluded: true,
      },
    ])
  })

  it('refuses an exclusion with no candidate and a plan with no lists', async () => {
    answer({
      routing_plan: {
        rules: [],
        excluded: [{ service_id: 'discord', reason_codes: ['too_broad'] }],
      },
    })
    await expect(loadDiagnostics(snapshotID)).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )

    answer({ routing_plan: { rules: [] } })
    await expect(loadDiagnostics(snapshotID)).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )
  })
})
