package planner

import (
	"bytes"
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/renderers/rawjson"
)

func TestBuildPlanDomainTargetExcludesFreshIPs(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 123, time.UTC)
	addresses := []string{"192.0.2.2", "192.0.2.1", "::ffff:192.0.2.1"}
	sightings := make([]domain.Sighting, 0, len(addresses))
	for _, value := range addresses {
		resource, err := domain.NewAddrResourceFromString(value)
		if err != nil {
			t.Fatal(err)
		}
		sightings = append(sightings, domain.Sighting{ListID: "example", ComponentID: "web", Resource: resource, SourceID: "dns:example", SourceClass: domain.SourceObserved, SourceRevision: "test", FirstSeen: now, LastSeen: now, ValidUntil: now.Add(2 * time.Hour), Validity: domain.ValidityValid, ObservationCount: 1})
	}
	plan, err := BuildPlan(domain.ExampleListDefinition(), sightings, domain.RawJSONTargetDefinition(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rules) != 1 || plan.Rules[0].Kind != domain.RuleDomainSuffix || plan.Rules[0].CanonicalValue() != "example.com" {
		t.Fatalf("rules = %#v", plan.Rules)
	}
	if len(plan.Excluded) != 2 {
		t.Fatalf("excluded = %d, want two unique IP candidates", len(plan.Excluded))
	}
	for _, excluded := range plan.Excluded {
		if !contains(excluded.ReasonCodes, ReasonNotRequiredForDomainCapableTarget) {
			t.Fatalf("excluded reasons = %#v", excluded.ReasonCodes)
		}
	}
}

