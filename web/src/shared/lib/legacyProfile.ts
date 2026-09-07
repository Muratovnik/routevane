// The v3 importer generated names in one closed shape. Recognising that shape
// lets the current UI explain migrated data without rewriting the stored name
// or mistaking every name that happens to start with "Imported" for one of its
// own records.
const legacyImportedName = /^Imported ([1-9]\d*) · [^·]+ · [^·]+$/

export function legacyImportedOrdinal(name: string): number | null {
  const match = legacyImportedName.exec(name)
  if (match === null) return null
  const ordinal = Number(match[1])
  return Number.isSafeInteger(ordinal) ? ordinal : null
}
