package openwrtnftset

import (
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
		ID: "openwrt", ProfileKey: Version, RendererID: ID,
		Constraints: domain.TargetConstraints{
			// The device resolves names itself, so this target claims suffix
			// matching and no address capability at all.
			SupportsDomainSuffix: true, SupportsDynamicDNSSet: true,
			MaxRules: MaxLines, MaxArtifactSize: MaxArtifactSize,
		},
	}
}

func planWith(t *testing.T, seeds []domain.Seed) domain.RoutingPlan {
	t.Helper()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	definition := domain.ServiceDefinition{
		ID: "example", CatalogRevision: "catalog",
		Components: []domain.ComponentDefinition{{ID: "web", Required: true}},
		Seeds:      seeds,
	}
	plan, err := planner.BuildPlanSet([]planner.ServiceInput{{Definition: definition, Sightings: nil}}, testTarget(), now)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func suffixSeed(value string) domain.Seed {
	return domain.Seed{Kind: domain.RuleDomainSuffix, Value: value, ComponentID: "web", SourceID: "manual:" + value, SourceClass: domain.SourceManual}
}

func TestRenderProducesTheGoldenFragment(t *testing.T) {
	plan := planWith(t, []domain.Seed{
		suffixSeed("youtube.com"),
		suffixSeed("googlevideo.com"),
		suffixSeed("ytimg.com"),
	})
	payload, err := Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("testdata", "dnsmasq.golden.conf")
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
		t.Fatalf("rendered fragment does not match the golden file:\n--- got ---\n%s\n--- want ---\n%s", payload, golden)
	}
	if err := Validate(payload); err != nil {
		t.Fatalf("golden fragment failed its own validator: %v", err)
	}
	// The artifact carries no address: that is the point of this format.
	for _, forbidden := range []string{"192.0.2.", "2001:db8", "/32", "/24"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("a dynamic set fragment must not carry addresses: %s", payload)
		}
	}
}

func TestRenderIsIndependentOfPlanRuleOrder(t *testing.T) {
	plan := planWith(t, []domain.Seed{suffixSeed("a.example.com"), suffixSeed("b.example.com"), suffixSeed("c.example.com")})
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
		t.Fatalf("rule order changed the fragment:\n%s\n%s", forward, backward)
	}
}

func TestSupportedRuleKindsAreNarrowerThanEveryAddressFormat(t *testing.T) {
	kinds := (Renderer{}).SupportedRuleKinds()
	if len(kinds) != 1 || kinds[0] != domain.RuleDomainSuffix {
		t.Fatalf("supported kinds = %#v", kinds)
	}
}

