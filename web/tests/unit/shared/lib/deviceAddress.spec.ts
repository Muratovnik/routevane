import { describe, expect, it } from 'vitest'

import { validAddress } from '@/shared/lib/deviceAddress'

describe('destination address shape', () => {
  it('accepts what a deployer is actually addressed by', () => {
    for (const value of [
      'http://10.255.0.1',
      'https://my.keenetic.net',
      'http://[fd00::1]',
      'https://[fd00::1]:8443/',
      'http://10.255.0.1:81/',
      'http://10.255.0.1:8080',
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
      '10.255.0.1',
      'router',
      'router:8080',
      '10.255.0.1:8080',
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

  // Which networks a router may live on is the service's policy, stated at the
  // field from its `address_rejected` answer. The shape check says nothing of it.
  it('leaves the choice of network to the service', () => {
    for (const value of [
      'http://127.0.0.1',
      'http://8.8.8.8',
      'https://my.keenetic.net',
      'http://router.local',
      'http://169.254.169.254',
    ]) {
      expect(validAddress(value), value).toBe(true)
    }
  })

  it('rejects a malformed address before a request is made', () => {
    for (const value of [
      '',
      'http://10.255.0.1/admin',
      'http://',
      'http://[fd00::1',
      'http://[fd00::1]:70000',
      'http://10.255.0.1?query=yes',
      'http://10.255.0.1#fragment',
      'ftp://10.255.0.1',
      'http://10.255.0.1 ',
      'два адреса сразу',
      'http://10.255.0.1:0',
      'http://10.255.0.1:70000',
      'file://',
      'file:///',
      'ssh://10.255.0.1',
    ]) {
      expect(validAddress(value), value).toBe(false)
    }
  })
})
