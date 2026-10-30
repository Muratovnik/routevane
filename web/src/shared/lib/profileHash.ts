// The profile page keeps its facet and one-time setup in the URL fragment. The
// server deliberately refuses query strings, so the fragment is the one part
// of the address a bookmark or refresh can carry to the same facet.

// `send` names the format whose send panel is open over the page, so a reload
// or a link reopens it.
export type ProfilePageLocation = {
  send: string
  setup: string
  tab: string
}

export const profilePageHash = (
  options: { send?: string; setup?: string; tab?: string } = {},
): string => {
  const values = new URLSearchParams()
  if (options.setup !== undefined && options.setup !== '')
    values.set('setup', options.setup)
  if (
    options.tab !== undefined &&
    options.tab !== '' &&
    options.tab !== 'overview'
  )
    values.set('tab', options.tab)
  if (options.send !== undefined && options.send !== '')
    values.set('send', options.send)
  const encoded = values.toString()
  return encoded === '' ? '' : `#${encoded}`
}

export const parseProfilePageHash = (hash: string): ProfilePageLocation => {
  const values = new URLSearchParams(hash.replace(/^#/, ''))
  return {
    send: values.get('send') ?? '',
    setup: values.get('setup') ?? '',
    tab: values.get('tab') ?? 'overview',
  }
}

// parseLegacyProfileHash reads the retired drawer address `/#list={id}`, so an
// old bookmark still resolves to the profile's page instead of an empty shelf.
// Library list slugs and malformed values must never become profile routes.
export const parseLegacyProfileHash = (hash: string): string => {
  const values = new URLSearchParams(hash.replace(/^#/, ''))
  const id = values.get('profile') ?? values.get('list') ?? ''
  return /^[a-f0-9]{32}$/.test(id) ? id : ''
}
