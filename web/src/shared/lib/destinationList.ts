/**
 * One reader for every way an operator hands Routevane a set of destinations:
 * a typed list, a JSON array, or a router's own routes file. A destination is
 * a domain, an IPv4 or IPv6 address, or a network in CIDR form — the three
 * shapes the list card shows and the list API accepts.
 *
 * The grammar is deliberately the server's rather than a looser one. The batch
 * endpoint refuses a whole submission over a single malformed value, so a token
 * this reader is unsure about is dropped and counted instead of sent.
 */

export type DestinationList = {
  values: string[]
  skipped: number
}

// A batch file is mostly not destinations: its labels and remarks are expected
// content, so they are dropped in silence. `skipped` counts only what looked
// like a destination and was not one.
const commentPrefixes = ['#', '//', ';', '::']
const remark = /^rem(\s|$)/i
const routeCommand = /^route(\s|$)/i
const routeAdd = /^route\s+add\s+(\S+)\s+mask\s+(\S+)(\s|$)/i
const hexGroup = /^[0-9a-f]{1,4}$/i
const hexOrColon = /^[0-9a-f:]+$/i
// Go's address parser refuses a leading zero in an octet or in a prefix length,
// so this one does too rather than send a value the server will reject.
const octet = /^(0|[1-9]\d{0,2})$/
const prefixBits = /^(0|[1-9]\d*)$/

export function parseDestinationList(text: string): DestinationList {
  const body = stripBOM(text)
  const collector = createCollector()
  const items = jsonArray(body.trim())
  if (items !== null) {
    for (const item of items) {
      if (typeof item === 'string') collector.take(item)
      else collector.skip()
    }
    return collector.result()
  }
  for (const rawLine of body.split(/\r?\n/)) {
    const line = stripBOM(rawLine).trim()
    if (line === '' || isComment(line)) continue
    if (routeCommand.test(line)) {
      // A route command Routevane cannot read is never reduced to its first
      // word: `route` would pass as a domain and take the whole batch down.
      const destination = routeDestination(line)
      if (destination === null) collector.skip()
      else collector.take(destination)
      continue
    }
    collector.take(line.split(/\s+/)[0] ?? '')
  }
  return collector.result()
}

/**
 * normalizeDomain is the domain half of the grammar on its own, for the one
 * form that may still only carry domains: a custom list's definition.
 */
export function normalizeDomain(value: string): string | null {
  const domain = value.trim().replace(/\.$/, '').toLowerCase()
  if (domain === '' || domain.length > 253 || /[^a-z0-9.-]/.test(domain))
    return null
  const labels = domain.split('.')
  const valid = labels.every(
    (label) =>
      label !== '' &&
      label.length <= 63 &&
      !label.startsWith('-') &&
      !label.endsWith('-'),
  )
  // An all-digit last label is a mistyped address, not a domain. Without this
  // `10.0.0.256` would be filed as a name and quietly routed as one.
  if (!valid || /^\d+$/.test(labels.at(-1) ?? '')) return null
  return domain
}

function createCollector() {
  const values: string[] = []
  const seen = new Set<string>()
  let skipped = 0
  return {
    result: (): DestinationList => ({ skipped, values }),
    skip: (): void => {
      skipped += 1
    },
    take: (token: string): void => {
      const value = normalizeDestination(token)
      if (value === null) {
        skipped += 1
        return
      }
      // The same destination named twice is one destination, not a refusal.
      const key = value.toLowerCase()
      if (seen.has(key)) return
      seen.add(key)
      values.push(value)
    },
  }
}

function normalizeDestination(token: string): string | null {
  const value = token.trim()
  if (value === '') return null
  if (value.includes('/')) return normalizePrefix(value)
  if (isIPv4(value)) return value
  if (value.includes(':')) return isIPv6(value) ? value : null
  return normalizeDomain(value)
}

function normalizePrefix(value: string): string | null {
  const slash = value.indexOf('/')
  const address = value.slice(0, slash)
  const bits = value.slice(slash + 1)
  if (!prefixBits.test(bits)) return null
  const length = Number(bits)
  if (isIPv4(address)) return length <= 32 ? value : null
  if (isIPv6(address)) return length <= 128 ? value : null
  return null
}

function isIPv4(value: string): boolean {
  const octets = value.split('.')
  return (
    octets.length === 4 &&
    octets.every((part) => octet.test(part) && Number(part) <= 255)
  )
}

// A structural check, not a canonicalizer: hexadecimal groups joined by colons,
// with at most one `::` standing in for the groups left out. Embedded IPv4 and
// zone identifiers are refused rather than guessed at.
function isIPv6(value: string): boolean {
  if (!hexOrColon.test(value) || !value.includes(':')) return false
  const compression = value.indexOf('::')
  if (compression === -1) {
    const groups = value.split(':')
    return groups.length === 8 && groups.every((group) => hexGroup.test(group))
  }
  if (compression !== value.lastIndexOf('::')) return false
  const [head = '', tail = ''] = value.split('::')
  const groups = [
    ...(head === '' ? [] : head.split(':')),
    ...(tail === '' ? [] : tail.split(':')),
  ]
  return groups.length <= 7 && groups.every((group) => hexGroup.test(group))
}

// `route ADD 34.0.240.0 MASK 255.255.240.0 0.0.0.0` is how a Keenetic states a
// network, and a host mask states a single address.
function routeDestination(line: string): string | null {
  const match = routeAdd.exec(line)
  if (match === null) return null
  const [, address = '', mask = ''] = match
  if (!isIPv4(address) || !isIPv4(mask)) return null
  const length = prefixLength(mask)
  if (length === null) return null
  return length === 32 ? address : `${address}/${length}`
}

function prefixLength(mask: string): number | null {
  const bits = mask
    .split('.')
    .reduce((total, part) => total * 256 + Number(part), 0)
  // A prefix mask is a run of ones followed by a run of zeros. Anything else is
  // a filter, not a network, and Routevane does not invent a network from it.
  for (let length = 0; length <= 32; length += 1) {
    if (bits === 2 ** 32 - 2 ** (32 - length)) return length
  }
  return null
}

function jsonArray(text: string): unknown[] | null {
  if (!text.startsWith('[')) return null
  try {
    const parsed: unknown = JSON.parse(text)
    return Array.isArray(parsed) ? parsed : null
  } catch {
    return null
  }
}

function isComment(line: string): boolean {
  return (
    commentPrefixes.some((prefix) => line.startsWith(prefix)) ||
    remark.test(line)
  )
}

// A file saved by a Windows tool opens with a byte order mark, which would
// otherwise make its first destination unreadable.
function stripBOM(value: string): string {
  return value.codePointAt(0) === 0xfeff ? value.slice(1) : value
}
