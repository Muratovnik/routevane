// The list page keeps its facet and one-time setup in the URL fragment. The
// server deliberately refuses query strings, so the fragment is the one part
// of the address a bookmark or refresh can carry to the same facet.

export type ListPageLocation = {
  setup: string
  tab: string
}

export function listPageHash(
  options: { setup?: string; tab?: string } = {},
): string {
  const values = new URLSearchParams()
  if (options.setup !== undefined && options.setup !== '')
    values.set('setup', options.setup)
  if (
    options.tab !== undefined &&
    options.tab !== '' &&
    options.tab !== 'overview'
  )
    values.set('tab', options.tab)
  const encoded = values.toString()
  return encoded === '' ? '' : `#${encoded}`
}

export function parseListPageHash(hash: string): ListPageLocation {
  const values = new URLSearchParams(hash.replace(/^#/, ''))
  return {
    setup: values.get('setup') ?? '',
    tab: values.get('tab') ?? 'overview',
  }
}

// parseLegacyListHash reads the retired drawer address `/#list={id}`, so an
// old bookmark still resolves to the list's page instead of an empty shelf.
export function parseLegacyListHash(hash: string): string {
  return new URLSearchParams(hash.replace(/^#/, '')).get('list') ?? ''
}
