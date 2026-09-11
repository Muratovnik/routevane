import { describe, expect, it } from 'vitest'

import { validAddress } from '@/shared/lib/deviceAddress'

describe('destination address shape', () => {
  it('accepts what a deployer is actually addressed by', () => {
    for (const value of [
      'http://192.168.1.1',
      'https://my.keenetic.net',
      'http://[fd00::1]',
      'https://[fd00::1]:8443/',
      'http://192.168.1.1:81/',
      'http://192.168.1.1:8080',
      // A local configuration file is what the sing-box deployer declares as
      // its own example, so the form has to accept exactly that.
      'file:///C:/sing-box/config.json',
      'file:///C:/Program Files/sing-box/config.json',
      'file:///etc/sing-box/config.json',
    ]) {
      expect(validAddress(value), value).toBe(true)
    }
  })

  // Every shipped deployer is reached over a scheme it names, and the service
  // refuses an address that leaves one out. A form that took a bare host was
  // promising something the service would not honour.
  it('refuses an address that leaves its scheme to be guessed', () => {
    for (const value of [
      '192.168.1.1',
      'router',
      'router:8080',
      '192.168.1.1:8080',
      '[fe80::1]:8080',
    ]) {
      expect(validAddress(value), value).toBe(false)
    }
  })

  it.each(['username', 'password'] as const)(
    'refuses an embedded %s',
    (field) => {
      const address = new URL('http://[fd00::1]')
      address[field] = 'synthetic-fixture'
      expect(validAddress(address.href)).toBe(false)
    },
  )

  it('rejects a malformed address before a request is made', () => {
    for (const value of [
      '',
      'http://192.168.1.1/admin',
      'http://',
      'http://[fd00::1',
      'http://[fd00::1]:70000',
      'http://192.168.1.1?query=yes',
      'http://192.168.1.1#fragment',
      'ftp://192.168.1.1',
      'http://192.168.1.1 ',
      'два адреса сразу',
      'http://192.168.1.1:0',
      'http://192.168.1.1:70000',
      'file://',
      'file:///',
      'ssh://192.168.1.1',
    ]) {
      expect(validAddress(value), value).toBe(false)
    }
  })
})
