package discovery

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

func TestClassifyAppliesTheActivationPolicyDeterministically(t *testing.T) {
	target := testTarget(t, "https://app.example.co.uk/")
	evidence := SessionEvidence{
		Target: target,
		Hosts: []HostEvidence{
			// Same-site, required component, exercised: accepted.
			{Host: "app.example.co.uk", Component: domain.ComponentCore, StepIDs: []string{"open"}},
			{Host: "auth.example.co.uk", Component: domain.ComponentAuth, StepIDs: []string{"sign-in"}},
			{Host: "media.example.co.uk", Component: domain.ComponentMedia, StepIDs: []string{"play"}},
			// Same-site but the component was never exercised.
			{Host: "voice.example.co.uk", Component: domain.ComponentVoice},
			// Same-site optional components are recorded, never activated.
			{Host: "metrics.example.co.uk", Component: domain.ComponentTelemetry, StepIDs: []string{"open"}},
			{Host: "ads.example.co.uk", Component: domain.ComponentAdvertising, StepIDs: []string{"open"}},
			// Same-site without attribution.
			{Host: "unknown.example.co.uk"},
			// Outside the registrable domain, whatever the attribution claims.
			{Host: "cdn.thirdparty.test", Component: domain.ComponentCore, StepIDs: []string{"open"}},
			{Host: "other.co.uk", Component: domain.ComponentAuth},
			// Refused by policy.
			{Host: "internal.thirdparty.test", Refused: RefusedLocalDestination},
		},
	}
	exercised := []string{domain.ComponentCore, domain.ComponentAuth, domain.ComponentMedia, domain.ComponentTelemetry, domain.ComponentAdvertising}
	decisions := Classify(evidence, exercised)
	outcomes := map[string]Decision{}
	for _, decision := range decisions {
		outcomes[decision.Host] = decision
	}
	accepted := []string{"app.example.co.uk", "auth.example.co.uk", "media.example.co.uk"}
	for _, host := range accepted {
		if outcomes[host].Outcome != ActivationAccepted {
			t.Fatalf("%q = %#v, want accepted", host, outcomes[host])
		}
		if !contains(outcomes[host].Reasons, ReasonSameSiteRequiredComponent) {
			t.Fatalf("%q reasons = %#v", host, outcomes[host].Reasons)
		}
	}
	dependencies := map[string]string{
		"voice.example.co.uk":      ReasonComponentNotExercised,
		"metrics.example.co.uk":    ReasonOptionalComponent,
		"ads.example.co.uk":        ReasonOptionalComponent,
		"unknown.example.co.uk":    ReasonUnknownComponent,
		"cdn.thirdparty.test":      ReasonSharedThirdParty,
		"other.co.uk":              ReasonSharedThirdParty,
		"internal.thirdparty.test": CandidateRefusedByPolicy,
	}
	for host, reason := range dependencies {
		if outcomes[host].Outcome != ActivationDependency {
			t.Fatalf("%q = %#v, want dependency", host, outcomes[host])
		}
		if !contains(outcomes[host].Reasons, reason) {
			t.Fatalf("%q reasons = %#v, want %q", host, outcomes[host].Reasons, reason)
		}
	}

	// The same evidence in a different order produces the same decisions.
	reversed := evidence
	reversed.Hosts = append([]HostEvidence(nil), evidence.Hosts...)
	for left, right := 0, len(reversed.Hosts)-1; left < right; left, right = left+1, right-1 {
		reversed.Hosts[left], reversed.Hosts[right] = reversed.Hosts[right], reversed.Hosts[left]
	}
	again := Classify(reversed, exercised)
	if len(again) != len(decisions) {
		t.Fatalf("decision count changed: %d vs %d", len(again), len(decisions))
	}
	for index := range again {
		if again[index].Host != decisions[index].Host || again[index].Outcome != decisions[index].Outcome {
			t.Fatalf("evidence order changed decision %d: %#v vs %#v", index, again[index], decisions[index])
		}
	}
}

