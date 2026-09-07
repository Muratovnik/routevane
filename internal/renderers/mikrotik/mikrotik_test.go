package mikrotik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/planner"
)

func testTarget() domain.TargetDefinition {
	return domain.TargetDefinition{
		ID: "mikrotik", FormatKey: Version, RendererID: ID,
		Constraints: domain.TargetConstraints{
			SupportsDomainExact: true,
			SupportsIPv4:        true, SupportsIPv6: true, SupportsPrefixes: true,
			MaxRules: MaxLines, MaxArtifactSize: MaxArtifactSize,
		},
	}
}

func planWith(t *testing.T, seeds []domain.Seed, sightings []domain.Sighting) domain.RoutingPlan {
	t.Helper()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	definition := domain.ListDefinition{
		ID: "example", CatalogRevision: "catalog",
		Components: []domain.ComponentDefinition{{ID: "web", Required: true}},
		Seeds:      seeds,
	}
	plan, err := planner.BuildPlanSet([]planner.ListInput{{Definition: definition, Sightings: sightings}}, testTarget(), now)
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
	return domain.Sighting{ListID: "example", ComponentID: "web", Resource: resource, SourceID: "feed", SourceClass: domain.SourceOfficial, SourceRevision: "v1", ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid}
}

func TestRenderProducesTheGoldenScript(t *testing.T) {
	plan := planWith(t, []domain.Seed{
		exactSeed("www.youtube.com"),
		exactSeed("i.ytimg.com"),
	}, []domain.Sighting{
		officialPrefix(t, "2001:db8::/32"),
		officialPrefix(t, "198.51.100.0/24"),
		officialPrefix(t, "192.0.2.0/24"),
	})
	payload, err := Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("testdata", "addresslist.golden.rsc")
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
		t.Fatalf("rendered script does not match the golden file:\n--- got ---\n%s\n--- want ---\n%s", payload, golden)
	}
	if err := Validate(payload); err != nil {
		t.Fatalf("golden script failed its own validator: %v", err)
	}
	// The script must replace what it owns, not add to it.
	if strings.Count(string(payload), "remove [find list=") != 2 {
		t.Fatalf("both sections must clear their own list first:\n%s", payload)
	}
	// A name belongs in both sections because each list resolves its own family.
	if strings.Count(string(payload), "www.youtube.com") != 2 {
		t.Fatalf("an exact domain must appear in both sections:\n%s", payload)
	}
}

func TestRenderIsIndependentOfPlanRuleOrder(t *testing.T) {
	plan := planWith(t, []domain.Seed{exactSeed("a.example.com"), exactSeed("b.example.com")},
		[]domain.Sighting{officialPrefix(t, "192.0.2.0/24"), officialPrefix(t, "198.51.100.0/24"), officialPrefix(t, "2001:db8::/32")})
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
		t.Fatalf("rule order changed the script:\n%s\n%s", forward, backward)
	}
}

func TestSupportedRuleKindsDifferFromTheOtherFormats(t *testing.T) {
	kinds := map[domain.RuleKind]struct{}{}
	for _, kind := range (Renderer{}).SupportedRuleKinds() {
		kinds[kind] = struct{}{}
	}
	for _, kind := range []domain.RuleKind{domain.RuleDomainExact, domain.RuleIPv4, domain.RuleIPv6, domain.RulePrefix4, domain.RulePrefix6} {
		if _, ok := kinds[kind]; !ok {
			t.Fatalf("rule kind %q must be supported by this format", kind)
		}
	}
	// A suffix is an address-list entry this format cannot express.
	if _, ok := kinds[domain.RuleDomainSuffix]; ok {
		t.Fatal("a suffix must not be claimed by an address-list format")
	}
	if len(kinds) != 5 {
		t.Fatalf("unexpected supported kinds: %#v", kinds)
	}
}

