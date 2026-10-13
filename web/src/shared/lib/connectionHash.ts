// «Подключения» names the connection to open in the URL fragment, so a link
// from another screen can land on that connection. The server deliberately
// refuses query strings, so the fragment is the one part of the address a
// link, a bookmark or a refresh can carry.

export const connectionPageHash = (id: string): string => {
  if (id === '') return ''
  const values = new URLSearchParams()
  values.set('connection', id)
  return `#${values.toString()}`
}

export const parseConnectionPageHash = (hash: string): string =>
  new URLSearchParams(hash.replace(/^#/, '')).get('connection') ?? ''
