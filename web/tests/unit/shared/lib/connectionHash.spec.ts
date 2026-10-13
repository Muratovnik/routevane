import { describe, expect, it } from 'vitest'

import {
  connectionPageHash,
  parseConnectionPageHash,
} from '@/shared/lib/connectionHash'

describe('connection page hash', () => {
  it('carries the connection and omits an empty one', () => {
    expect(connectionPageHash('')).toBe('')
    expect(connectionPageHash('device-1')).toBe('#connection=device-1')
  })

  it('reads back what it writes, including characters that need escaping', () => {
    const id = 'home router&1'
    expect(parseConnectionPageHash(connectionPageHash(id))).toBe(id)
  })

  it.each(['', '#', '#tab=outputs', '#list=discord'])(
    'names no connection in %s',
    (hash) => {
      expect(parseConnectionPageHash(hash)).toBe('')
    },
  )
})
