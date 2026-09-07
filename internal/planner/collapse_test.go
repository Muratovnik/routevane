package planner

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

func prefixRule(t *testing.T, value string, class domain.SourceClass) domain.RouteRule {
	t.Helper()
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := domain.NewPrefixRule(prefix, "example", "web", class, []string{ReasonOfficialRule}, []string{"feed"})
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func values(rules []domain.RouteRule) string {
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		switch {
		case rule.Kind.IsPrefix():
			out = append(out, rule.Prefix.String())
		case rule.Kind.IsDomain():
			out = append(out, rule.Domain)
		default:
			out = append(out, rule.Addr.String())
		}
	}
	return strings.Join(out, ",")
}

// A feed publishes networks. Leaving them uncollapsed spent the target's rule
// budget on values that merge exactly.
func TestCollapseMergesAdjacentFeedPrefixes(t *testing.T) {
	rules := collapseObserved([]domain.RouteRule{
		prefixRule(t, "192.0.2.0/25", domain.SourceCommunity),
		prefixRule(t, "192.0.2.128/25", domain.SourceCommunity),
	})
	if len(rules) != 1 || values(rules) != "192.0.2.0/24" {
		t.Fatalf("collapsed = %v", values(rules))
	}
	if !containsReason(rules[0].ReasonCodes, ReasonLosslessCollapsed) {
		t.Fatalf("reasons = %v", rules[0].ReasonCodes)
	}
}

// A network wholly inside another is not a second rule.
func TestCollapseDropsAContainedPrefix(t *testing.T) {
	rules := collapseObserved([]domain.RouteRule{
		prefixRule(t, "192.0.2.0/24", domain.SourceOfficial),
		prefixRule(t, "192.0.2.128/25", domain.SourceOfficial),
	})
	if len(rules) != 1 || values(rules) != "192.0.2.0/24" {
		t.Fatalf("collapsed = %v", values(rules))
	}
}

// The class is part of what a rule is. Merging a community list into a vendor's
// own publication would make the diagnostics claim the vendor said it.
func TestCollapseKeepsSourceClassesApart(t *testing.T) {
	rules := collapseObserved([]domain.RouteRule{
		prefixRule(t, "192.0.2.0/25", domain.SourceCommunity),
		prefixRule(t, "192.0.2.128/25", domain.SourceOfficial),
	})
	if len(rules) != 2 {
		t.Fatalf("collapsed across classes: %v", values(rules))
	}
}

// A prefix that arrived as a network and merged with nothing is not a collapse,
// and claiming otherwise would put a reason code on an untouched rule.
func TestAnUnmergedNetworkCarriesNoCollapseReason(t *testing.T) {
	rules := collapseObserved([]domain.RouteRule{prefixRule(t, "198.51.100.0/24", domain.SourceCommunity)})
	if len(rules) != 1 {
		t.Fatalf("collapsed = %v", values(rules))
	}
	if containsReason(rules[0].ReasonCodes, ReasonLosslessCollapsed) {
		t.Fatalf("reasons = %v", rules[0].ReasonCodes)
	}
}

// Expiry survives a merge as the latest of the merged rules: dropping it would
// make a collapsed route outlive every observation it stands for.
func TestCollapseKeepsTheLatestExpiry(t *testing.T) {
	early := prefixRule(t, "192.0.2.0/25", domain.SourceCommunity)
	late := prefixRule(t, "192.0.2.128/25", domain.SourceCommunity)
	earlyAt := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	lateAt := earlyAt.Add(48 * time.Hour)
	early.ExpiresAt = &earlyAt
	late.ExpiresAt = &lateAt
	rules := collapseObserved([]domain.RouteRule{early, late})
	if len(rules) != 1 || rules[0].ExpiresAt == nil || !rules[0].ExpiresAt.Equal(lateAt) {
		t.Fatalf("collapsed = %#v", rules)
	}
}

// A domain rule is not an address and must pass through untouched.
func TestCollapseLeavesDomainRulesAlone(t *testing.T) {
	rule, err := domain.NewDomainRule(domain.RuleDomainSuffix, "example.com", "example", "web", domain.SourceCommunity, []string{ReasonOfficialRule}, []string{"feed"})
	if err != nil {
		t.Fatal(err)
	}
	rules := collapseObserved([]domain.RouteRule{rule})
	if len(rules) != 1 || rules[0].Kind != domain.RuleDomainSuffix || rules[0].Domain != "example.com" {
		t.Fatalf("collapsed = %#v", rules)
	}
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

// A curated third-party prefix list is a declaration, not an inference. Refusing
// it would have made declaring the class honestly the reason the material could
// not be used (ADR 0015).
func TestACommunityPrefixListIsRoutable(t *testing.T) {
	sighting := domain.Sighting{
		ListID: "example", ComponentID: "web",
		Resource:    mustPrefixResource(t, "198.51.100.0/24"),
		SourceID:    "iplist",
		SourceClass: domain.SourceCommunity,
	}
	if !declaredNetwork(sighting) {
		t.Fatal("a community prefix list must be routable")
	}
	sighting.SourceClass = domain.SourceObserved
	if declaredNetwork(sighting) {
		t.Fatal("an observed prefix is an inference and stays quarantined")
	}
	sighting.SourceClass = domain.SourceCommunity
	sighting.SharedNetworkEvidence = domain.SharedNetworkEvidenceTrusted
	if declaredNetwork(sighting) {
		t.Fatal("a shared CDN range is not list specific")
	}
}

func mustPrefixResource(t *testing.T, value string) domain.Resource {
	t.Helper()
	resource, err := domain.NewPrefixResourceFromString(value)
	if err != nil {
		t.Fatal(err)
	}
	return resource
}

// A vendor's own range is not derived from the names beside it and may serve
// endpoints no name reaches, so a name-routing target keeps it. A third party's
// aggregation of those same names is redundant and goes.
func TestDomainSufficiencyKeepsAVendorRangeAndDropsAnAggregatedOne(t *testing.T) {
	definition := domain.ListDefinition{
		ID:         "example",
		Components: []domain.ComponentDefinition{{ID: "web", Required: true}},
		Seeds: []domain.Seed{{
			Kind: domain.RuleDomainSuffix, Value: "example.com", ComponentID: "web",
			SourceID: "manual:domain", SourceClass: domain.SourceManual,
		}},
	}
	cutoff := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	sighting := func(value string, class domain.SourceClass, id string) domain.Sighting {
		return domain.Sighting{
			ListID: "example", ComponentID: "web",
			Resource: mustPrefixResource(t, value), SourceID: id, SourceClass: class,
			FirstSeen: cutoff, LastSeen: cutoff, ValidUntil: cutoff.Add(time.Hour),
			ObservationCount: 1, Validity: domain.ValidityValid,
		}
	}
	target := domain.TargetDefinition{
		ID: "singbox", FormatKey: "p", RendererID: "r",
		Constraints: domain.TargetConstraints{
			SupportsDomainSuffix: true, SupportsIPv4: true, SupportsPrefixes: true,
			MaxRules: 1000, MaxArtifactSize: 1 << 20,
		},
	}
	plan, err := BuildPlan(definition, []domain.Sighting{
		sighting("198.51.100.0/24", domain.SourceOfficial, "vendor"),
		sighting("203.0.113.0/24", domain.SourceCommunity, "iplist"),
	}, target, cutoff, nil)
	if err != nil {
		t.Fatal(err)
	}
	if values(plan.Rules) != "example.com,198.51.100.0/24" {
		t.Fatalf("rules = %v", values(plan.Rules))
	}
}
