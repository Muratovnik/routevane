package planner

import (
	"net/netip"

	"go4.org/netipx"
)

// CollapseLossless returns the smallest deterministic set of disjoint
// prefixes that represents exactly the supplied addresses. An address starts
// as /32 or /128; a merge happens only when both complete siblings are present.
func CollapseLossless(addrs []netip.Addr) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(addrs))
	for _, raw := range addrs {
		if !raw.IsValid() {
			continue
		}
		a := raw.Unmap()
		prefixes = append(prefixes, netip.PrefixFrom(a, a.BitLen()).Masked())
	}
	return CollapseLosslessPrefixes(prefixes)
}

// CollapseLosslessPrefixes is the same reduction over values that are already
// networks. A feed publishes prefixes, not addresses, so a collapse that only
// understood /32s left every network source uncollapsed and every budget
// estimate high.
//
// The result is the minimal sorted cover of exactly the supplied addresses:
// IPv4 before IPv6, then ascending by address and length. Nothing downstream
// may widen it, so a value this package cannot represent as a network is
// dropped rather than rounded outwards.
func CollapseLosslessPrefixes(prefixes []netip.Prefix) []netip.Prefix {
	var builder netipx.IPSetBuilder
	for _, raw := range prefixes {
		if !raw.IsValid() {
			continue
		}
		// A 4-in-6 value is one address in two spellings, and a length that
		// only fits the mapped form describes no IPv4 network at all.
		p := netip.PrefixFrom(raw.Addr().Unmap(), raw.Bits()).Masked()
		if !p.IsValid() {
			continue
		}
		builder.AddPrefix(p)
	}
	// The builder accumulates an error only for an invalid input, and every
	// input reaching it here has been validated above.
	set, _ := builder.IPSet()
	return set.Prefixes()
}
