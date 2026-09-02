package planner

import (
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

func overlapTestRule(t testing.TB, kind domain.RuleKind, value, owner string) domain.RouteRule {
	t.Helper()
	var rule domain.RouteRule
	var err error
	switch {
	case kind.IsDomain():
		rule, err = domain.NewDomainRule(kind, value, owner, "web", domain.SourceManual, nil, nil)
	case kind.IsIP():
		rule, err = domain.NewAddrRule(netip.MustParseAddr(value), owner, "web", domain.SourceManual, nil, nil)
	default:
		rule, err = domain.NewPrefixRule(netip.MustParsePrefix(value), owner, "web", domain.SourceManual, nil, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func TestRuleOverlapsRespectTypedContainmentAndDistinctOwners(t *testing.T) {
	for _, test := range []struct {
		name                string
		leftKind, rightKind domain.RuleKind
		left, right, owner  string
		want                string
	}{
		{"duplicate domains", domain.RuleDomainSuffix, domain.RuleDomainSuffix, "example.com", "example.com", "beta", "duplicate"},
		{"same list is not duplicate", domain.RuleIPv4, domain.RuleIPv4, "192.0.2.1", "192.0.2.1", "alpha", ""},
		{"equal networks", domain.RulePrefix6, domain.RulePrefix6, "2001:db8::/48", "2001:db8::/48", "beta", "duplicate"},
		{"network covers address", domain.RulePrefix4, domain.RuleIPv4, "192.0.2.0/24", "192.0.2.1", "beta", "covered"},
		{"host prefix covers address", domain.RulePrefix4, domain.RuleIPv4, "192.0.2.1/32", "192.0.2.1", "beta", "covered"},
		{"IPv6 nesting", domain.RulePrefix6, domain.RulePrefix6, "2001:db8::/32", "2001:db8:1::/48", "beta", "covered"},
		{"IPv6 address", domain.RulePrefix6, domain.RuleIPv6, "2001:db8::/32", "2001:db8::1", "beta", "covered"},
		{"different networks", domain.RulePrefix4, domain.RuleIPv4, "192.0.2.0/25", "192.0.2.128", "beta", ""},
		{"different families", domain.RulePrefix4, domain.RuleIPv6, "192.0.2.0/24", "2001:db8::1", "beta", ""},
		{"own containment", domain.RulePrefix4, domain.RuleIPv4, "192.0.2.0/24", "192.0.2.1", "alpha", ""},
		{"suffix contains exact host", domain.RuleDomainSuffix, domain.RuleDomainExact, "example.com", "example.com", "beta", "covered"},
		{"suffix contains child", domain.RuleDomainSuffix, domain.RuleDomainSuffix, "example.com", "api.example.com", "beta", "covered"},
		{"exact is not suffix", domain.RuleDomainExact, domain.RuleDomainExact, "example.com", "api.example.com", "beta", ""},
		{"label boundary", domain.RuleDomainSuffix, domain.RuleDomainExact, "example.com", "notexample.com", "beta", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			rules := []domain.RouteRule{overlapTestRule(t, test.leftKind, test.left, "alpha"), overlapTestRule(t, test.rightKind, test.right, test.owner)}
			before := slices.Clone(rules)
			got := AnalyzeRuleOverlaps(rules, 100)
			if !reflect.DeepEqual(rules, before) {
				t.Fatal("analysis mutated its input")
			}
			if got.Truncated {
				t.Fatal("two-rule result truncated")
			}
			if test.want == "" {
				if len(got.Items) != 0 {
					t.Fatalf("false relation: %#v", got)
				}
				return
			}
			if len(got.Items) != 1 || got.Items[0].Kind != test.want {
				t.Fatalf("got %#v", got)
			}
			item := got.Items[0]
			if test.want == "duplicate" {
				if !slices.Equal(item.Entry.Services, []string{"alpha", "beta"}) || item.Entry.Value != test.left || item.Covering != nil {
					t.Fatalf("duplicate=%#v", item)
				}
			} else if item.Covering == nil || item.Entry.Value != test.right || item.Covering.Value != test.left || !slices.Equal(item.Entry.Services, []string{"beta"}) || !slices.Equal(item.Covering.Services, []string{"alpha"}) {
				t.Fatalf("coverage=%#v", item)
			}
			slices.Reverse(rules)
			if !reflect.DeepEqual(got, AnalyzeRuleOverlaps(rules, 100)) {
				t.Fatal("order changed analysis")
			}
		})
	}
}

func TestRuleOverlapsDeduplicateComponentsAndBoundDetails(t *testing.T) {
	rules := []domain.RouteRule{}
	for i := range 3 {
		for _, owner := range []string{"alpha", "beta", "gamma"} {
			r := overlapTestRule(t, domain.RuleDomainSuffix, fmt.Sprintf("host%d.example", i), owner)
			rules = append(rules, r)
			r.ComponentID = "other"
			rules = append(rules, r)
		}
	}
	all := AnalyzeRuleOverlaps(rules, 3)
	if all.Truncated || len(all.Items) != 3 || len(all.Items[0].Entry.Services) != 3 {
		t.Fatalf("all=%#v", all)
	}
	limited := AnalyzeRuleOverlaps(rules, 2)
	if !limited.Truncated || !reflect.DeepEqual(limited.Items, all.Items[:2]) {
		t.Fatalf("limited=%#v", limited)
	}
	zero := AnalyzeRuleOverlaps(rules, 0)
	if !zero.Truncated || len(zero.Items) != 0 {
		t.Fatalf("zero=%#v", zero)
	}
	if got := AnalyzeRuleOverlaps(nil, 0); got.Truncated || len(got.Items) != 0 {
		t.Fatalf("empty=%#v", got)
	}
}

func TestRuleOverlapsNeverListsTheCoveredOwnerAsItsOwnCover(t *testing.T) {
	rules := []domain.RouteRule{
		overlapTestRule(t, domain.RuleDomainSuffix, "example.com", "alpha"),
		overlapTestRule(t, domain.RuleDomainSuffix, "example.com", "beta"),
		overlapTestRule(t, domain.RuleDomainExact, "api.example.com", "alpha"),
	}
	got := AnalyzeRuleOverlaps(rules, 100)
	if len(got.Items) != 2 {
		t.Fatalf("got=%#v", got)
	}
	coverage := got.Items[1]
	if coverage.Covering == nil || !slices.Equal(coverage.Entry.Services, []string{"alpha"}) || !slices.Equal(coverage.Covering.Services, []string{"beta"}) {
		t.Fatalf("self cover=%#v", coverage)
	}
}

// An independent pairwise oracle exercises the indexed prefix search, including
// address-family boundaries; no input ordering or analysis mutates the plan.
func FuzzRuleOverlapPrefixContainment(f *testing.F) {
	f.Add(uint64(1), uint64(255), uint8(24), uint8(28), false)
	f.Add(uint64(0), uint64(1), uint8(32), uint8(64), true)
	f.Fuzz(func(t *testing.T, first, second uint64, leftBits, rightBits uint8, ipv6 bool) {
		addr := func(n uint64) netip.Addr {
			if !ipv6 {
				return netip.AddrFrom4([4]byte{192, 0, byte(n >> 8), byte(n)})
			}
			return netip.AddrFrom16([16]byte{0x20, 1, 0xd, 0xb8, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)})
		}
		a, b := addr(first), addr(second)
		p := netip.PrefixFrom(a, int(leftBits)%(a.BitLen()+1)).Masked()
		q := netip.PrefixFrom(b, int(rightBits)%(b.BitLen()+1)).Masked()
		left, _ := domain.NewPrefixRule(p, "alpha", "web", domain.SourceManual, nil, nil)
		right, _ := domain.NewPrefixRule(q, "beta", "web", domain.SourceManual, nil, nil)
		got := AnalyzeRuleOverlaps([]domain.RouteRule{left, right}, 10)
		want := p == q || (p.Bits() < q.Bits() && p.Contains(q.Addr())) || (q.Bits() < p.Bits() && q.Contains(p.Addr()))
		if (len(got.Items) == 1) != want || len(got.Items) > 1 || got.Truncated {
			t.Fatalf("%s %s: %#v", p, q, got)
		}
		if p == q && got.Items[0].Kind != "duplicate" {
			t.Fatal("equal prefix is not an exact duplicate")
		}
		if !reflect.DeepEqual(got, AnalyzeRuleOverlaps([]domain.RouteRule{right, left}, 10)) {
			t.Fatal("unstable order")
		}
	})
}
