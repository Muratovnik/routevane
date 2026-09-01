package singbox

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
		ID: "singbox", ProfileKey: Version, RendererID: ID,
		Constraints: domain.TargetConstraints{
			SupportsDomainExact: true, SupportsDomainSuffix: true,
			SupportsIPv4: true, SupportsIPv6: true, SupportsPrefixes: true,
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

func officialPrefix(t *testing.T, value string) domain.Sighting {
	t.Helper()
	resource, err := domain.NewPrefixResourceFromString(value)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	return domain.Sighting{ServiceID: "example", ComponentID: "web", Resource: resource, SourceID: "feed", SourceClass: domain.SourceOfficial, SourceRevision: "v1", ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid}
}

func TestRenderProducesTheGoldenSourceRuleSet(t *testing.T) {
	plan := planWith(t, []domain.Seed{
		{Kind: domain.RuleDomainSuffix, Value: "youtube.com", ComponentID: "web", SourceID: "manual:youtube.com", SourceClass: domain.SourceManual},
		{Kind: domain.RuleDomainSuffix, Value: "googlevideo.com", ComponentID: "web", SourceID: "manual:googlevideo.com", SourceClass: domain.SourceManual},
		{Kind: domain.RuleDomainExact, Value: "www.youtube.com", ComponentID: "web", SourceID: "manual:www.youtube.com", SourceClass: domain.SourceManual},
	}, []domain.Sighting{
		officialPrefix(t, "2001:db8::/32"),
		officialPrefix(t, "198.51.100.0/24"),
		officialPrefix(t, "192.0.2.0/24"),
	})
	payload, err := Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("testdata", "ruleset.golden.json")
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
		t.Fatalf("rendered document does not match the golden file:\n--- got ---\n%s\n--- want ---\n%s", payload, golden)
	}
	if err := Validate(payload); err != nil {
		t.Fatalf("golden document failed its own validator: %v", err)
	}
}

func TestRenderIsIndependentOfPlanRuleOrder(t *testing.T) {
	plan := planWith(t, []domain.Seed{
		{Kind: domain.RuleDomainSuffix, Value: "example.com", ComponentID: "web", SourceID: "manual:example.com", SourceClass: domain.SourceManual},
	}, []domain.Sighting{officialPrefix(t, "192.0.2.0/24"), officialPrefix(t, "198.51.100.0/24")})
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
		t.Fatalf("rule order changed the document:\n%s\n%s", forward, backward)
	}
}

func TestSupportedRuleKindsDifferFromTheRouterDialect(t *testing.T) {
	kinds := map[domain.RuleKind]struct{}{}
	for _, kind := range (Renderer{}).SupportedRuleKinds() {
		kinds[kind] = struct{}{}
	}
	for _, kind := range []domain.RuleKind{domain.RuleDomainExact, domain.RuleDomainSuffix, domain.RuleIPv4, domain.RuleIPv6, domain.RulePrefix4, domain.RulePrefix6} {
		if _, ok := kinds[kind]; !ok {
			t.Fatalf("rule kind %q must be supported by this format", kind)
		}
	}
	if len(kinds) != 6 {
		t.Fatalf("unexpected supported kinds: %#v", kinds)
	}
}

