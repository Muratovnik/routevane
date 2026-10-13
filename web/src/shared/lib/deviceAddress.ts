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

// Four decimal octets without leading zeros, which is all the service's own
// parser reads as an IPv4 literal.
const ipv4Octets = (host: string): number[] | null => {
  const parts = host.split('.')
  if (parts.length !== 4) return null
  const octets = parts.map((part) =>
    /^(?:0|[1-9]\d{0,2})$/.test(part) ? Number(part) : Number.NaN,
  )
  return octets.every((octet) => octet <= 255) ? octets : null
}

const privateIPv4 = (host: string): boolean => {
  const octets = ipv4Octets(host)
  if (octets === null) return false
  const [first = 0, second = 0, third = 0, fourth = 0] = octets
  if (first === 10) return true
  if (first === 172) return second >= 16 && second <= 31
  if (first === 192) return second === 168
  // Link-local unicast is how a device answers before it has a lease, but the
  // cloud metadata address is never a router.
  if (first === 169 && second === 254) return !(third === 169 && fourth === 254)
  return false
}

const privateIPv6 = (literal: string): boolean => {
  let canonical: string
  try {
    canonical = new URL(`http://[${literal}]`).hostname.slice(1, -1)
  } catch {
    return false
  }
  // `::ffff:192.168.1.1` is the same device as `192.168.1.1`.
  const mapped = /^::ffff:([0-9a-f]{1,4}):([0-9a-f]{1,4})$/.exec(canonical)
  if (mapped !== null) {
    const high = Number.parseInt(mapped[1]!, 16)
    const low = Number.parseInt(mapped[2]!, 16)
    return privateIPv4(`${high >> 8}.${high & 0xff}.${low >> 8}.${low & 0xff}`)
  }
  const first = Number.parseInt(canonical.split(':')[0] || '0', 16)
  // Unique-local (fc00::/7) and link-local unicast (fe80::/10).
  return (first & 0xfe00) === 0xfc00 || (first & 0xffc0) === 0xfe80
}

/**
 * Whether a router address names a device on the operator's own network, the
 * way the service decides it (`netpolicy.ValidateDeviceURL`): an explicit http
 * or https address whose host is a private or link-local IP literal. A name is
 * refused on purpose, because it would make the destination depend on a name
 * server the router itself may be serving, and every other special-use range
 * stays refused so that a list on this machine or a metadata endpoint can never
 * be reached through a device address.
 *
 * It answers for the shape the form can read. The host is taken from the text
 * the operator typed rather than from a normalizing URL parser: that parser
 * reads `3232235777` and `0xC0.168.1.1` as `192.168.1.1`, and the service does
 * not, so accepting them here would send an address the service refuses. An
 * IPv6 zone identifier is not accepted either, because the form's own shape
 * check does not admit it; the service is the authority for what it permits.
 */
export const localDeviceAddress = (value: string): boolean => {
  if (!HTTP_SCHEME_PATTERN.test(value) || !validAddress(value)) return false
  const authority = /^https?:\/\/([^/?#]*)/i.exec(value)?.[1] ?? ''
  if (authority.startsWith('[')) {
    const end = authority.indexOf(']')
    if (end === -1) return false
    const port = authority.slice(end + 1)
    return (
      (port === '' || /^:\d+$/.test(port)) &&
      privateIPv6(authority.slice(1, end))
    )
  }
  const [host = '', ...rest] = authority.split(':')
  if (rest.length > 1 || (rest.length === 1 && !/^\d+$/.test(rest[0]!)))
    return false
  return privateIPv4(host)
}
