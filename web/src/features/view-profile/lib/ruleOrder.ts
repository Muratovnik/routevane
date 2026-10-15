/**
 * The order the rules of a build are read in. An address is a number, not a
 * word: 10.0.0.2 stands before 10.0.0.10, and 2001:db8::1 is the same place as
 * its long form. IPv4 comes first, then IPv6; a network stands at its address,
 * the wider one before the narrower, and a single address as the narrowest
 * network of all, so 10.0.0.0/8 precedes 10.0.0.0/24, which precedes 10.0.0.0.
 * Every other value — a domain, or anything that does not read as an address —
 * follows as one group in plain character order, which does not move when the
 * interface changes language.
 *
 * The grammar is the one the list reader accepts: four decimal octets without
 * leading zeros, or hexadecimal groups with at most one `::`. An embedded IPv4
 * tail or a zone identifier is not guessed at and reads as an ordinary value.
 */

type Address = {
  family: 4 | 6
  parts: number[]
  prefix: number
}

const OCTET = /^(0|[1-9]\d{0,2})$/
const HEX_GROUP = /^[0-9a-f]{1,4}$/i
const PREFIX_BITS = /^(0|[1-9]\d*)$/
const IPV6_GROUPS = 8

const ipv4Parts = (text: string): number[] | null => {
  const octets = text.split('.')
  if (octets.length !== 4 || !octets.every((octet) => OCTET.test(octet)))
    return null
  const parts = octets.map(Number)
  return parts.every((part) => part <= 255) ? parts : null
}

const ipv6Parts = (text: string): number[] | null => {
  const halves = text.split('::')
  if (halves.length > 2) return null
  const groups = (half: string): string[] =>
    half === '' ? [] : half.split(':')
  const head = groups(halves[0] ?? '')
  const tail = halves.length === 2 ? groups(halves[1] ?? '') : []
  const written = [...head, ...tail]
  if (!written.every((group) => HEX_GROUP.test(group))) return null
  const hex = (group: string): number => Number.parseInt(group, 16)
  if (halves.length === 1)
    return written.length === IPV6_GROUPS ? written.map(hex) : null
  // `::` stands for at least one group of zeros.
  if (written.length >= IPV6_GROUPS) return null
  return [
    ...head.map(hex),
    ...Array.from({ length: IPV6_GROUPS - written.length }, () => 0),
    ...tail.map(hex),
  ]
}

const address = (value: string): Address | null => {
  const [host = '', bits, ...rest] = value.split('/')
  if (rest.length > 0) return null
  const family = host.includes(':') ? 6 : 4
  const parts = family === 4 ? ipv4Parts(host) : ipv6Parts(host)
  if (parts === null) return null
  const width = family === 4 ? 32 : 128
  if (bits === undefined) return { family, parts, prefix: width }
  if (!PREFIX_BITS.test(bits) || Number(bits) > width) return null
  return { family, parts, prefix: Number(bits) }
}

const compareText = (left: string, right: string): number => {
  if (left === right) return 0
  return left < right ? -1 : 1
}

const compareAddresses = (left: Address, right: Address): number => {
  if (left.family !== right.family) return left.family - right.family
  for (let index = 0; index < left.parts.length; index += 1) {
    const difference = (left.parts[index] ?? 0) - (right.parts[index] ?? 0)
    if (difference !== 0) return difference
  }
  return left.prefix - right.prefix
}

const compareParsed = (
  left: string,
  leftAddress: Address | null,
  right: string,
  rightAddress: Address | null,
): number => {
  if (leftAddress !== null && rightAddress !== null)
    return compareAddresses(leftAddress, rightAddress)
  if (leftAddress !== null) return -1
  if (rightAddress !== null) return 1
  return compareText(left, right)
}

/** Compares two rule values in the order described above. */
export const compareRuleValues = (left: string, right: string): number =>
  compareParsed(left, address(left), right, address(right))

/**
 * Sorts items by their rule value, reading each distinct value once. Items
 * whose values are equal in that order keep the order `tieBreak` gives them,
 * and then their incoming order.
 */
export const sortByRuleValue = <T>(
  items: readonly T[],
  valueOf: (item: T) => string,
  tieBreak: (left: T, right: T) => number = () => 0,
): T[] => {
  const parsed = new Map<string, Address | null>()
  const addressOf = (value: string): Address | null => {
    if (!parsed.has(value)) parsed.set(value, address(value))
    return parsed.get(value) ?? null
  }
  return [...items].sort((left, right) => {
    const leftValue = valueOf(left)
    const rightValue = valueOf(right)
    return (
      compareParsed(
        leftValue,
        addressOf(leftValue),
        rightValue,
        addressOf(rightValue),
      ) || tieBreak(left, right)
    )
  })
}
