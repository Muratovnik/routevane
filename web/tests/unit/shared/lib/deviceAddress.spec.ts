import { describe, expect, it } from 'vitest'

import { localDeviceAddress, validAddress } from '@/shared/lib/deviceAddress'

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

// Mirrors the destination policy the service applies to a router address
// (`netpolicy.ValidateDeviceURL`): an http or https address whose host is a
// private or link-local IP literal. A name is refused on purpose.
describe('local device address', () => {
  it('accepts private and link-local unicast literals', () => {
    for (const value of [
      'http://192.168.100.7',
      'https://10.0.0.1:8080',
      'http://172.16.0.1/',
      'http://172.31.255.254',
      'http://169.254.10.20',
      'http://[fe80::1]',
      'http://[fd00::1]:8443',
    ]) {
      expect(localDeviceAddress(value), value).toBe(true)
    }
  })

  it('refuses what the service refuses even though its shape is fine', () => {
    for (const value of [
      // Documentation, loopback, public, carrier-grade NAT, metadata, range
      // edges and unspecified addresses are not a router on this network.
      'http://192.0.2.1/',
      'http://127.0.0.1',
      'http://8.8.8.8',
      'http://100.64.0.1',
      'http://169.254.169.254',
      'http://172.15.0.1',
      'http://172.32.0.1',
      'http://0.0.0.0',
      'http://[::1]',
      'http://[2001:db8::1]',
      // A name would make the destination depend on a name server.
      'http://router.local',
      'https://my.keenetic.net',
    ]) {
      expect(localDeviceAddress(value), value).toBe(false)
    }
  })

  it('keeps the shape rules in force', () => {
    for (const value of [
      '10.255.0.1',
      'http://10.255.0.1/admin',
      'http://admin@10.255.0.1',
      'http://10.255.0.1?x=1',
      ' http://10.255.0.1',
    ]) {
      expect(localDeviceAddress(value), value).toBe(false)
    }
  })
})
