package planner

import (
	"math/big"
	"math/rand/v2"
	"net/netip"
	"testing"
)

// The collapse is checked against an independent cover built from
// arbitrary-precision spans rather than against the library that produces it.
// Agreement is then evidence about the published set: the same addresses, the
// smallest set of prefixes that can carry them, and one order.

type addressSpan struct {
	width  int
	lo, hi *big.Int
}

func spanOfPrefix(p netip.Prefix) addressSpan {
	addr := p.Addr()
	width := addr.BitLen()
	var raw []byte
	if addr.Is4() {
		value := addr.As4()
		raw = value[:]
	} else {
		value := addr.As16()
		raw = value[:]
	}
	lo := new(big.Int).SetBytes(raw)
	size := new(big.Int).Lsh(big.NewInt(1), uint(width-p.Bits()))
	hi := new(big.Int).Sub(new(big.Int).Add(lo, size), big.NewInt(1))
	return addressSpan{width: width, lo: lo, hi: hi}
}

// mergedSpans is the address set of the input, as the fewest closed spans that
// describe it. Two spans merge when they overlap or touch; spans of different
// widths never do, because an IPv4 address is not the IPv6 address that follows
// the last IPv4 one.
func mergedSpans(prefixes []netip.Prefix, width int) []addressSpan {
	spans := make([]addressSpan, 0, len(prefixes))
	for _, p := range prefixes {
		if span := spanOfPrefix(p); span.width == width {
			spans = append(spans, span)
		}
	}
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j].lo.Cmp(spans[j-1].lo) < 0; j-- {
			spans[j], spans[j-1] = spans[j-1], spans[j]
		}
	}
	merged := make([]addressSpan, 0, len(spans))
	for _, span := range spans {
		if len(merged) == 0 {
			merged = append(merged, span)
			continue
		}
		last := merged[len(merged)-1]
		touching := new(big.Int).Add(last.hi, big.NewInt(1))
		if span.lo.Cmp(touching) > 0 {
			merged = append(merged, span)
			continue
		}
		if span.hi.Cmp(last.hi) > 0 {
			merged[len(merged)-1].hi = span.hi
		}
	}
	return merged
}

// coveringPrefixes decomposes one span into the unique minimal set of prefixes
// that covers it exactly: at each position the largest aligned block that still
// fits inside the span.
func coveringPrefixes(span addressSpan) []netip.Prefix {
	one := big.NewInt(1)
	out := make([]netip.Prefix, 0)
	lo := new(big.Int).Set(span.lo)
	for lo.Cmp(span.hi) <= 0 {
		bits := span.width
		for bits > 0 {
			size := new(big.Int).Lsh(one, uint(span.width-bits+1))
			if new(big.Int).Mod(lo, size).Sign() != 0 {
				break
			}
			end := new(big.Int).Sub(new(big.Int).Add(lo, size), one)
			if end.Cmp(span.hi) > 0 {
				break
			}
			bits--
		}
		out = append(out, netip.PrefixFrom(addrOfSpanValue(span.width, lo), bits))
		lo = new(big.Int).Add(lo, new(big.Int).Lsh(one, uint(span.width-bits)))
	}
	return out
}

func addrOfSpanValue(width int, value *big.Int) netip.Addr {
	if width == 32 {
		var raw [4]byte
		value.FillBytes(raw[:])
		return netip.AddrFrom4(raw)
	}
	var raw [16]byte
	value.FillBytes(raw[:])
	return netip.AddrFrom16(raw)
}

// expectedCover is the whole contract in one value: IPv4 before IPv6, each
// family's spans in ascending order, each span carried by its minimal blocks.
func expectedCover(prefixes []netip.Prefix) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(prefixes))
	for _, width := range []int{32, 128} {
		for _, span := range mergedSpans(prefixes, width) {
			out = append(out, coveringPrefixes(span)...)
		}
	}
	return out
}

func prefixStrings(prefixes []netip.Prefix) []string {
	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, p.String())
	}
	return out
}

func equalPrefixes(a, b []netip.Prefix) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCollapseLosslessPublishesTheMinimalCoverOfExactlyTheInput(t *testing.T) {
	for _, test := range collapsePropertyCases() {
		t.Run(test.name, func(t *testing.T) {
			want := expectedCover(test.input)
			got := CollapseLosslessPrefixes(test.input)
			if !equalPrefixes(got, want) {
				t.Fatalf("collapsed %v\n got = %v\nwant = %v", prefixStrings(test.input), prefixStrings(got), prefixStrings(want))
			}
			// The published set must describe the same addresses when it is
			// collapsed again: a cover that grew or shrank would show here.
			if again := CollapseLosslessPrefixes(got); !equalPrefixes(again, want) {
				t.Fatalf("second collapse = %v, want %v", prefixStrings(again), prefixStrings(want))
			}
			for i := range got {
				for j := i + 1; j < len(got); j++ {
					if got[i].Overlaps(got[j]) {
						t.Fatalf("overlapping prefixes %v and %v", got[i], got[j])
					}
				}
			}
		})
	}
}