func TestBuildPlanDomainSufficiencyIsComponentScoped(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	webIP, _ := domain.NewAddrResourceFromString("192.0.2.1")
	authIP, _ := domain.NewAddrResourceFromString("192.0.2.2")
	definition := domain.ListDefinition{
		ID:         "multi",
		Components: []domain.ComponentDefinition{{ID: "web", Required: true}, {ID: "auth", Required: true}},
		Seeds:      []domain.Seed{{Kind: domain.RuleDomainSuffix, Value: "example.com", ComponentID: "web", SourceID: "manual:web", SourceClass: domain.SourceManual}},
	}
	sightings := []domain.Sighting{
		{ListID: "multi", ComponentID: "web", Resource: webIP, SourceID: "dns", SourceClass: domain.SourceObserved, ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid},
		{ListID: "multi", ComponentID: "auth", Resource: authIP, SourceID: "dns", SourceClass: domain.SourceObserved, ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid},
	}
	plan, err := BuildPlan(definition, sightings, domain.RawJSONTargetDefinition(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rules) != 2 {
		t.Fatalf("rules = %#v", plan.Rules)
	}
	if len(plan.Excluded) != 1 || plan.Excluded[0].Candidate.ComponentID != "web" || !contains(plan.Excluded[0].ReasonCodes, ReasonNotRequiredForDomainCapableTarget) {
		t.Fatalf("excluded = %#v", plan.Excluded)
	}
	if plan.Rules[1].ComponentID != "auth" || plan.Rules[1].CanonicalValue() != "192.0.2.2" {
		t.Fatalf("auth rule = %#v", plan.Rules)
	}
}

func TestBuildPlanRejectsWhitespaceSeedAsInvalidResource(t *testing.T) {
	definition := domain.ListDefinition{ID: "example", Components: []domain.ComponentDefinition{{ID: "web", Required: true}}, Seeds: []domain.Seed{{Kind: domain.RuleDomainSuffix, Value: " example.com", ComponentID: "web"}, {Kind: domain.RuleDomainSuffix, Value: "example.com ", ComponentID: "web"}}}
	plan, err := BuildPlan(definition, nil, domain.RawJSONTargetDefinition(), time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rules) != 0 || len(plan.Excluded) != 2 {
		t.Fatalf("plan = %#v", plan)
	}
	for _, excluded := range plan.Excluded {
		if !contains(excluded.ReasonCodes, ReasonInvalidResource) {
			t.Fatalf("excluded = %#v", plan.Excluded)
		}
	}
}

func TestBuildPlanIPOnlyFreshAndStale(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	resource, _ := domain.NewAddrResourceFromString("192.0.2.1")
	fresh := domain.Sighting{ListID: "example", ComponentID: "web", Resource: resource, SourceID: "dns", SourceClass: domain.SourceObserved, ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid, ObservationCount: 1}
	stale := fresh
	stale.Resource, _ = domain.NewAddrResourceFromString("192.0.2.2")
	stale.ValidUntil = now
	target := domain.TargetDefinition{ID: "ip-only", FormatKey: "ip-only", Constraints: domain.TargetConstraints{SupportsIPv4: true}}
	plan, err := BuildPlan(domain.ListDefinition{ID: "example", Components: []domain.ComponentDefinition{{ID: "web", Required: true}}}, []domain.Sighting{stale, fresh}, target, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rules) != 1 || plan.Rules[0].CanonicalValue() != "192.0.2.1" {
		t.Fatalf("rules = %#v", plan.Rules)
	}
	if len(plan.Excluded) != 1 {
		t.Fatalf("excluded = %#v", plan.Excluded)
	}
	for _, excluded := range plan.Excluded {
		if !contains(excluded.ReasonCodes, ReasonStaleObservation) {
			t.Fatalf("excluded = %#v", plan.Excluded)
		}
	}
}

func TestBuildPlanQuarantinesObservedPrefixExpansion(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	prefix, _ := domain.NewPrefixResourceFromString("104.16.0.0/12")
	plan, err := BuildPlan(domain.ListDefinition{ID: "example", Components: []domain.ComponentDefinition{{ID: "web", Required: true}}}, []domain.Sighting{{ListID: "example", ComponentID: "web", Resource: prefix, SourceID: "rdap", SourceClass: domain.SourceMetadata, ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid}}, domain.TargetDefinition{ID: "ip-only", FormatKey: "ip-only", Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rules) != 0 || len(plan.Excluded) != 1 || plan.Excluded[0].Outcome != "quarantined" {
		t.Fatalf("plan = %#v", plan)
	}
	if !contains(plan.Excluded[0].ReasonCodes, ReasonWideNetworkExpansion) || contains(plan.Excluded[0].ReasonCodes, ReasonSharedCDNorCloud) {
		t.Fatalf("reasons = %#v", plan.Excluded[0].ReasonCodes)
	}
}

func TestBuildPlanAddsSharedCDNReasonOnlyForTrustedEvidence(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	prefix, _ := domain.NewPrefixResourceFromString("104.16.0.0/12")
	sighting := domain.Sighting{ListID: "example", ComponentID: "web", Resource: prefix, SourceID: "untrusted-rdap", SourceClass: domain.SourceMetadata, SharedNetworkEvidence: domain.SharedNetworkEvidenceTrusted, ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid}
	plan, err := BuildPlan(domain.ListDefinition{ID: "example", Components: []domain.ComponentDefinition{{ID: "web", Required: true}}}, []domain.Sighting{sighting}, domain.TargetDefinition{ID: "ip-only", FormatKey: "ip-only", Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Excluded) != 1 || !contains(plan.Excluded[0].ReasonCodes, ReasonWideNetworkExpansion) || !contains(plan.Excluded[0].ReasonCodes, ReasonSharedCDNorCloud) {
		t.Fatalf("reasons = %#v", plan.Excluded)
	}
}

func TestCollapseLosslessExactSet(t *testing.T) {
	values := []netip.Addr{netip.MustParseAddr("192.0.2.0"), netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2"), netip.MustParseAddr("192.0.2.3"), netip.MustParseAddr("2001:db8::1")}
	got := CollapseLossless(values)
	want := []string{"192.0.2.0/30", "2001:db8::1/128"}
	if len(got) != len(want) {
		t.Fatalf("prefixes = %v, want %v", got, want)
	}
	for i, prefix := range got {
		if prefix.String() != want[i] {
			t.Fatalf("prefixes = %v, want %v", got, want)
		}
	}
	for _, value := range values {
		found := false
		for _, prefix := range got {
			if prefix.Contains(value) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("collapsed set lost %v", value)
		}
	}
}

func TestCollapseLosslessAlignedAndNonAlignedFamilies(t *testing.T) {
	tests := []struct {
		name   string
		values []netip.Addr
		want   []string
	}{
		{name: "aligned v4", values: []netip.Addr{netip.MustParseAddr("192.0.2.0"), netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2"), netip.MustParseAddr("192.0.2.3")}, want: []string{"192.0.2.0/30"}},
		{name: "nonaligned v4", values: []netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")}, want: []string{"192.0.2.1/32", "192.0.2.2/32"}},
		{name: "aligned v6", values: []netip.Addr{netip.MustParseAddr("2001:db8::"), netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("2001:db8::2"), netip.MustParseAddr("2001:db8::3")}, want: []string{"2001:db8::/126"}},
		{name: "nonaligned v6", values: []netip.Addr{netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("2001:db8::2")}, want: []string{"2001:db8::1/128", "2001:db8::2/128"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CollapseLossless(test.values)
			if len(got) != len(test.want) {
				t.Fatalf("prefixes = %v, want %v", got, test.want)
			}
			for i, prefix := range got {
				if prefix.String() != test.want[i] {
					t.Fatalf("prefixes = %v, want %v", got, test.want)
				}
			}
			assertCollapsedExact(t, test.values)
		})
	}
}

func FuzzCollapseLosslessExactSet(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	f.Add([]byte{1, 2, 127, 128, 254, 255})
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 32 {
			input = input[:32]
		}
		values := make([]netip.Addr, 0, len(input))
		for i, value := range input {
			if i%2 == 0 {
				values = append(values, netip.AddrFrom4([4]byte{192, 0, 2, value}))
			} else {
				bytes := [16]byte{0x20, 0x01, 0x0d, 0xb8}
				bytes[15] = value
				values = append(values, netip.AddrFrom16(bytes))
			}
		}
		if len(values) > 0 {
			assertCollapsedExact(t, values)
		}
	})
}

func assertCollapsedExact(t *testing.T, values []netip.Addr) {
	t.Helper()
	unique := make(map[string]netip.Addr)
	for _, value := range values {
		unique[value.Unmap().String()] = value.Unmap()
	}
	prefixes := CollapseLossless(values)
	for i, prefix := range prefixes {
		for j := i + 1; j < len(prefixes); j++ {
			other := prefixes[j]
			if prefix.Addr().Is4() != other.Addr().Is4() {
				continue
			}
			if prefix.Contains(other.Addr()) || other.Contains(prefix.Addr()) {
				t.Fatalf("overlapping prefixes %v and %v", prefix, other)
			}
		}
		width := prefix.Addr().BitLen() - prefix.Bits()
		if width > 12 {
			t.Fatalf("test set would require unbounded enumeration for %v", prefix)
		}
		current := prefix.Addr()
		for {
			if !prefix.Contains(current) {
				break
			}
			value, ok := unique[current.String()]
			if !ok || value.Is4() != prefix.Addr().Is4() {
				t.Fatalf("prefix %v adds address %v", prefix, current)
			}
			if next := current.Next(); !next.IsValid() || !prefix.Contains(next) {
				break
			} else {
				current = next
			}
		}
	}
	for _, value := range unique {
		count := 0
		for _, prefix := range prefixes {
			if prefix.Contains(value) {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("address %v covered %d times by %v", value, count, prefixes)
		}
	}
}

func TestBuildPlanAndRawJSONStableAcrossOrder(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 123, time.UTC)
	resource1, _ := domain.NewAddrResourceFromString("192.0.2.1")
	resource2, _ := domain.NewAddrResourceFromString("192.0.2.2")
	base := []domain.Sighting{
		{ListID: "example", ComponentID: "web", Resource: resource1, SourceID: "dns", SourceClass: domain.SourceObserved, SourceRevision: "v1", FirstSeen: now, LastSeen: now, ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid, ObservationCount: 1},
		{ListID: "example", ComponentID: "web", Resource: resource2, SourceID: "dns", SourceClass: domain.SourceObserved, SourceRevision: "v1", FirstSeen: now, LastSeen: now, ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid, ObservationCount: 1},
	}
	target := domain.TargetDefinition{ID: "ip-only", FormatKey: "ip-only", Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true}}
	first, err := BuildPlan(domain.ListDefinition{ID: "example", CatalogRevision: "catalog", Components: []domain.ComponentDefinition{{ID: "web", Required: true}}}, base, target, now)
	if err != nil {
		t.Fatal(err)
	}
	reordered := []domain.Sighting{base[1], base[0], base[0]}
	second, err := BuildPlan(domain.ListDefinition{ID: "example", CatalogRevision: "catalog", Components: []domain.ComponentDefinition{{ID: "web", Required: true}}}, reordered, target, now)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := rawjson.Render(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := rawjson.Render(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) || first.SemanticHash != second.SemanticHash {
		t.Fatalf("order changed plan/hash:\n%s\n%s\n%q != %q", firstJSON, secondJSON, first.SemanticHash, second.SemanticHash)
	}
}

func TestPrefixSupportRequiresMatchingAddressFamily(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		kind   domain.RuleKind
		value  string
		target domain.TargetConstraints
	}{
		{"prefix6 on ipv4 target", domain.RulePrefix6, "2001:db8::/48", domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true}},
		{"prefix4 on ipv6 target", domain.RulePrefix4, "192.0.2.0/24", domain.TargetConstraints{SupportsIPv6: true, SupportsPrefixes: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := domain.ListDefinition{ID: "example", Components: []domain.ComponentDefinition{{ID: "web", Required: true}}, Seeds: []domain.Seed{{Kind: test.kind, Value: test.value, ComponentID: "web", SourceID: "manual:prefix", SourceClass: domain.SourceManual}}}
			plan, err := BuildPlan(definition, nil, domain.TargetDefinition{ID: "target", FormatKey: "target-v1", Constraints: test.target}, now)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Rules) != 0 || len(plan.Excluded) != 1 || !contains(plan.Excluded[0].ReasonCodes, ReasonUnsupportedByTarget) {
				t.Fatalf("family-mismatched prefix accepted: %#v", plan)
			}
		})
	}
}

func TestBuildPlanSetIsCanonicalAndAppliesOneGlobalLimitWithoutTruncation(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	definition := func(id string, values ...string) domain.ListDefinition {
		result := domain.ListDefinition{ID: id, CatalogRevision: "catalog", Components: []domain.ComponentDefinition{{ID: "web", Required: true}}}
		for _, value := range values {
			result.Seeds = append(result.Seeds, domain.Seed{Kind: domain.RuleIPv4, Value: value, ComponentID: "web", SourceID: "manual:" + value, SourceClass: domain.SourceManual})
		}
		return result
	}
	inputs := []ListInput{{Definition: definition("beta", "192.0.2.3", "192.0.2.4")}, {Definition: definition("alpha", "192.0.2.1", "192.0.2.2")}}
	target := domain.TargetDefinition{ID: "keenetic", FormatKey: "keenetic-bat-ipv4-v1", RendererID: "keenetic-route-bat", Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: 3, MaxArtifactSize: 131072}}
	plan, err := BuildPlanSet(inputs, target, now)
	if !errors.Is(err, ErrRuleLimitExceeded) {
		t.Fatalf("global limit error=%v", err)
	}
	if len(plan.Rules) != 4 {
		t.Fatalf("global limit truncated diagnostic plan: %#v", plan.Rules)
	}
	if !reflect.DeepEqual(plan.Lists, []string{"alpha", "beta"}) || !contains(plan.Warnings, "partial_coverage:rule_limit") {
		t.Fatalf("noncanonical limited plan: %#v", plan)
	}
	unlimited := target
	unlimited.Constraints.MaxRules = 10
	first, err := BuildPlanSet(inputs, unlimited, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPlanSet([]ListInput{inputs[1], inputs[0]}, unlimited, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.SemanticHash != second.SemanticHash || !reflect.DeepEqual(first.Rules, second.Rules) {
		t.Fatalf("list permutation changed plan/hash: %#v %#v", first, second)
	}
}

func TestBuildPlanSetFailsRequiredUncoveredButAllowsExplicitPartialCoverage(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	target := domain.TargetDefinition{ID: "keenetic", FormatKey: "keenetic-bat-ipv4-v1", RendererID: "keenetic-route-bat", Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: 1024, MaxArtifactSize: 131072}}
	definition := domain.ListDefinition{ID: "example", CatalogRevision: "catalog", Components: []domain.ComponentDefinition{{ID: "web", Required: true}}, Seeds: []domain.Seed{{Kind: domain.RuleDomainSuffix, Value: "example.com", ComponentID: "web", SourceID: "manual:domain", SourceClass: domain.SourceManual}}}
	if _, err := BuildPlanSet([]ListInput{{Definition: definition}}, target, now); !errors.Is(err, ErrRequiredCoverage) {
		t.Fatalf("uncovered required component error=%v", err)
	}
	freshResource, _ := domain.NewAddrResourceFromString("192.0.2.1")
	fresh := domain.Sighting{ListID: "example", ComponentID: "web", Resource: freshResource, SourceID: "dns", SourceClass: domain.SourceObserved, SourceRevision: "v1", ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid}
	plan, err := BuildPlanSet([]ListInput{{Definition: definition, Sightings: []domain.Sighting{fresh}}}, target, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rules) != 1 || plan.Rules[0].CanonicalValue() != "192.0.2.1" || !contains(plan.Warnings, "partial_coverage:unsupported_or_quarantined:example") {
		t.Fatalf("explicit partial coverage diagnostics missing: %#v", plan)
	}
}

func TestBuildPlanSetExcludesStaleAndMetadataBroadPrefixFromInstallableRules(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	definition := domain.ListDefinition{ID: "example", CatalogRevision: "catalog", Components: []domain.ComponentDefinition{{ID: "web", Required: true}}}
	freshResource, _ := domain.NewAddrResourceFromString("192.0.2.1")
	staleResource, _ := domain.NewAddrResourceFromString("192.0.2.2")
	broadResource, _ := domain.NewPrefixResourceFromString("104.16.0.0/12")
	sightings := []domain.Sighting{
		{ListID: "example", ComponentID: "web", Resource: freshResource, SourceID: "dns", SourceClass: domain.SourceObserved, SourceRevision: "v1", ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid},
		{ListID: "example", ComponentID: "web", Resource: staleResource, SourceID: "dns", SourceClass: domain.SourceObserved, SourceRevision: "v1", ValidUntil: now, Validity: domain.ValidityStale},
		{ListID: "example", ComponentID: "web", Resource: broadResource, SourceID: "rdap", SourceClass: domain.SourceMetadata, SourceRevision: "v1", Metadata: `{"owner":"injected"}`, ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid},
	}
	target := domain.TargetDefinition{ID: "keenetic", FormatKey: "keenetic-bat-ipv4-v1", RendererID: "keenetic-route-bat", Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: 1024, MaxArtifactSize: 131072}}
	plan, err := BuildPlanSet([]ListInput{{Definition: definition, Sightings: sightings}}, target, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rules) != 1 || plan.Rules[0].CanonicalValue() != "192.0.2.1" {
		t.Fatalf("unsafe stale/metadata rule reached plan: %#v", plan.Rules)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestBuildPlanRefusesSpecialUseAndOverWideDestinations(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	target := domain.TargetDefinition{ID: "ip-only", FormatKey: "ip-only", Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsIPv6: true, SupportsPrefixes: true}}
	definition := domain.ListDefinition{ID: "example", Components: []domain.ComponentDefinition{{ID: "web"}}, CatalogRevision: "test"}
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"the operator's own private network", "10.0.0.0/8", ReasonSpecialUseDestination},
		{"a home LAN range", "192.168.1.0/24", ReasonSpecialUseDestination},
		{"loopback", "127.0.0.1", ReasonSpecialUseDestination},
		{"the cloud metadata address", "169.254.169.254", ReasonSpecialUseDestination},
		{"carrier-grade NAT space", "100.64.0.1", ReasonSpecialUseDestination},
		{"half the internet", "0.0.0.0/1", ReasonSpecialUseDestination},
		{"a quarter of the internet", "64.0.0.0/2", ReasonPrefixTooWide},
		{"an over-wide IPv6 prefix", "2001:db8::/8", ReasonPrefixTooWide},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			// A community source is entitled to declare its own networks, which
			// is exactly why the value itself has to be judged.
			resource, err := resourceFromString(test.value)
			if err != nil {
				t.Fatal(err)
			}
			sighting := domain.Sighting{
				ListID: "example", ComponentID: "web", Resource: resource,
				SourceID: "feed:community", SourceClass: domain.SourceCommunity, SourceRevision: "test",
				FirstSeen: now, LastSeen: now, ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid, ObservationCount: 1,
			}
			plan, err := BuildPlan(definition, []domain.Sighting{sighting}, target, now)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Rules) != 0 {
				t.Fatalf("%q became a route rule: %#v", test.value, plan.Rules)
			}
			if len(plan.Excluded) != 1 || !contains(plan.Excluded[0].ReasonCodes, test.want) {
				t.Fatalf("excluded = %#v, want reason %q", plan.Excluded, test.want)
			}
			if plan.Excluded[0].Outcome != "rejected" {
				t.Fatalf("outcome = %q, want rejected", plan.Excluded[0].Outcome)
			}
		})
	}
}