func TestDescriptorDescribesTheFormatItRenders(t *testing.T) {
	descriptor := (Renderer{}).Descriptor()
	if !descriptor.IsValid() {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if descriptor.ID != ID || descriptor.Version != Version || descriptor.ContentType != "application/json" || descriptor.FileExtension != "json" {
		t.Fatalf("descriptor = %#v", descriptor)
	}
}

func TestProjectedRuleCountCountsCanonicalEntriesOnce(t *testing.T) {
	plan := planWith(t, []domain.Seed{
		{Kind: domain.RuleDomainSuffix, Value: "example.com", ComponentID: "web", SourceID: "manual:example.com", SourceClass: domain.SourceManual},
	}, []domain.Sighting{officialPrefix(t, "192.0.2.0/24")})
	count, err := ProjectedRuleCount(plan)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	// A duplicated plan rule must not change the count the target limit sees.
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

func TestValidateRejectsHostileAndNonCanonicalDocuments(t *testing.T) {
	valid := `{
  "version": 3,
  "rules": [
    {
      "domain_suffix": [
        "example.com"
      ],
      "ip_cidr": [
        "192.0.2.0/24"
      ]
    }
  ]
}
`
	if err := Validate([]byte(valid)); err != nil {
		t.Fatalf("canonical document rejected: %v", err)
	}
	cases := []struct {
		name    string
		payload string
	}{
		{"empty", ""},
		{"no trailing newline", strings.TrimRight(valid, "\n")},
		{"unsupported version", strings.Replace(valid, `"version": 3`, `"version": 2`, 1)},
		{"unknown top level field", strings.Replace(valid, `"version": 3`, `"version": 3,`+"\n"+`  "extra": 1`, 1)},
		{"unknown rule field", strings.Replace(valid, `"domain_suffix"`, `"process_name"`, 1)},
		{"two rules", strings.Replace(valid, "  ]\n}", "  ,{}]\n}", 1)},
		{"no rules", `{"version":3,"rules":[]}` + "\n"},
		{"empty match set", `{"version":3,"rules":[{}]}` + "\n"},
		{"unsorted domains", strings.Replace(valid, `"example.com"`, `"z.example.com",`+"\n"+`        "a.example.com"`, 1)},
		{"duplicate prefix", strings.Replace(valid, `"192.0.2.0/24"`, `"192.0.2.0/24",`+"\n"+`        "192.0.2.0/24"`, 1)},
		{"unmasked prefix", strings.Replace(valid, `"192.0.2.0/24"`, `"192.0.2.5/24"`, 1)},
		{"invalid prefix", strings.Replace(valid, `"192.0.2.0/24"`, `"192.0.2.0/33"`, 1)},
		{"padded prefix", strings.Replace(valid, `"192.0.2.0/24"`, `" 192.0.2.0/24"`, 1)},
		{"uppercase domain", strings.Replace(valid, `"example.com"`, `"Example.com"`, 1)},
		{"trailing dot domain", strings.Replace(valid, `"example.com"`, `"example.com."`, 1)},
		{"trailing content", valid + "{}\n"},
		{"compact form", `{"version":3,"rules":[{"domain_suffix":["example.com"]}]}` + "\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := Validate([]byte(testCase.payload)); err == nil {
				t.Fatalf("invalid document accepted: %s", testCase.payload)
			}
		})
	}
}

func TestRenderRefusesARuleKindTheFormatCannotExpress(t *testing.T) {
	plan := planWith(t, []domain.Seed{
		{Kind: domain.RuleDomainSuffix, Value: "example.com", ComponentID: "web", SourceID: "manual:example.com", SourceClass: domain.SourceManual},
	}, nil)
	plan.Rules = append(plan.Rules, domain.RouteRule{Kind: domain.RuleKind("mac_address"), Action: domain.ActionRoute, ServiceID: "example", ComponentID: "web"})
	if _, err := Render(plan); err == nil {
		t.Fatal("an unsupported rule kind must fail the render")
	}
}

func FuzzParseNeverAcceptsANonCanonicalDocument(f *testing.F) {
	f.Add(`{"version":3,"rules":[{"domain_suffix":["example.com"]}]}` + "\n")
	f.Add("{\n  \"version\": 3,\n  \"rules\": [\n    {\n      \"ip_cidr\": [\n        \"192.0.2.0/24\"\n      ]\n    }\n  ]\n}\n")
	f.Add("")
	f.Fuzz(func(t *testing.T, payload string) {
		ruleSet, err := Parse([]byte(payload))
		if err != nil {
			return
		}
		if ruleSet.Version != FormatVersion {
			t.Fatalf("accepted version %d", ruleSet.Version)
		}
		rendered, renderErr := renderRule(ruleSet.Rule)
		if renderErr != nil {
			t.Fatalf("accepted a document its own projection cannot render: %v", renderErr)
		}
		if string(rendered) != payload {
			t.Fatalf("accepted a non-canonical document: %q", payload)
		}
		var probe map[string]json.RawMessage
		if err := json.Unmarshal([]byte(payload), &probe); err != nil {
			t.Fatalf("accepted a document that is not a JSON object: %q", payload)
		}
	})
}