// Order is part of the answer, and the order of arrival is not. A source that
// listed the same networks in another sequence used to be a different plan.
func TestCollapseLosslessIsIndependentOfInputOrder(t *testing.T) {
	shuffle := rand.New(rand.NewPCG(0x5EED, 0x0111))
	for _, test := range collapsePropertyCases() {
		first := CollapseLosslessPrefixes(test.input)
		for attempt := 0; attempt < 4; attempt++ {
			permuted := append([]netip.Prefix(nil), test.input...)
			shuffle.Shuffle(len(permuted), func(i, j int) { permuted[i], permuted[j] = permuted[j], permuted[i] })
			if got := CollapseLosslessPrefixes(permuted); !equalPrefixes(got, first) {
				t.Fatalf("%s permuted = %v, want %v", test.name, prefixStrings(got), prefixStrings(first))
			}
		}
	}
}

// A value that names no network is dropped, never widened into one. The 4-in-6
// forms are the case that matters: the same address in two spellings, with a
// length that may describe no IPv4 network at all.
func TestCollapseLosslessDropsValuesThatNameNoNetwork(t *testing.T) {
	mapped := netip.PrefixFrom(netip.MustParseAddr("::ffff:192.0.2.0"), 120)
	got := CollapseLosslessPrefixes([]netip.Prefix{
		{},
		mapped,
		netip.PrefixFrom(netip.MustParseAddr("::ffff:198.51.100.0"), 24),
		netip.MustParsePrefix("203.0.113.0/24"),
	})
	want := []string{"198.51.100.0/24", "203.0.113.0/24"}
	if strings := prefixStrings(got); len(strings) != len(want) || strings[0] != want[0] || strings[1] != want[1] {
		t.Fatalf("collapsed = %v, want %v", prefixStrings(got), want)
	}
	if empty := CollapseLosslessPrefixes(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("empty collapse = %#v", empty)
	}
}

type collapseCase struct {
	name  string
	input []netip.Prefix
}

func collapsePropertyCases() []collapseCase {
	parse := func(values ...string) []netip.Prefix {
		out := make([]netip.Prefix, 0, len(values))
		for _, value := range values {
			out = append(out, netip.MustParsePrefix(value))
		}
		return out
	}
	cases := []collapseCase{
		{name: "empty", input: nil},
		{name: "siblings merge", input: parse("192.0.2.0/25", "192.0.2.128/25")},
		{name: "contained disappears", input: parse("192.0.2.0/24", "192.0.2.128/25", "192.0.2.200/32")},
		{name: "duplicates", input: parse("192.0.2.0/24", "192.0.2.0/24", "192.0.2.0/24")},
		{name: "cascade to the parent", input: parse("192.0.2.0/26", "192.0.2.64/26", "192.0.2.128/25")},
		{name: "adjacent but unaligned", input: parse("192.0.2.1/32", "192.0.2.2/32")},
		{name: "whole space", input: parse("0.0.0.0/1", "128.0.0.0/1")},
		{name: "both families", input: parse("192.0.2.0/25", "192.0.2.128/25", "2001:db8::/33", "2001:db8:8000::/33")},
		{name: "family boundary", input: parse("255.255.255.255/32", "::/128", "0.0.0.0/32")},
		{name: "v6 siblings", input: parse("2001:db8::/127", "2001:db8::1/128")},
		{name: "default routes", input: parse("0.0.0.0/0", "192.0.2.0/24", "::/0", "2001:db8::/32")},
	}
	random := rand.New(rand.NewPCG(0xC011A95E, 0x0FFEE))
	for i := 0; i < 200; i++ {
		count := 1 + random.IntN(24)
		input := make([]netip.Prefix, 0, count)
		for j := 0; j < count; j++ {
			input = append(input, randomPrefix(random))
		}
		cases = append(cases, collapseCase{name: "random", input: input})
	}
	return cases
}

// randomPrefix draws from a deliberately small address space so that nesting,
// sibling pairs and repeats are common rather than astronomically unlikely.
func randomPrefix(random *rand.Rand) netip.Prefix {
	if random.IntN(2) == 0 {
		bits := 20 + random.IntN(13)
		if random.IntN(16) == 0 {
			bits = random.IntN(9)
		}
		var raw [4]byte
		value := uint32(0xC0000200) + uint32(random.IntN(4096))
		raw[0], raw[1], raw[2], raw[3] = byte(value>>24), byte(value>>16), byte(value>>8), byte(value)
		return netip.PrefixFrom(netip.AddrFrom4(raw), bits).Masked()
	}
	bits := 112 + random.IntN(17)
	if random.IntN(16) == 0 {
		bits = random.IntN(33)
	}
	raw := [16]byte{0x20, 0x01, 0x0d, 0xb8}
	low := random.IntN(4096)
	raw[14], raw[15] = byte(low>>8), byte(low)
	return netip.PrefixFrom(netip.AddrFrom16(raw), bits).Masked()
}
