package planner

import (
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

func degradedTarget() domain.TargetProfile {
	return domain.TargetProfile{
		ID: "keenetic", ProfileKey: "keenetic-bat-ipv4-v1", RendererID: "keenetic-route-bat",
		Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: 1024, MaxArtifactSize: 131072},
	}
}

func degradedDefinition() domain.ServiceDefinition {
	return domain.ServiceDefinition{ID: "example", CatalogRevision: "catalog", Components: []domain.ComponentDefinition{{ID: "web", Required: true}}}
}

func addrSighting(t *testing.T, value, sourceID string, class domain.SourceClass, validUntil time.Time, validity domain.ObservationValidity) domain.Sighting {
	t.Helper()
	resource, err := domain.NewAddrResourceFromString(value)
	if err != nil {
		t.Fatal(err)
	}
	return domain.Sighting{ServiceID: "example", ComponentID: "web", Resource: resource, SourceID: sourceID, SourceClass: class, SourceRevision: "v1", ValidUntil: validUntil, Validity: validity}
}

func prefixSighting(t *testing.T, value, sourceID string, class domain.SourceClass, validUntil time.Time, evidence domain.SharedNetworkEvidence) domain.Sighting {
	t.Helper()
	resource, err := domain.NewPrefixResourceFromString(value)
	if err != nil {
		t.Fatal(err)
	}
	return domain.Sighting{ServiceID: "example", ComponentID: "web", Resource: resource, SourceID: sourceID, SourceClass: class, SourceRevision: "v1", ValidUntil: validUntil, Validity: domain.ValidityValid, SharedNetworkEvidence: evidence}
}

func TestGraceKeepsExpiredObservationsRoutableAndMarksThePlanDegraded(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	expired := addrSighting(t, "192.0.2.7", "dns", domain.SourceObserved, now.Add(-time.Hour), domain.ValidityStale)
	input := ServiceInput{
		Definition: degradedDefinition(),
		Sightings:  []domain.Sighting{expired},
		Degraded:   []DegradedSource{{SourceID: "dns", GraceUntil: now.Add(6 * time.Hour)}},
	}
	plan, err := BuildPlanSet([]ServiceInput{input}, degradedTarget(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rules) != 1 || plan.Rules[0].CanonicalValue() != "192.0.2.7" {
		t.Fatalf("grace did not keep the route: %#v", plan.Rules)
	}
	if !contains(plan.Rules[0].ReasonCodes, ReasonSourceDegraded) {
		t.Fatalf("grace-admitted rule is missing its reason code: %#v", plan.Rules[0].ReasonCodes)
	}
	if plan.Rules[0].ExpiresAt == nil || !plan.Rules[0].ExpiresAt.Equal(now.Add(6*time.Hour)) {
		t.Fatalf("grace-admitted rule must expire with the grace window: %#v", plan.Rules[0].ExpiresAt)
	}
	if !contains(plan.Warnings, ReasonSourceDegraded+":example:dns") {
		t.Fatalf("plan is missing the source_degraded warning: %#v", plan.Warnings)
	}
}

func TestGraceDoesNotApplyOutsideItsWindowOrToOtherSources(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	expired := addrSighting(t, "192.0.2.7", "dns", domain.SourceObserved, now.Add(-time.Hour), domain.ValidityStale)
	fresh := addrSighting(t, "192.0.2.8", "other", domain.SourceObserved, now.Add(time.Hour), domain.ValidityValid)
	cases := []struct {
		name     string
		degraded []DegradedSource
	}{
		{"window already closed", []DegradedSource{{SourceID: "dns", GraceUntil: now}}},
		{"another source is degraded", []DegradedSource{{SourceID: "other", GraceUntil: now.Add(time.Hour)}}},
		{"no degraded source", nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			input := ServiceInput{Definition: degradedDefinition(), Sightings: []domain.Sighting{expired, fresh}, Degraded: testCase.degraded}
			plan, err := BuildPlanSet([]ServiceInput{input}, degradedTarget(), now)
			if err != nil {
				t.Fatal(err)
			}
			for _, rule := range plan.Rules {
				if rule.CanonicalValue() == "192.0.2.7" {
					t.Fatalf("an expired observation outside grace became a route: %#v", plan.Rules)
				}
			}
			if len(plan.Rules) != 1 || plan.Rules[0].CanonicalValue() != "192.0.2.8" {
				t.Fatalf("rules = %#v", plan.Rules)
			}
		})
	}
}

