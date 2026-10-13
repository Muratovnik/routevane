import { describe, expect, it } from 'vitest'

import { outputActionError } from '@/features/view-profile/model/useProfileView'
import { RoutevaneAPIError } from '@/shared/api/http'

describe('outputActionError', () => {
  it.each([
    [409, 'profile_archived', 'outputs.error.archived'],
    [409, 'subscription_not_issued', 'outputs.reissue.error.notIssued'],
    [404, '', 'outputs.error.missing'],
    [500, '', 'error.unexpected'],
  ])('names a %i %s refusal of a format action as %s', (status, code, key) => {
    expect(
      outputActionError(new RoutevaneAPIError('refused', status, code)),
    ).toBe(key)
  })

  it('states a request that never reached the service as a network failure', () => {
    expect(outputActionError(new TypeError('Failed to fetch'))).toBe(
      'error.network',
    )
  })
})