func TestDescriptorDescribesTheFormatItRenders(t *testing.T) {
	descriptor := (Renderer{}).Descriptor()
	if !descriptor.IsValid() {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if descriptor.ID != ID || descriptor.Version != Version || descriptor.ContentType != ContentType || descriptor.FileExtension != "rsc" {
		t.Fatalf("descriptor = %#v", descriptor)
	}
}

func TestProjectedRuleCountCountsEveryEntryOnce(t *testing.T) {
	plan := planWith(t, []domain.Seed{exactSeed("a.example.com")},
		[]domain.Sighting{officialPrefix(t, "192.0.2.0/24"), officialPrefix(t, "2001:db8::/32")})
	count, err := ProjectedRuleCount(plan)
	if err != nil {
		t.Fatal(err)
	}
	// One prefix per family plus the name in both sections.
	if count != 4 {
		t.Fatalf("count = %d, want 4", count)
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

func validScript() string {
	return Header + "\n" +
		sectionIPv4 + "\n" +
		"remove [find list=" + ListIPv4 + "]\n" +
		"add address=192.0.2.0/24 list=" + ListIPv4 + "\n" +
		"add address=a.example.com list=" + ListIPv4 + "\n" +
		sectionIPv6 + "\n" +
		"remove [find list=" + ListIPv6 + "]\n" +
		"add address=2001:db8::/32 list=" + ListIPv6 + "\n"
}

func TestValidateRejectsHostileAndNonCanonicalScripts(t *testing.T) {
	valid := validScript()
	if err := Validate([]byte(valid)); err != nil {
		t.Fatalf("canonical script rejected: %v", err)
	}
	cases := []struct {
		name    string
		payload string
	}{
		{"empty", ""},
		{"no trailing newline", strings.TrimRight(valid, "\n")},
		{"crlf line endings", strings.ReplaceAll(valid, "\n", "\r\n")},
		{"missing header", strings.TrimPrefix(valid, Header+"\n")},
		{"header only", Header + "\n"},
		{"section without removal", strings.Replace(valid, "remove [find list="+ListIPv4+"]\n", "", 1)},
		{"foreign list cleared", strings.Replace(valid, "remove [find list="+ListIPv4+"]", "remove [find list=other]", 1)},
		{"foreign list added", strings.Replace(valid, "add address=192.0.2.0/24 list="+ListIPv4, "add address=192.0.2.0/24 list=other", 1)},
		{"entry outside a section", Header + "\n" + "add address=192.0.2.0/24 list=" + ListIPv4 + "\n"},
		{"sections out of order", Header + "\n" +
			sectionIPv6 + "\nremove [find list=" + ListIPv6 + "]\nadd address=2001:db8::/32 list=" + ListIPv6 + "\n" +
			sectionIPv4 + "\nremove [find list=" + ListIPv4 + "]\nadd address=192.0.2.0/24 list=" + ListIPv4 + "\n"},
		{"repeated section", valid + sectionIPv6 + "\nremove [find list=" + ListIPv6 + "]\n"},
		{"unsorted prefixes", strings.Replace(valid, "add address=192.0.2.0/24 list="+ListIPv4+"\n",
			"add address=198.51.100.0/24 list="+ListIPv4+"\nadd address=192.0.2.0/24 list="+ListIPv4+"\n", 1)},
		{"name before prefix", strings.Replace(valid,
			"add address=192.0.2.0/24 list="+ListIPv4+"\nadd address=a.example.com list="+ListIPv4,
			"add address=a.example.com list="+ListIPv4+"\nadd address=192.0.2.0/24 list="+ListIPv4, 1)},
		{"duplicate entry", strings.Replace(valid, "add address=192.0.2.0/24 list="+ListIPv4+"\n",
			"add address=192.0.2.0/24 list="+ListIPv4+"\nadd address=192.0.2.0/24 list="+ListIPv4+"\n", 1)},
		{"bare address", strings.Replace(valid, "192.0.2.0/24", "192.0.2.1", 1)},
		{"unmasked prefix", strings.Replace(valid, "192.0.2.0/24", "192.0.2.5/24", 1)},
		{"wrong family section", strings.Replace(valid, "add address=2001:db8::/32 list="+ListIPv6, "add address=192.0.2.0/24 list="+ListIPv6, 1)},
		{"uppercase domain", strings.Replace(valid, "a.example.com", "A.example.com", 1)},
		{"trailing dot domain", strings.Replace(valid, "a.example.com", "a.example.com.", 1)},
		{"padded line", strings.Replace(valid, "add address=192.0.2.0/24", " add address=192.0.2.0/24", 1)},
		{"blank line", valid + "\n"},
		{"unknown command", valid + "/system reboot\n"},
		{"extra property", strings.Replace(valid, "list="+ListIPv4+"\n", "list="+ListIPv4+" timeout=1d\n", 1)},
		{"comment appended", valid + "# added by hand\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := Validate([]byte(testCase.payload)); err == nil {
				t.Fatalf("invalid script accepted: %q", testCase.payload)
			}
		})
	}
}

func TestRenderRefusesARuleKindTheFormatCannotExpress(t *testing.T) {
	plan := planWith(t, []domain.Seed{exactSeed("a.example.com")}, nil)
	suffix := plan
	suffix.Rules = append([]domain.RouteRule(nil), plan.Rules...)
	suffix.Rules = append(suffix.Rules, domain.RouteRule{Kind: domain.RuleDomainSuffix, Action: domain.ActionRoute, ListID: "example", ComponentID: "web", Domain: "example.com"})
	if _, err := Render(suffix); err == nil {
		t.Fatal("a suffix rule must fail the render")
	}
}

func FuzzParseNeverAcceptsANonCanonicalScript(f *testing.F) {
	f.Add(validScript())
	f.Add(Header + "\n")
	f.Add("")
	f.Add(Header + "\n" + sectionIPv4 + "\nremove [find list=" + ListIPv4 + "]\nadd address=a.example.com list=" + ListIPv4 + "\n")
	f.Fuzz(func(t *testing.T, payload string) {
		script, err := Parse([]byte(payload))
		if err != nil {
			return
		}
		if len(script.IPv4)+len(script.IPv6) == 0 {
			t.Fatalf("accepted a script with no entry: %q", payload)
		}
		rendered, renderErr := renderScript(script)
		if renderErr != nil {
			t.Fatalf("accepted a script its own projection cannot render: %v", renderErr)
		}
		if string(rendered) != payload {
			t.Fatalf("accepted a non-canonical script: %q", payload)
		}
		if !strings.HasPrefix(payload, Header+"\n") {
			t.Fatalf("accepted a script without the header: %q", payload)
		}
	})
}