func TestGraceNeverRevivesArchivedOrInvalidObservations(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	fresh := addrSighting(t, "192.0.2.8", "dns", domain.SourceObserved, now.Add(time.Hour), domain.ValidityValid)
	for _, validity := range []domain.ObservationValidity{domain.ValidityArchived, domain.ValidityInvalid} {
		t.Run(string(validity), func(t *testing.T) {
			retained := addrSighting(t, "192.0.2.7", "dns", domain.SourceObserved, now.Add(-100*24*time.Hour), validity)
			input := ServiceInput{
				Definition: degradedDefinition(),
				Sightings:  []domain.Sighting{retained, fresh},
				Degraded:   []DegradedSource{{SourceID: "dns", GraceUntil: now.Add(time.Hour)}},
			}
			plan, err := BuildPlanSet([]ServiceInput{input}, degradedTarget(), now)
			if err != nil {
				t.Fatal(err)
			}
			for _, rule := range plan.Rules {
				if rule.CanonicalValue() == "192.0.2.7" {
					t.Fatalf("grace revived a %s observation: %#v", validity, plan.Rules)
				}
			}
		})
	}
}

func TestOfficialFeedPrefixesRouteWhileInferredPrefixesStayQuarantined(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	official := prefixSighting(t, "192.0.2.0/24", "feed", domain.SourceOfficial, now.Add(time.Hour), domain.SharedNetworkEvidenceNone)
	sharedOfficial := prefixSighting(t, "198.51.100.0/24", "feed", domain.SourceOfficial, now.Add(time.Hour), domain.SharedNetworkEvidenceTrusted)
	inferred := prefixSighting(t, "203.0.113.0/24", "rdap", domain.SourceMetadata, now.Add(time.Hour), domain.SharedNetworkEvidenceNone)
	observed := prefixSighting(t, "192.0.2.128/25", "dns", domain.SourceObserved, now.Add(time.Hour), domain.SharedNetworkEvidenceNone)
	input := ServiceInput{Definition: degradedDefinition(), Sightings: []domain.Sighting{official, sharedOfficial, inferred, observed}}
	plan, err := BuildPlanSet([]ServiceInput{input}, degradedTarget(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rules) != 1 || plan.Rules[0].CanonicalValue() != "192.0.2.0/24" {
		t.Fatalf("only the operator-declared prefix may route: %#v", plan.Rules)
	}
	if !contains(plan.Rules[0].ReasonCodes, ReasonOfficialRule) {
		t.Fatalf("official prefix is missing its reason code: %#v", plan.Rules[0].ReasonCodes)
	}
	quarantined := map[string][]string{}
	for _, excluded := range plan.Excluded {
		quarantined[excluded.Candidate.CanonicalValue()] = excluded.ReasonCodes
	}
	for _, value := range []string{"198.51.100.0/24", "203.0.113.0/24", "192.0.2.128/25"} {
		reasons, ok := quarantined[value]
		if !ok || !contains(reasons, ReasonWideNetworkExpansion) {
			t.Fatalf("prefix %q must stay quarantined: %#v", value, quarantined)
		}
	}
	if !contains(quarantined["198.51.100.0/24"], ReasonSharedCDNorCloud) {
		t.Fatalf("shared-network evidence must survive on an official prefix: %#v", quarantined["198.51.100.0/24"])
	}
}

func TestGracedPlanStaysDeterministicAcrossDegradedOrder(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	first := addrSighting(t, "192.0.2.7", "alpha", domain.SourceObserved, now.Add(-time.Hour), domain.ValidityStale)
	second := addrSighting(t, "192.0.2.8", "beta", domain.SourceObserved, now.Add(-time.Hour), domain.ValidityStale)
	window := []DegradedSource{{SourceID: "alpha", GraceUntil: now.Add(time.Hour)}, {SourceID: "beta", GraceUntil: now.Add(time.Hour)}}
	reversed := []DegradedSource{window[1], window[0]}
	definition := degradedDefinition()
	left, err := BuildPlanSet([]ServiceInput{{Definition: definition, Sightings: []domain.Sighting{first, second}, Degraded: window}}, degradedTarget(), now)
	if err != nil {
		t.Fatal(err)
	}
	right, err := BuildPlanSet([]ServiceInput{{Definition: definition, Sightings: []domain.Sighting{second, first}, Degraded: reversed}}, degradedTarget(), now)
	if err != nil {
		t.Fatal(err)
	}
	if left.SemanticHash != right.SemanticHash {
		t.Fatalf("degraded ordering changed the semantic hash: %s vs %s", left.SemanticHash, right.SemanticHash)
	}
	for _, warning := range []string{ReasonSourceDegraded + ":example:alpha", ReasonSourceDegraded + ":example:beta"} {
		if !contains(left.Warnings, warning) {
			t.Fatalf("warning %q missing: %#v", warning, left.Warnings)
		}
	}
}
