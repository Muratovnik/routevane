// Package netpolicy owns the destination policy shared by every outbound
// boundary. It was extracted once a second boundary needed the same rules: a
// policy that two callers each reimplement is a policy that will diverge.
package netpolicy

import (
	"errors"
	"net/netip"
)

var (
	// ErrUnsafeDestination reports a destination outside the public unicast
	// policy: it would let a caller-supplied URL reach the machine running
	// Routevane or its local network.
	ErrUnsafeDestination = errors.New("destination is not a public unicast address")
)

// These ranges are excluded from both network access and routing. Keep the
// classification in one place so a prefix cannot hide a forbidden subnetwork.
var excludedDestinations = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("255.255.255.255/32"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	// Retain the existing conservative NAT64-family boundary, including
	// 64:ff9b:1::/48, as well as Teredo and 6to4 transition destinations.
	netip.MustParsePrefix("64:ff9b::/32"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

var benchmarking = netip.MustParsePrefix("198.18.0.0/15")

// PublicUnicast reports whether one resolved address may be contacted.
//
// An IPv4-mapped IPv6 address is unmapped first and then judged as the IPv4
// destination it actually is, so ::ffff:127.0.0.1 is refused for the same reason
// 127.0.0.1 is.
//
// Documentation ranges stay allowed on purpose: they are not a local trust
// boundary, and tests need an address class that is neither local nor a real
// internet host.
func PublicUnicast(address netip.Addr) bool {
	return RoutableDestination(address) && !benchmarking.Contains(address.WithZone("").Unmap())
}

// RoutableDestination reports whether one address may appear in a routing plan
// as a destination traffic is steered to.
//
// This is a different question from PublicUnicast, which asks whether Routevane
// itself may open a connection. A plan is data about destinations, so the answer
// differs in exactly one range: RFC 2544 benchmarking space is allowed here.
// Steering it reaches nobody and costs nothing, and refusing it would only make
// it unusable as a synthetic destination — the same reason PublicUnicast keeps
// documentation ranges allowed.
//
// Everything that makes an address local, unreachable, or a metadata list is
// refused by both, and by the same classification: the two questions share this
// package precisely so their common answer cannot drift apart.
func RoutableDestination(address netip.Addr) bool {
	address = address.WithZone("").Unmap()
	if !address.IsValid() {
		return false
	}
	for _, excluded := range excludedDestinations {
		if excluded.Contains(address) {
			return false
		}
	}
	return true
}

// RoutablePrefix reports whether every address covered by a prefix can be a
// routing destination. Documentation and benchmarking ranges remain allowed.
func RoutablePrefix(prefix netip.Prefix) bool {
	if !prefix.IsValid() {
		return false
	}
	if prefix.Addr().Is4In6() {
		if prefix.Bits() < 96 {
			return false
		}
		prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
	}
	for _, excluded := range excludedDestinations {
		if prefix.Overlaps(excluded) {
			return false
		}
	}
	return true
}

// AllPublicUnicast reports whether every candidate address may be contacted.
// Refusing the whole answer, rather than picking an allowed address out of a
// mixed answer, is what makes a split DNS answer unusable for rebinding.
func AllPublicUnicast(candidates []netip.Addr) bool {
	if len(candidates) == 0 {
		return false
	}
	for _, candidate := range candidates {
		if !PublicUnicast(candidate) {
			return false
		}
	}
	return true
}

// LocalHostname reports whether a hostname is a name for this machine or its
// local network by construction, so it can be refused without resolving it.
func LocalHostname(host string) bool {
	switch host {
	case "", "localhost", "localhost.localdomain", "ip6-localhost", "ip6-loopback":
		return true
	}
	for _, suffix := range []string{".localhost", ".local", ".localdomain", ".internal", ".home.arpa"} {
		if len(host) > len(suffix) && host[len(host)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}
