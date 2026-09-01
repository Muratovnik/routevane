import { describe, expect, it } from 'vitest'

import {
  listPageHash,
  parseLegacyListHash,
  parseListPageHash,
} from './listHash'

describe('list page hash', () => {
  it('carries the facet and the one-time setup, and omits the defaults', () => {
    expect(listPageHash()).toBe('')
    expect(listPageHash({ tab: '' })).toBe('')
    expect(listPageHash({ setup: '', tab: 'overview' })).toBe('')
    expect(listPageHash({ tab: 'outputs' })).toBe('#tab=outputs')
    expect(listPageHash({ setup: 'keenetic', tab: 'outputs' })).toBe(
      '#setup=keenetic&tab=outputs',
    )
  })

  it('parses what it writes and defaults the rest', () => {
    expect(parseListPageHash('#setup=keenetic&tab=outputs')).toEqual({
      setup: 'keenetic',
      tab: 'outputs',
    })
    expect(parseListPageHash('')).toEqual({ setup: '', tab: 'overview' })
  })

  it('still reads the retired drawer address', () => {
    expect(parseLegacyListHash('#list=abc123')).toBe('abc123')
    expect(parseLegacyListHash('#tab=outputs')).toBe('')
  })
})
