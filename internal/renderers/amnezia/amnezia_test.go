package amnezia

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/planner"
)

func testTarget() domain.TargetProfile {
	return domain.TargetProfile{
		ID: "amnezia", ProfileKey: Version, RendererID: ID,
		Constraints: domain.TargetConstraints{
			SupportsDomainExact: true,
			SupportsIPv4:        true, SupportsPrefixes: true,
			MaxRules: MaxEntries, MaxArtifactSize: MaxArtifactSize,
		},
	}
}

func planWith(t *testing.T, seeds []domain.Seed, sightings []domain.Sighting) domain.RoutingPlan {
	t.Helper()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	definition := domain.ServiceDefinition{
		ID: "example", CatalogRevision: "catalog",
		Components: []domain.ComponentDefinition{{ID: "web", Required: true}},
		Seeds:      seeds,
	}
	plan, err := planner.BuildPlanSet([]planner.ServiceInput{{Definition: definition, Sightings: sightings}}, testTarget(), now)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func exactSeed(value string) domain.Seed {
	return domain.Seed{Kind: domain.RuleDomainExact, Value: value, ComponentID: "web", SourceID: "manual:" + value, SourceClass: domain.SourceManual}
}

func officialPrefix(t *testing.T, value string) domain.Sighting {
	t.Helper()
	resource, err := domain.NewPrefixResourceFromString(value)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	return domain.Sighting{ServiceID: "example", ComponentID: "web", Resource: resource, SourceID: "feed", SourceClass: domain.SourceOfficial, SourceRevision: "v1", ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid}
}

func TestRenderProducesTheGoldenSiteList(t *testing.T) {
	plan := planWith(t, []domain.Seed{exactSeed("www.youtube.com"), exactSeed("i.ytimg.com")},
		[]domain.Sighting{officialPrefix(t, "198.51.100.0/24"), officialPrefix(t, "192.0.2.0/24")})
	payload, err := Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("testdata", "sites.golden.json")
	if os.Getenv("ROUTEVANE_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture is checked out with the platform's line endings, so it is
	// normalized to this format's own before the comparison. Without this a
	// golden file matches only on the platform that happened to write it.
	if string(payload) != strings.ReplaceAll(string(golden), "\r\n", "\n") {
		t.Fatalf("rendered list does not match the golden file:\n--- got ---\n%s\n--- want ---\n%s", payload, golden)
	}
	if err := Validate(payload); err != nil {
		t.Fatalf("golden list failed its own validator: %v", err)
	}
	// The importer refuses anything but an array at the top level.
	var probe []map[string]any
	if err := json.Unmarshal(payload, &probe); err != nil {
		t.Fatalf("the document must be a JSON array: %v", err)
	}
	if len(probe) != 4 {
		t.Fatalf("entries = %d", len(probe))
	}
	for _, entry := range probe {
		if _, present := entry["hostname"]; !present {
			t.Fatalf("every entry must carry a hostname: %#v", entry)
		}
		if len(entry) != 2 {
			t.Fatalf("an entry must carry exactly hostname and ips: %#v", entry)
		}
	}
}

func TestRenderIsIndependentOfPlanRuleOrder(t *testing.T) {
	plan := planWith(t, []domain.Seed{exactSeed("a.example.com"), exactSeed("b.example.com")},
		[]domain.Sighting{officialPrefix(t, "192.0.2.0/24"), officialPrefix(t, "198.51.100.0/24")})
	forward, err := Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	reversed := plan
	reversed.Rules = append([]domain.RouteRule(nil), plan.Rules...)
	for left, right := 0, len(reversed.Rules)-1; left < right; left, right = left+1, right-1 {
		reversed.Rules[left], reversed.Rules[right] = reversed.Rules[right], reversed.Rules[left]
	}
	backward, err := Render(reversed)
	if err != nil {
		t.Fatal(err)
	}
	if string(forward) != string(backward) {
		t.Fatalf("rule order changed the list:\n%s\n%s", forward, backward)
	}
}

func TestSupportedRuleKindsExcludeWhatTheClientWouldNotRoute(t *testing.T) {
	kinds := map[domain.RuleKind]struct{}{}
	for _, kind := range (Renderer{}).SupportedRuleKinds() {
		kinds[kind] = struct{}{}
	}
	for _, kind := range []domain.RuleKind{domain.RuleDomainExact, domain.RuleIPv4, domain.RulePrefix4} {
		if _, ok := kinds[kind]; !ok {
			t.Fatalf("rule kind %q must be supported", kind)
		}
	}
	// The client collects IPv4 answers only, and it resolves the single name it
	// was given rather than matching a suffix.
	for _, kind := range []domain.RuleKind{domain.RuleIPv6, domain.RulePrefix6, domain.RuleDomainSuffix} {
		if _, ok := kinds[kind]; ok {
			t.Fatalf("rule kind %q must not be claimed", kind)
		}
	}
}

func TestDescriptorDescribesTheFormatItRenders(t *testing.T) {
	descriptor := (Renderer{}).Descriptor()
	if !descriptor.IsValid() {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if descriptor.ID != ID || descriptor.Version != Version || descriptor.ContentType != ContentType || descriptor.FileExtension != "json" {
		t.Fatalf("descriptor = %#v", descriptor)
	}
}

func TestProjectedRuleCountCountsCanonicalEntriesOnce(t *testing.T) {
	plan := planWith(t, []domain.Seed{exactSeed("a.example.com")}, []domain.Sighting{officialPrefix(t, "192.0.2.0/24")})
	count, err := ProjectedRuleCount(plan)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	duplicated := plan
	duplicated.Rules = append(append([]domain.RouteRule(nil), plan.Rules...), plan.Rules...)
	duplicateCount, err := ProjectedRuleCount(duplicated)
	if err != nil {
		t.Fatal(err)
	}
	if duplicateCount != count {
		t.Fatalf("duplicate rules changed the projection: %d vs %d", duplicateCount, count)
	}
}

func validList() string {
	return `[
  {
    "hostname": "192.0.2.0/24",
    "ips": []
  },
  {
    "hostname": "a.example.com",
    "ips": []
  }
]
`
}

func TestValidateRejectsHostileAndNonCanonicalDocuments(t *testing.T) {
	valid := validList()
	if err := Validate([]byte(valid)); err != nil {
		t.Fatalf("canonical list rejected: %v", err)
	}
	cases := []struct {
		name    string
		payload string
	}{
		{"empty", ""},
		{"no trailing newline", strings.TrimRight(valid, "\n")},
		{"object at top level", `{"sites":[]}` + "\n"},
		{"empty array", "[]\n"},
		{"unknown field", strings.Replace(valid, `"ips": []`, `"ips": [], "extra": 1`, 1)},
		{"missing hostname", strings.Replace(valid, `"hostname": "192.0.2.0/24",`, "", 1)},
		{"populated address list", strings.Replace(valid, `"ips": []`, `"ips": ["192.0.2.1"]`, 1)},
		{"unsorted entries", `[
  {
    "hostname": "a.example.com",
    "ips": []
  },
  {
    "hostname": "192.0.2.0/24",
    "ips": []
  }
]
`},
		{"duplicate entry", strings.Replace(valid, `  {
    "hostname": "a.example.com",`, `  {
    "hostname": "192.0.2.0/24",
    "ips": []
  },
  {
    "hostname": "a.example.com",`, 1)},
		{"bare address", strings.Replace(valid, "192.0.2.0/24", "192.0.2.1", 1)},
		{"unmasked prefix", strings.Replace(valid, "192.0.2.0/24", "192.0.2.5/24", 1)},
		{"ipv6 prefix", strings.Replace(valid, "192.0.2.0/24", "2001:db8::/32", 1)},
		{"single label hostname", strings.Replace(valid, "a.example.com", "localhost", 1)},
		{"uppercase hostname", strings.Replace(valid, "a.example.com", "A.example.com", 1)},
		{"trailing dot hostname", strings.Replace(valid, "a.example.com", "a.example.com.", 1)},
		{"padded hostname", strings.Replace(valid, `"a.example.com"`, `" a.example.com"`, 1)},
		{"scheme prefixed hostname", strings.Replace(valid, "a.example.com", "https://a.example.com", 1)},
		{"compact form", `[{"hostname":"a.example.com","ips":[]}]` + "\n"},
		{"trailing content", valid + "[]\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := Validate([]byte(testCase.payload)); err == nil {
				t.Fatalf("invalid list accepted: %q", testCase.payload)
			}
		})
	}
}

func TestRenderRefusesARuleKindTheFormatCannotExpress(t *testing.T) {
	plan := planWith(t, []domain.Seed{exactSeed("a.example.com")}, nil)
	for _, kind := range []domain.RuleKind{domain.RuleDomainSuffix, domain.RuleIPv6} {
		hostile := plan
		hostile.Rules = append([]domain.RouteRule(nil), plan.Rules...)
		rule := domain.RouteRule{Kind: kind, Action: domain.ActionRoute, ServiceID: "example", ComponentID: "web"}
		if kind == domain.RuleDomainSuffix {
			rule.Domain = "example.com"
		}
		hostile.Rules = append(hostile.Rules, rule)
		if _, err := Render(hostile); err == nil {
			t.Fatalf("rule kind %q must fail the render", kind)
		}
	}
}

func FuzzParseNeverAcceptsANonCanonicalDocument(f *testing.F) {
	f.Add(validList())
	f.Add("[]\n")
	f.Add("")
	f.Add(`[{"hostname":"a.example.com","ips":[]}]` + "\n")
	f.Fuzz(func(t *testing.T, payload string) {
		sites, err := Parse([]byte(payload))
		if err != nil {
			return
		}
		if len(sites) == 0 {
			t.Fatalf("accepted a list with no entry: %q", payload)
		}
		rendered, renderErr := renderSites(sites)
		if renderErr != nil {
			t.Fatalf("accepted a list its own projection cannot render: %v", renderErr)
		}
		if string(rendered) != payload {
			t.Fatalf("accepted a non-canonical list: %q", payload)
		}
		var probe []json.RawMessage
		if err := json.Unmarshal([]byte(payload), &probe); err != nil {
			t.Fatalf("accepted a document that is not a JSON array: %q", payload)
		}
	})
}
