import { describe, expect, it } from 'vitest'

import { parseDestinationList } from './destinationList'

describe('destination list', () => {
  it('reads domains, addresses and networks from one plain list', () => {
    expect(
      parseDestinationList(
        'Discord.com\n203.0.113.7\n203.0.113.0/29\n2001:db8::1\n2001:db8::/32\n',
      ),
    ).toEqual({
      skipped: 0,
      values: [
        'discord.com',
        '203.0.113.7',
        '203.0.113.0/29',
        '2001:db8::1',
        '2001:db8::/32',
      ],
    })
  })

  it('drops blank lines, every comment shape, and a trailing note', () => {
    expect(
      parseDestinationList(
        [
          '# a list',
          '// second',
          '; third',
          ':: fourth',
          'REM fifth',
          '',
          '  corp.example  ',
          '1.2.3.0/24  # comment',
        ].join('\n'),
      ),
    ).toEqual({ skipped: 0, values: ['corp.example', '1.2.3.0/24'] })
  })

  it('turns a routes file into networks and single addresses', () => {
    expect(
      parseDestinationList(
        [
          '@echo off',
          ':: exported routes',
          'route ADD 34.0.240.0 MASK 255.255.240.0 0.0.0.0',
          'route add 5.8.16.1 MASK 255.255.255.255 0.0.0.0 METRIC 1',
          '',
        ].join('\r\n'),
      ),
    ).toEqual({ skipped: 1, values: ['34.0.240.0/20', '5.8.16.1'] })
  })

  it('reads a JSON array and counts what is not a string', () => {
    expect(
      parseDestinationList('["corp.example", "198.51.100.0/24", 42]'),
    ).toEqual({ skipped: 1, values: ['corp.example', '198.51.100.0/24'] })
  })

  it('keeps the first spelling of a repeated destination', () => {
    expect(
      parseDestinationList('Corp.Example\ncorp.example\nCORP.EXAMPLE\n'),
    ).toEqual({ skipped: 0, values: ['corp.example'] })
  })

  it('counts every line it cannot read, a non-contiguous mask included', () => {
    expect(
      parseDestinationList(
        [
          'route ADD 198.18.32.0 MASK 255.0.255.0 0.0.0.0',
          'route DELETE 198.18.0.0',
          '10.0.0.256',
          '203.0.113.0/33',
          '-bad.example',
          'good.example',
        ].join('\n'),
      ),
    ).toEqual({ skipped: 5, values: ['good.example'] })
  })
})
