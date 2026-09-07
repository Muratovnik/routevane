import { describe, expect, it } from 'vitest'

import { legacyImportedOrdinal } from '@/shared/lib/legacyProfile'

describe('legacy list names', () => {
  it('recognises the exact name emitted by the v3 importer', () => {
    expect(
      legacyImportedOrdinal('Imported 12 · discord, youtube · keenetic'),
    ).toBe(12)
  })

  it('does not reinterpret ordinary user-authored names', () => {
    expect(legacyImportedOrdinal('Imported contacts')).toBeNull()
    expect(legacyImportedOrdinal('Imported 0 · discord · keenetic')).toBeNull()
    expect(legacyImportedOrdinal('Imported 2 · discord')).toBeNull()
  })
})
