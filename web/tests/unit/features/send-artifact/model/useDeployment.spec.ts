import { describe, expect, it } from 'vitest'

import {
  messageKey,
  validAddress,
} from '@/features/send-artifact/model/useDeployment'
import { RoutevaneAPIError } from '@/shared/api/http'

describe('address validation', () => {
  it('accepts what a device is actually reachable at', () => {
    for (const value of [
      '192.168.1.1',
      'http://192.168.1.1',
      'https://my.keenetic.net',
      'router',
      '192.168.1.1:8080',
      'http://192.168.1.1:81/',
      // A local configuration file is what the sing-box deployer declares as
      // its own example, so the form has to accept exactly that.
      'file:///C:/sing-box/config.json',
      'file:///C:/Program Files/sing-box/config.json',
      'file:///etc/sing-box/config.json',
    ]) {
      expect(validAddress(value), value).toBe(true)
    }
  })

  it('rejects a malformed address before a request is made', () => {
    for (const value of [
      '',
      '192.168.1.1/admin',
      'http://',
      'ftp://192.168.1.1',
      '192.168.1.1 ',
      'два адреса сразу',
      '192.168.1.1:0',
      '192.168.1.1:70000',
      'file://',
      'file:///',
      'ssh://192.168.1.1',
    ]) {
      expect(validAddress(value), value).toBe(false)
    }
  })
})

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
