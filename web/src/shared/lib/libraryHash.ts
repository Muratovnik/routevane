// «Списки» keeps which category is open and which list card is showing in the
// URL fragment. The server deliberately refuses query strings, so the fragment
// is the one part of the address a bookmark, a refresh or a second tab can
// carry to the same place.

export type LibraryLocation = {
  category: string[]
  list: string
}

export function libraryPageHash(
  options: { category?: string | string[]; list?: string } = {},
): string {
  const values = new URLSearchParams()
  const categories = Array.isArray(options.category)
    ? options.category
    : options.category
      ? [options.category]
      : []
  for (const category of new Set(categories))
    values.append('category', category)
  if (options.list !== undefined && options.list !== '')
    values.set('list', options.list)
  const encoded = values.toString()
  return encoded === '' ? '' : `#${encoded}`
}

export function parseLibraryPageHash(hash: string): LibraryLocation {
  const values = new URLSearchParams(hash.replace(/^#/, ''))
  return {
    category: [...new Set(values.getAll('category').filter(Boolean))],
    list: values.get('list') ?? '',
  }
}
