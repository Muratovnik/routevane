// «Списки» keeps which category is open and which list card is showing in the
// URL fragment. The server deliberately refuses query strings, so the fragment
// is the one part of the address a bookmark, a refresh or a second tab can
// carry to the same place.

export type LibraryLocation = {
  category: string
  list: string
}

export function libraryPageHash(
  options: { category?: string; list?: string } = {},
): string {
  const values = new URLSearchParams()
  if (options.category !== undefined && options.category !== '')
    values.set('category', options.category)
  if (options.list !== undefined && options.list !== '')
    values.set('list', options.list)
  const encoded = values.toString()
  return encoded === '' ? '' : `#${encoded}`
}

export function parseLibraryPageHash(hash: string): LibraryLocation {
  const values = new URLSearchParams(hash.replace(/^#/, ''))
  return {
    category: values.get('category') ?? '',
    list: values.get('list') ?? '',
  }
}
