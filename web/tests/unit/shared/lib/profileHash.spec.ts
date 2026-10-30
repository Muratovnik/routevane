import { describe, expect, it } from 'vitest'

import {
  profilePageHash,
  parseLegacyProfileHash,
  parseProfilePageHash,
} from '@/shared/lib/profileHash'

describe('list page hash', () => {
  it('carries the facet and the one-time setup, and omits the defaults', () => {
    expect(profilePageHash()).toBe('')
    expect(profilePageHash({ tab: '' })).toBe('')
    expect(profilePageHash({ setup: '', tab: 'overview' })).toBe('')
    expect(profilePageHash({ tab: 'outputs' })).toBe('#tab=outputs')
    expect(profilePageHash({ setup: 'keenetic', tab: 'outputs' })).toBe(
      '#setup=keenetic&tab=outputs',
    )
  })

  it('parses what it writes and defaults the rest', () => {
    expect(parseProfilePageHash('#setup=keenetic&tab=outputs')).toEqual({
      send: '',
      setup: 'keenetic',
      tab: 'outputs',
    })
    expect(parseProfilePageHash('')).toEqual({
      send: '',
      setup: '',
      tab: 'overview',
    })
    // The open send panel is part of the address, beside the tab.
    expect(profilePageHash({ send: 'output-1', tab: 'outputs' })).toBe(
      '#tab=outputs&send=output-1',
    )
    expect(parseProfilePageHash('#tab=outputs&send=output-1')).toEqual({
      send: 'output-1',
      setup: '',
      tab: 'outputs',
    })
  })

  it.each(['profile', 'list'])(
    'still reads the retired %s drawer address',
    (key) => {
      const id = 'a1'.repeat(16)
      expect(parseLegacyProfileHash(`#${key}=${id}`)).toBe(id)
    },
  )

  it.each([
    '',
    '#tab=outputs',
    '#list=discord',
    '#profile=abc123',
    `#list=${'a'.repeat(31)}`,
    `#profile=${'a'.repeat(33)}`,
    `#profile=${'g'.repeat(32)}`,
    `#list=${'A'.repeat(32)}`,
    '#profile=..%2Fsettings',
  ])('ignores a fragment that is not a canonical profile id: %s', (hash) => {
    expect(parseLegacyProfileHash(hash)).toBe('')
  })

  it('gives the explicit profile parameter precedence over the legacy name', () => {
    const profile = 'a'.repeat(32)
    const list = 'b'.repeat(32)
    expect(parseLegacyProfileHash(`#profile=${profile}&list=${list}`)).toBe(
      profile,
    )
    expect(parseLegacyProfileHash(`#profile=invalid&list=${list}`)).toBe('')
  })
})
