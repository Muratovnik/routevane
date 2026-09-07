// The profile page keeps its facet and one-time setup in the URL fragment. The
// server deliberately refuses query strings, so the fragment is the one part
// of the address a bookmark or refresh can carry to the same facet.

export type ProfilePageLocation = {
  setup: string
  tab: string
}

export function profilePageHash(
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

export function parseProfilePageHash(hash: string): ProfilePageLocation {
  const values = new URLSearchParams(hash.replace(/^#/, ''))
  return {
    setup: values.get('setup') ?? '',
    tab: values.get('tab') ?? 'overview',
  }
}

// parseLegacyProfileHash reads the retired drawer address `/#list={id}`, so an
// old bookmark still resolves to the profile's page instead of an empty shelf.
export function parseLegacyProfileHash(hash: string): string {
  return new URLSearchParams(hash.replace(/^#/, '')).get('profile') ?? ''
}
