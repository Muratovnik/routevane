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
      setup: 'keenetic',
      tab: 'outputs',
    })
    expect(parseProfilePageHash('')).toEqual({ setup: '', tab: 'overview' })
  })

  it('still reads the retired drawer address', () => {
    expect(parseLegacyProfileHash('#profile=abc123')).toBe('abc123')
    expect(parseLegacyProfileHash('#tab=outputs')).toBe('')
  })
})