func TestDescriptorDescribesTheFormatItRenders(t *testing.T) {
	descriptor := (Renderer{}).Descriptor()
	if !descriptor.IsValid() {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if descriptor.ID != ID || descriptor.Version != Version || descriptor.ContentType != ContentType || descriptor.FileExtension != "conf" {
		t.Fatalf("descriptor = %#v", descriptor)
	}
}

func TestProjectedRuleCountCountsCanonicalDirectivesOnce(t *testing.T) {
	plan := planWith(t, []domain.Seed{suffixSeed("example.com"), suffixSeed("example.net")})
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

func TestValidateRejectsHostileAndNonCanonicalFragments(t *testing.T) {
	valid := Header + "\n" +
		"nftset=/a.example.com/" + setSpecification() + "\n" +
		"nftset=/b.example.com/" + setSpecification() + "\n"
	if err := Validate([]byte(valid)); err != nil {
		t.Fatalf("canonical fragment rejected: %v", err)
	}
	cases := []struct {
		name    string
		payload string
	}{
		{"empty", ""},
		{"no trailing newline", strings.TrimRight(valid, "\n")},
		{"crlf line endings", strings.ReplaceAll(valid, "\n", "\r\n")},
		{"header only", Header + "\n"},
		{"missing header", strings.TrimPrefix(valid, Header+"\n")},
		{"wrong header", strings.Replace(valid, Header, "# routevane openwrt-nftset-conf-v2", 1)},
		{"unsorted directives", Header + "\n" +
			"nftset=/b.example.com/" + setSpecification() + "\n" +
			"nftset=/a.example.com/" + setSpecification() + "\n"},
		{"duplicate directive", valid + "nftset=/b.example.com/" + setSpecification() + "\n"},
		{"blank line", strings.Replace(valid, "nftset=/b", "\nnftset=/b", 1)},
		{"padded directive", strings.Replace(valid, "nftset=/b", " nftset=/b", 1)},
		{"uppercase domain", strings.Replace(valid, "a.example.com", "A.example.com", 1)},
		{"trailing dot domain", strings.Replace(valid, "a.example.com", "a.example.com.", 1)},
		{"another option", strings.Replace(valid, "nftset=/b.example.com", "ipset=/b.example.com", 1)},
		// A directive that targets some other set would route the operator's
		// traffic through a set the installation hint never described.
		{"foreign set", strings.Replace(valid, SetIPv4, "othersetname", 1)},
		{"foreign table", strings.Replace(valid, Table, "filter", 1)},
		{"ipv4 only", strings.Replace(valid, "nftset=/b.example.com/"+setSpecification(), "nftset=/b.example.com/4#"+Family+"#"+Table+"#"+SetIPv4, 1)},
		{"comment after directives", valid + "# added by hand\n"},
		{"two domains on one line", Header + "\n" + "nftset=/a.example.com/b.example.com/" + setSpecification() + "\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := Validate([]byte(testCase.payload)); err == nil {
				t.Fatalf("invalid fragment accepted: %q", testCase.payload)
			}
		})
	}
}

func TestRenderRefusesARuleKindTheFormatCannotExpress(t *testing.T) {
	plan := planWith(t, []domain.Seed{suffixSeed("example.com")})
	// An exact domain is the case that matters: this format would silently widen
	// it to every name under the domain, so it must be refused.
	exact := plan
	exact.Rules = append([]domain.RouteRule(nil), plan.Rules...)
	exact.Rules = append(exact.Rules, domain.RouteRule{Kind: domain.RuleDomainExact, Action: domain.ActionRoute, ServiceID: "example", ComponentID: "web", Domain: "www.example.com"})
	if _, err := Render(exact); err == nil {
		t.Fatal("an exact domain rule must fail the render")
	}
	address := plan
	address.Rules = append([]domain.RouteRule(nil), plan.Rules...)
	address.Rules = append(address.Rules, domain.RouteRule{Kind: domain.RuleKind("mac_address"), Action: domain.ActionRoute, ServiceID: "example", ComponentID: "web"})
	if _, err := Render(address); err == nil {
		t.Fatal("an unsupported rule kind must fail the render")
	}
}

func FuzzParseNeverAcceptsANonCanonicalFragment(f *testing.F) {
	f.Add(Header + "\n" + "nftset=/example.com/" + setSpecification() + "\n")
	f.Add(Header + "\n")
	f.Add("")
	f.Add("nftset=/example.com/4#inet#fw4#routevane4\n")
	f.Fuzz(func(t *testing.T, payload string) {
		rules, err := Parse([]byte(payload))
		if err != nil {
			return
		}
		if len(rules) == 0 {
			t.Fatalf("accepted a fragment with no directive: %q", payload)
		}
		rendered, renderErr := renderRules(rules)
		if renderErr != nil {
			t.Fatalf("accepted a fragment its own projection cannot render: %v", renderErr)
		}
		if string(rendered) != payload {
			t.Fatalf("accepted a non-canonical fragment: %q", payload)
		}
		if !strings.HasPrefix(payload, Header+"\n") {
			t.Fatalf("accepted a fragment without the header: %q", payload)
		}
	})
}
