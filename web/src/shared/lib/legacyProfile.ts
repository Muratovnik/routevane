// The v3 importer generated names in one closed shape. Recognising that shape
// lets the current UI explain migrated data without rewriting the stored name
// or mistaking every name that happens to start with "Imported" for one of its
// own records.
const LEGACY_IMPORTED_NAME = /^Imported ([1-9]\d*) · [^·]+ · [^·]+$/

export const legacyImportedOrdinal = (name: string): number | null => {
  const match = LEGACY_IMPORTED_NAME.exec(name)
  if (match === null) return null
  const ordinal = Number(match[1])
  return Number.isSafeInteger(ordinal) ? ordinal : null
}
