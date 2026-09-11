/**
 * The shape a destination address has to have before the surface sends it.
 *
 * A destination is whatever its deployer declares: a device on the network, or
 * a configuration file on this computer. What this rejects is a malformed
 * address, never an address that merely turns out to be unreachable — that
 * answer belongs to the destination. The URL parser owns host and port syntax,
 * including bracketed IPv6; which networks a device may live on, and whether a
 * host may be named rather than numbered, stay on the server, which refuses
 * them with the whole policy in view.
 *
 * The scheme is part of the shape rather than part of that policy. Every
 * shipped deployer requires an explicit one — a router is reached over http or
 * https, a local configuration through file — and the service refuses an
 * address that leaves it out, so a form that accepted `192.168.1.1` was
 * promising something the service would not honour and saying nothing about
 * why. Stating it here lets the field answer instead.
 */
const HTTP_SCHEME_PATTERN = /^https?:\/\//i

export const validAddress = (value: string): boolean => {
  if (value === '' || value !== value.trim()) return false
  // A local configuration file is a legitimate destination, and its own
  // deployer offers it as the example, so the form must accept what it shows.
  if (/^file:/i.test(value)) {
    try {
      return new URL(value).pathname.length > 1
    } catch {
      return false
    }
  }
  if (!HTTP_SCHEME_PATTERN.test(value)) return false
  if (/\s|\\/.test(value)) return false
  try {
    const url = new URL(value)
    return (
      (url.protocol === 'http:' || url.protocol === 'https:') &&
      url.hostname !== '' &&
      url.username === '' &&
      url.password === '' &&
      (url.pathname === '' || url.pathname === '/') &&
      url.search === '' &&
      url.hash === '' &&
      url.port !== '0'
    )
  } catch {
    return false
  }
}
