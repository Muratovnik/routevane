import { describe, expect, it } from 'vitest'

import { messageKey } from '@/features/send-artifact/model/useDeployment'
import { RoutevaneAPIError } from '@/shared/api/http'

describe('failure messages', () => {
  // The whole point: a server code or an English server sentence never reaches
  // the operator as itself.
  it('maps a device failure code to its own message', () => {
    expect(messageKey(new RoutevaneAPIError('connection_invalid', 400))).toBe(
      'error.connection_invalid',
    )
  })

  it('maps the plan refusal that used to surface as "operation failed"', () => {
    expect(messageKey(new RoutevaneAPIError('operation failed', 422))).toBe(
      'error.operationFailed',
    )
  })

  it('explains an unowned FQDN collision without calling it a changed target', () => {
    expect(
      messageKey(new RoutevaneAPIError('fqdn_ownership_conflict', 409)),
    ).toBe('error.fqdn_ownership_conflict')
  })

  it('maps transport and status answers', () => {
    expect(messageKey(new RoutevaneAPIError('not found', 404))).toBe(
      'error.notFound',
    )
    expect(
      messageKey(new RoutevaneAPIError('output target changed', 409)),
    ).toBe('error.targetChanged')
    expect(messageKey(new RoutevaneAPIError('artifact unavailable', 503))).toBe(
      'error.unavailable',
    )
    expect(messageKey(new RoutevaneAPIError('teapot', 418))).toBe(
      'error.unexpected',
    )
    expect(messageKey(new TypeError('failed to fetch'))).toBe('error.network')
  })
})
