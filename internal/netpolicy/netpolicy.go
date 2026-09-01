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
	address = address.Unmap()
	if !address.IsValid() {
		return false
	}
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsUnspecified() || address.IsMulticast() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() ||
		address.IsInterfaceLocalMulticast() {
		return false
	}
	if address.Is4() {
		octets := address.As4()
		switch {
		case octets[0] == 0, octets[0] == 127:
			return false
		case octets[0] == 100 && octets[1] >= 64 && octets[1] <= 127:
			// RFC 6598 carrier-grade NAT space.
			return false
		case octets[0] == 169 && octets[1] == 254:
			// Link-local, including the 169.254.169.254 metadata address.
			return false
		case octets[0] == 192 && octets[1] == 0 && octets[2] == 0:
			// RFC 6890 IETF protocol assignments.
			return false
		case octets[0] == 198 && (octets[1] == 18 || octets[1] == 19):
			// RFC 2544 benchmarking space.
			return false
		}
		return true
	}
	// Reject IPv6 transition ranges that embed an IPv4 destination the policy
	// above would otherwise never see.
	bytes := address.As16()
	switch {
	case bytes[0] == 0x20 && bytes[1] == 0x02:
		// 2002::/16 6to4.
		return false
	case bytes[0] == 0x20 && bytes[1] == 0x01 && bytes[2] == 0x00 && bytes[3] == 0x00:
		// 2001::/32 Teredo.
		return false
	case bytes[0] == 0x00 && bytes[1] == 0x64 && bytes[2] == 0xff && bytes[3] == 0x9b:
		// 64:ff9b::/96 NAT64.
		return false
	}
	return true
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
// Everything that makes an address local, unreachable, or a metadata service is
// refused by both, and by the same classification: the two questions share this
// package precisely so their common answer cannot drift apart.
func RoutableDestination(address netip.Addr) bool {
	address = address.Unmap()
	if PublicUnicast(address) {
		return true
	}
	if address.Is4() {
		octets := address.As4()
		// 198.18.0.0/15 is refused above only for being benchmarking space, so
		// no other reason to refuse it is being waived here.
		return octets[0] == 198 && (octets[1] == 18 || octets[1] == 19)
	}
	return false
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