func TestScenarioValidateRefusesUnboundedOrUnattributableExploration(t *testing.T) {
	valid := Scenario{Target: "https://app.example.com/", Steps: []Step{
		{ID: "open", Component: domain.ComponentCore, URL: "https://app.example.com/", SettleSeconds: 1},
		{ID: "sign-in", Component: domain.ComponentAuth, URL: "https://app.example.com/login", SettleSeconds: 1},
		{ID: "play", Component: domain.ComponentMedia, SettleSeconds: 2},
	}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(valid.ExercisedComponents(), ",") != "auth,core,media" {
		t.Fatalf("exercised = %v", valid.ExercisedComponents())
	}

	tooMany := Scenario{Target: valid.Target}
	for index := 0; index <= MaxScenarioSteps; index++ {
		tooMany.Steps = append(tooMany.Steps, Step{ID: "s" + string(rune('a'+index%26)) + string(rune('a'+index/26)), Component: domain.ComponentCore, URL: valid.Target})
	}
	cases := map[string]Scenario{
		"no step":             {Target: valid.Target},
		"too many steps":      tooMany,
		"invalid step id":     {Target: valid.Target, Steps: []Step{{ID: "Open Step", Component: domain.ComponentCore, URL: valid.Target}}},
		"duplicate step id":   {Target: valid.Target, Steps: []Step{{ID: "open", Component: domain.ComponentCore, URL: valid.Target}, {ID: "open", Component: domain.ComponentAuth}}},
		"unknown component":   {Target: valid.Target, Steps: []Step{{ID: "open", Component: "guessing", URL: valid.Target}}},
		"negative settle":     {Target: valid.Target, Steps: []Step{{ID: "open", Component: domain.ComponentCore, URL: valid.Target, SettleSeconds: -1}}},
		"settle beyond bound": {Target: valid.Target, Steps: []Step{{ID: "open", Component: domain.ComponentCore, URL: valid.Target, SettleSeconds: MaxStepSettle + 1}}},
		"first step no url":   {Target: valid.Target, Steps: []Step{{ID: "open", Component: domain.ComponentCore}}},
		"local step url":      {Target: valid.Target, Steps: []Step{{ID: "open", Component: domain.ComponentCore, URL: "http://localhost:8080"}}},
		"private step url":    {Target: valid.Target, Steps: []Step{{ID: "open", Component: domain.ComponentCore, URL: "https://10.1.2.3/"}}},
	}
	for name, scenario := range cases {
		t.Run(name, func(t *testing.T) {
			if err := scenario.Validate(); !errors.Is(err, ErrInvalidScenario) {
				t.Fatalf("err = %v, want ErrInvalidScenario", err)
			}
		})
	}
}

func TestRelationsRecordProvenanceWithoutAssertingOwnership(t *testing.T) {
	target := testTarget(t, "https://app.example.com/")
	evidence := SessionEvidence{
		Target: target,
		Hosts: []HostEvidence{
			{Host: "media.example.com", Component: domain.ComponentMedia, StepIDs: []string{"play"}, LoadedBy: []string{"app.example.com"}, RedirectsTo: []string{"edge.thirdparty.test"}},
			// A self-relation is not provenance and is dropped.
			{Host: "app.example.com", Component: domain.ComponentCore, LoadedBy: []string{"app.example.com"}},
		},
	}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	relations, err := Relations(evidence, "example", "learning-session", "revision", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	byType := map[domain.RelationType][]string{}
	for _, relation := range relations {
		if !domain.KnownRelationType(relation.RelationType) {
			t.Fatalf("unknown relation type %q", relation.RelationType)
		}
		if relation.SourceResource.Kind != domain.ResourceDomain || relation.TargetResource.Kind != domain.ResourceDomain {
			t.Fatalf("a session relation must connect domains: %#v", relation)
		}
		if !relation.ValidUntil.Equal(now.Add(time.Hour)) || relation.Validity != domain.ValidityValid {
			t.Fatalf("relation lifecycle = %#v", relation)
		}
		byType[relation.RelationType] = append(byType[relation.RelationType], relation.SourceResource.CanonicalValue()+"->"+relation.TargetResource.CanonicalValue())
	}
	if strings.Join(byType[domain.RelationLoadedBy], ",") != "media.example.com->app.example.com" {
		t.Fatalf("loaded_by = %v", byType[domain.RelationLoadedBy])
	}
	if strings.Join(byType[domain.RelationRedirectsTo], ",") != "media.example.com->edge.thirdparty.test" {
		t.Fatalf("redirects_to = %v", byType[domain.RelationRedirectsTo])
	}
	if strings.Join(byType[domain.RelationObservedInSession], ",") != "media.example.com->play.step.routevane.invalid" {
		t.Fatalf("observed_in_session = %v", byType[domain.RelationObservedInSession])
	}
	if _, err := Relations(evidence, "Bad Id", "learning-session", "revision", now, time.Hour); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatal("an invalid service identity must be refused")
	}
	if _, err := Relations(evidence, "example", "learning-session", "", now, time.Hour); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatal("a missing revision must be refused")
	}
}

func TestMergeEvidenceKeepsTheStrongestAttributionAndClearsRefusal(t *testing.T) {
	target := testTarget(t, "https://app.example.com/")
	first := SessionEvidence{Steps: []string{"open"}, Requests: 2, Bytes: 10, Hosts: []HostEvidence{
		{Host: "app.example.com", Component: domain.ComponentTelemetry, StepIDs: []string{"open"}, Requests: 2},
		{Host: "blocked.thirdparty.test", Refused: RefusedLocalDestination},
	}}
	second := SessionEvidence{Steps: []string{"play"}, Requests: 3, Bytes: 20, Hosts: []HostEvidence{
		{Host: "app.example.com", Component: domain.ComponentCore, StepIDs: []string{"play"}, Requests: 3, LoadedBy: []string{"app.example.com"}},
		{Host: "blocked.thirdparty.test", Requests: 1},
	}}
	merged := MergeEvidence(target, first, second)
	if merged.Requests != 5 || merged.Bytes != 30 {
		t.Fatalf("totals = %#v", merged)
	}
	if strings.Join(merged.Steps, ",") != "open,play" {
		t.Fatalf("steps = %v", merged.Steps)
	}
	byHost := map[string]HostEvidence{}
	for _, host := range merged.Hosts {
		byHost[host.Host] = host
	}
	// The taxonomy order is the precedence: core outranks telemetry.
	if byHost["app.example.com"].Component != domain.ComponentCore {
		t.Fatalf("component = %q", byHost["app.example.com"].Component)
	}
	if strings.Join(byHost["app.example.com"].StepIDs, ",") != "open,play" {
		t.Fatalf("steps = %v", byHost["app.example.com"].StepIDs)
	}
	if byHost["app.example.com"].Requests != 5 {
		t.Fatalf("requests = %d", byHost["app.example.com"].Requests)
	}
	// A host contacted in any part is not a refused host.
	if byHost["blocked.thirdparty.test"].Refused != "" {
		t.Fatalf("refusal survived a successful contact: %#v", byHost["blocked.thirdparty.test"])
	}
}