func TestBuildPlanRefusesASpecialUseSeedWhateverDeclaredIt(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	target := domain.TargetDefinition{ID: "ip-only", FormatKey: "ip-only", Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true}}
	definition := domain.ListDefinition{
		ID: "example", Components: []domain.ComponentDefinition{{ID: "web"}}, CatalogRevision: "test",
		Seeds: []domain.Seed{{Kind: domain.RulePrefix4, Value: "192.168.0.0/16", ComponentID: "web", SourceID: "official:vendor", SourceClass: domain.SourceOfficial}},
	}
	plan, err := BuildPlan(definition, nil, target, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rules) != 0 {
		t.Fatalf("an official seed put a LAN range on the device: %#v", plan.Rules)
	}
	if len(plan.Excluded) != 1 || !contains(plan.Excluded[0].ReasonCodes, ReasonSpecialUseDestination) {
		t.Fatalf("excluded = %#v", plan.Excluded)
	}
}

func TestBuildPlanDomainSightingCoversAddressesReportedWithIt(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	// One feed answer carrying a name and the addresses it resolves to. The name
	// alone covers the component, so carrying the addresses spends the device's
	// budget twice for the same coverage.
	target := domain.TargetDefinition{ID: "domain-and-ip", FormatKey: "domain-and-ip", Constraints: domain.TargetConstraints{SupportsDomainSuffix: true, SupportsIPv4: true, SupportsPrefixes: true}}
	definition := domain.ListDefinition{ID: "example", Components: []domain.ComponentDefinition{{ID: "web"}}, CatalogRevision: "test"}
	sightings := make([]domain.Sighting, 0, 3)
	for _, value := range []string{"198.51.100.7", "198.51.100.8", "example.com"} {
		resource, err := resourceFromString(value)
		if err != nil {
			t.Fatal(err)
		}
		sightings = append(sightings, domain.Sighting{
			ListID: "example", ComponentID: "web", Resource: resource,
			SourceID: "feed:community", SourceClass: domain.SourceCommunity, SourceRevision: "test",
			FirstSeen: now, LastSeen: now, ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid, ObservationCount: 1,
		})
	}
	plan, err := BuildPlan(definition, sightings, target, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rules) != 1 || plan.Rules[0].Kind != domain.RuleDomainSuffix {
		t.Fatalf("rules = %#v, want only the name", plan.Rules)
	}
	if len(plan.Excluded) != 2 {
		t.Fatalf("excluded = %#v, want both addresses", plan.Excluded)
	}
	for _, excluded := range plan.Excluded {
		if !contains(excluded.ReasonCodes, ReasonNotRequiredForDomainCapableTarget) {
			t.Fatalf("excluded reasons = %#v", excluded.ReasonCodes)
		}
	}
}

// resourceFromString builds the resource a feed value would produce, choosing
// prefix or address the same way the feed decoder does.
func resourceFromString(value string) (domain.Resource, error) {
	if strings.Contains(value, "/") {
		return domain.NewPrefixResourceFromString(value)
	}
	if strings.Contains(value, ".") && !strings.ContainsAny(value, "abcdefghijklmnopqrstuvwxyz") {
		return domain.NewAddrResourceFromString(value)
	}
	if strings.Contains(value, ":") {
		return domain.NewAddrResourceFromString(value)
	}
	return domain.NewDomainResource(value)
}
