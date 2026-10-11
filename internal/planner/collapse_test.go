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

// Two networks that touch nothing were not merged, even though they share a
// group: the reason code belongs to a prefix that stands for several inputs.
func TestDisjointFeedNetworksCarryNoCollapseReason(t *testing.T) {
	rules := collapseObserved([]domain.RouteRule{
		prefixRule(t, "198.51.100.0/24", domain.SourceCommunity),
		prefixRule(t, "203.0.113.0/24", domain.SourceCommunity),
	})
	if values(rules) != "198.51.100.0/24,203.0.113.0/24" {
		t.Fatalf("collapsed = %v", values(rules))
	}
	for _, rule := range rules {
		if containsReason(rule.ReasonCodes, ReasonLosslessCollapsed) {
			t.Fatalf("an unmerged network claims a collapse: %#v", rule)
		}
	}
}

// When two source classes arrive at one destination, the class the plan keeps
// is a fixed precedence, never the order the rules happened to be read in.
func TestDedupeKeepsTheStrongestClassWhateverTheOrder(t *testing.T) {
	manual := prefixRule(t, "198.51.100.0/24", domain.SourceManual)
	official := prefixRule(t, "198.51.100.0/24", domain.SourceOfficial)
	observed := prefixRule(t, "198.51.100.0/24", domain.SourceObserved)
	for _, order := range [][]domain.RouteRule{
		{manual, official, observed},
		{observed, official, manual},
		{official, observed, manual},
	} {
		rules := dedupeRules(order)
		if len(rules) != 1 || rules[0].SourceClass != domain.SourceManual {
			t.Fatalf("dedupe of %v kept %#v", order, rules)
		}
	}
	rules := dedupeRules([]domain.RouteRule{observed, official})
	if len(rules) != 1 || rules[0].SourceClass != domain.SourceOfficial {
		t.Fatalf("dedupe kept %#v", rules)
	}
}

// Collapsing runs per source class, so two classes can each produce the same
// destination. Which class survives used to follow map iteration order, and the
// class is part of the semantic hash: one input set published two artifacts.
// Each case is planned many times so an order dependence cannot hide.
func TestCrossClassCollisionsPlanToOneHash(t *testing.T) {
	cutoff := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	sighting := func(resource domain.Resource, class domain.SourceClass, id string) domain.Sighting {
		return domain.Sighting{
			ListID: "example", ComponentID: "web", Resource: resource, SourceID: id, SourceClass: class,
			SourceRevision: "r", FirstSeen: cutoff, LastSeen: cutoff, ValidUntil: cutoff.Add(time.Hour),
			ObservationCount: 1, Validity: domain.ValidityValid,
		}
	}
	address, err := domain.NewAddrResourceFromString("198.51.100.7")
	if err != nil {
		t.Fatal(err)
	}
	target := domain.TargetDefinition{
		ID: "keenetic", FormatKey: "p", RendererID: "r",
		Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: 1000, MaxArtifactSize: 1 << 20},
	}
	cases := []struct {
		name      string
		seeds     []domain.Seed
		sightings []domain.Sighting
		value     string
		class     domain.SourceClass
	}{
		{
			name:  "manual network and two official halves",
			seeds: []domain.Seed{{Kind: domain.RulePrefix4, Value: "198.51.100.0/24", ComponentID: "web", SourceID: "manual:net", SourceClass: domain.SourceManual}},
			sightings: []domain.Sighting{
				sighting(mustPrefixResource(t, "198.51.100.0/25"), domain.SourceOfficial, "vendor"),
				sighting(mustPrefixResource(t, "198.51.100.128/25"), domain.SourceOfficial, "vendor"),
			},
			value: "198.51.100.0/24", class: domain.SourceManual,
		},
		{
			name: "official host network and an observed address",
			sightings: []domain.Sighting{
				sighting(mustPrefixResource(t, "198.51.100.7/32"), domain.SourceOfficial, "vendor"),
				sighting(address, domain.SourceObserved, "dns"),
			},
			value: "198.51.100.7", class: domain.SourceOfficial,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			definition := domain.ListDefinition{
				ID: "example", CatalogRevision: "c",
				Components: []domain.ComponentDefinition{{ID: "web", Required: true}},
				Seeds:      tc.seeds,
			}
			var firstHash string
			for run := 0; run < 64; run++ {
				plan, err := BuildPlan(definition, tc.sightings, target, cutoff, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(plan.Rules) != 1 || plan.Rules[0].CanonicalValue() != tc.value || plan.Rules[0].SourceClass != tc.class {
					t.Fatalf("run %d rules = %#v", run, plan.Rules)
				}
				if run == 0 {
					firstHash = plan.SemanticHash
				} else if plan.SemanticHash != firstHash {
					t.Fatalf("run %d hash %s differs from %s", run, plan.SemanticHash, firstHash)
				}
			}
		})
	}
}