func TestBuildLearnedDraftGivesEachExercisedAreaItsOwnComponent(t *testing.T) {
	target := testTarget(t, "https://app.example.com/")
	evidence := SessionEvidence{Hosts: []HostEvidence{
		{Host: "app.example.com", Component: domain.ComponentCore, StepIDs: []string{"open"}},
		{Host: "auth.example.com", Component: domain.ComponentAuth, StepIDs: []string{"sign-in"}},
		{Host: "media.example.com", Component: domain.ComponentMedia, StepIDs: []string{"play"}},
		{Host: "metrics.example.com", Component: domain.ComponentTelemetry, StepIDs: []string{"open"}},
		{Host: "voice.example.com", Component: domain.ComponentVoice},
		{Host: "cdn.thirdparty.test", Component: domain.ComponentCore, StepIDs: []string{"open"}},
	}}
	exercised := []string{domain.ComponentCore, domain.ComponentAuth, domain.ComponentMedia, domain.ComponentTelemetry}
	draft, err := BuildLearnedDraft(LearnedDraftRequest{Target: target, ServiceID: "example", Evidence: evidence, Exercised: exercised})
	if err != nil {
		t.Fatal(err)
	}
	components := make([]string, 0, len(draft.Definition.Components))
	for _, component := range draft.Definition.Components {
		components = append(components, component.ID)
		if component.Required != domain.RequiredComponent(component.ID) {
			t.Fatalf("component %#v disagrees with the taxonomy", component)
		}
	}
	if strings.Join(components, ",") != "core,auth,media" {
		t.Fatalf("components = %v", components)
	}
	sources := map[string][]string{}
	for _, source := range draft.Definition.Sources {
		sources[source.ComponentID] = source.Names
	}
	if strings.Join(sources[domain.ComponentCore], ",") != "app.example.com" {
		t.Fatalf("core names = %v", sources[domain.ComponentCore])
	}
	if strings.Join(sources[domain.ComponentAuth], ",") != "auth.example.com" {
		t.Fatalf("auth names = %v", sources[domain.ComponentAuth])
	}
	if strings.Join(sources[domain.ComponentMedia], ",") != "media.example.com" {
		t.Fatalf("media names = %v", sources[domain.ComponentMedia])
	}
	for _, unwanted := range []string{"metrics.example.com", "voice.example.com", "cdn.thirdparty.test"} {
		if contains(draft.AcceptedHosts, unwanted) {
			t.Fatalf("host %q must not be activated: %v", unwanted, draft.AcceptedHosts)
		}
		if !contains(candidateHostsOf(draft), unwanted) {
			t.Fatalf("host %q must stay a recorded dependency: %#v", unwanted, draft.Candidates)
		}
	}
	if strings.Join(draft.SeedDomains, ",") != "example.com" {
		t.Fatalf("seed domains = %v", draft.SeedDomains)
	}
	// The seed belongs to a component the definition actually declares.
	declared := map[string]struct{}{}
	for _, component := range draft.Definition.Components {
		declared[component.ID] = struct{}{}
	}
	for _, seed := range draft.Definition.Seeds {
		if _, ok := declared[seed.ComponentID]; !ok {
			t.Fatalf("seed %#v references an undeclared component", seed)
		}
	}
}

func TestBuildLearnedDraftRefusesEvidenceWithNothingActivatable(t *testing.T) {
	target := testTarget(t, "https://app.example.com/")
	evidence := SessionEvidence{Hosts: []HostEvidence{
		{Host: "cdn.thirdparty.test", Component: domain.ComponentCore, StepIDs: []string{"open"}},
		{Host: "metrics.example.com", Component: domain.ComponentTelemetry, StepIDs: []string{"open"}},
	}}
	if _, err := BuildLearnedDraft(LearnedDraftRequest{Target: target, ServiceID: "example", Evidence: evidence, Exercised: []string{domain.ComponentCore, domain.ComponentTelemetry}}); !errors.Is(err, ErrNoUsableEvidence) {
		t.Fatal("evidence with nothing activatable must be refused")
	}
}
