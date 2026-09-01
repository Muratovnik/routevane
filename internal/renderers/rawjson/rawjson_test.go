package rawjson

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

func TestRenderUsesCanonicalTypedValuesAndEmptyArrays(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 123, time.FixedZone("local", 3*60*60))
	rule, err := domain.NewDomainRule(domain.RuleDomainSuffix, "Example.COM.", "example", "web", domain.SourceManual, []string{"manual_rule"}, []string{"manual:example"})
	if err != nil {
		t.Fatal(err)
	}
	output, err := Render(domain.RoutingPlan{InterfaceVersion: domain.RoutingPlanInterfaceVersion, TargetID: "raw", ProfileKey: "raw-v1", Services: []string{"example"}, Rules: []domain.RouteRule{rule}, ObservationCutoff: now, PolicyVersion: "auto-v1", CatalogRevision: "m0"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if !strings.HasSuffix(text, "\n") || strings.HasSuffix(strings.TrimSuffix(text, "\n"), "\n") {
		t.Fatalf("trailing newline = %q", text)
	}
	for _, field := range []string{`"interface_version":"m0-spike-v1"`, `"excluded":[]`, `"warnings":[]`, `"coverage":[]`, `"relations":[]`, `"sightings":[]`, `"value":"example.com"`, `"observation_cutoff":"2026-08-20T09:00:00.000000123Z"`} {
		if !strings.Contains(text, field) {
			t.Fatalf("rendered JSON missing %s: %s", field, text)
		}
	}
}

func testPlan(t *testing.T) domain.RoutingPlan {
	t.Helper()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	suffixRule, err := domain.NewDomainRule(domain.RuleDomainSuffix, "youtube.com", "youtube", "web", domain.SourceManual, []string{"manual_rule"}, []string{"manual:youtube.com"})
	if err != nil {
		t.Fatal(err)
	}
	return domain.RoutingPlan{
		InterfaceVersion:  domain.RoutingPlanInterfaceVersion,
		TargetID:          "raw",
		ProfileKey:        Version,
		Services:          []string{"youtube"},
		Rules:             []domain.RouteRule{suffixRule},
		Coverage:          []domain.Coverage{{ServiceID: "youtube", ComponentID: "web", Complete: true, RuleCount: 1}},
		PolicyVersion:     "auto-v1",
		CatalogRevision:   "m0",
		ObservationCutoff: now,
		SemanticHash:      "deadbeef",
	}
}

// TestRenderProducesTheGoldenDocument pins the canonical byte form of a
// diagnostic snapshot. The golden file is regenerated with
// ROUTEVANE_UPDATE_GOLDEN=1, the same mechanism the other renderer packages
// use.
func TestRenderProducesTheGoldenDocument(t *testing.T) {
	payload, err := Render(testPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("testdata", "plan.golden.json")
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

// FuzzParseNeverAcceptsANonCanonicalDocument proves the real Parse contract:
// it decodes independently of Render (through planjson.Validate and its own
// json.Unmarshal), and any document it accepts must re-marshal to the exact
// same bytes. A document that is merely equivalent JSON but reindented,
// reordered, or carrying different field spacing must be refused rather than
// silently accepted.
func FuzzParseNeverAcceptsANonCanonicalDocument(f *testing.F) {
	golden, err := os.ReadFile(filepath.Join("testdata", "plan.golden.json"))
	if err == nil {
		f.Add(string(golden))
	}
	f.Add(`{"interface_version":"m0-spike-v1","target_id":"raw","profile_key":"raw-v1","services":[],"rules":[],"excluded":[],"warnings":[],"coverage":[],"relations":[],"sightings":[],"policy_version":"auto-v1","catalog_revision":"m0","observation_cutoff":"2026-08-20T00:00:00Z","semantic_hash":"h"}` + "\n")
	f.Add("")
	f.Add("{}\n")
	f.Fuzz(func(t *testing.T, payload string) {
		plan, err := Parse([]byte(payload))
		if err != nil {
			return
		}
		rendered, marshalErr := json.Marshal(plan)
		if marshalErr != nil {
			t.Fatalf("accepted a document its own projection cannot render: %v", marshalErr)
		}
		rendered = append(rendered, '\n')
		if string(rendered) != payload {
			t.Fatalf("accepted a non-canonical document: %q", payload)
		}
	})
}
