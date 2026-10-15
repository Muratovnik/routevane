import { describe, expect, it } from 'vitest'

import {
  compareRuleValues,
  sortByRuleValue,
} from '@/features/view-profile/lib/ruleOrder'

const sorted = (values: string[]): string[] =>
  [...values].sort(compareRuleValues)

const HIGHEST_IPV6 = 'ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff'

describe('rule order', () => {
  it('orders IPv4 addresses by their octets as numbers', () => {
    expect(
      sorted([
        '10.0.0.10',
        '10.0.1.0',
        '10.0.0.2',
        '9.255.255.255',
        '10.0.0.0',
        '255.255.255.255',
        '0.0.0.0',
      ]),
    ).toEqual([
      '0.0.0.0',
      '9.255.255.255',
      '10.0.0.0',
      '10.0.0.2',
      '10.0.0.10',
      '10.0.1.0',
      '255.255.255.255',
    ])
  })

  it('orders IPv6 addresses by their groups, expanding `::`', () => {
    expect(
      sorted([
        'fe80::',
        '2001:db8::1:0',
        '2001:db8::10',
        '::1',
        '2001:db8::2',
        '::',
      ]),
    ).toEqual([
      '::',
      '::1',
      '2001:db8::2',
      '2001:db8::10',
      '2001:db8::1:0',
      'fe80::',
    ])
  })

  it('reads a compressed, a long and an upper-case IPv6 form as one address', () => {
    expect(compareRuleValues('2001:db8::1', '2001:0db8:0:0:0:0:0:1')).toBe(0)
    expect(compareRuleValues('2001:DB8::1', '2001:db8::1')).toBe(0)
    expect(compareRuleValues('::', '0:0:0:0:0:0:0:0')).toBe(0)
  })

  it('puts a network at its address, wider before narrower, and an address last', () => {
    expect(
      sorted([
        '10.0.0.0',
        '10.0.0.0/24',
        '10.1.0.0/16',
        '10.0.0.0/8',
        '9.0.0.0/8',
        '10.0.0.0/32',
      ]),
    ).toEqual([
      '9.0.0.0/8',
      '10.0.0.0/8',
      '10.0.0.0/24',
      '10.0.0.0',
      '10.0.0.0/32',
      '10.1.0.0/16',
    ])
    expect(sorted(['2001:db8::', '2001:db8::/48', '2001:db8::/32'])).toEqual([
      '2001:db8::/32',
      '2001:db8::/48',
      '2001:db8::',
    ])
  })

  it('puts IPv4, then IPv6, then every other value in character order', () => {
    expect(
      sorted([
        'youtube.com',
        '2001:db8::1',
        '10.0.0.10',
        'a.example',
        '10.0.0.2/31',
        '::1',
        'Z.example',
        '255.255.255.255',
      ]),
    ).toEqual([
      '10.0.0.2/31',
      '10.0.0.10',
      '255.255.255.255',
      '::1',
      '2001:db8::1',
      // Character order, not the language's: an upper-case letter precedes
      // every lower-case one in English and Russian alike.
      'Z.example',
      'a.example',
      'youtube.com',
    ])
  })

  it.each([
    '10.0.0.256',
    '1.2.3',
    '1.2.3.4.5',
    '010.0.0.1',
    '10.0.0.0/33',
    '10.0.0.0/08',
    '10.0.0.0/',
    '10.0.0.0/8/8',
    '2001:db8::/129',
    '1:2:3:4:5:6:7:8:9',
    '1:2:3:4:5:6:7',
    '1:2:3:4::5:6:7:8',
    '1::2::3',
    '1:::2',
    '12345::',
    '::ffff:192.0.2.1',
    'fe80::1%eth0',
  ])('reads %s as an ordinary value, after every address', (value) => {
    expect(compareRuleValues(HIGHEST_IPV6, value)).toBeLessThan(0)
    expect(compareRuleValues(value, HIGHEST_IPV6)).toBeGreaterThan(0)
    expect(compareRuleValues(value, '255.255.255.255')).toBeGreaterThan(0)
  })

  it('sorts items by value, breaks ties as told, then keeps their order', () => {
    const items = [
      { list: 'b', value: '10.0.0.1' },
      { list: 'x', value: 'example.com' },
      { list: 'a', value: '10.0.0.1' },
      { list: 'second', value: '2001:db8::1' },
      { list: 'first', value: '2001:0db8::1' },
      { list: 'y', value: '10.0.0.0/8' },
    ]
    expect(
      sortByRuleValue(items, (item) => item.value).map((item) => item.list),
    ).toEqual(['y', 'b', 'a', 'second', 'first', 'x'])
    expect(
      sortByRuleValue(
        items,
        (item) => item.value,
        (left, right) => left.list.localeCompare(right.list),
      ).map((item) => item.list),
    ).toEqual(['y', 'a', 'b', 'first', 'second', 'x'])
    // The input is left as it was.
    expect(items[0]?.list).toBe('b')
  })
})
