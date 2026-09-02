package application

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/planner"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
)

type m2Store struct {
	snapshots map[string]PlanningSnapshot
	errFor    map[string]error
}

func (*m2Store) ApplySuccess(context.Context, SuccessCycle) error  { return nil }
func (*m2Store) RecordFailure(context.Context, FailureCycle) error { return nil }
func (s *m2Store) ReadPlanningSnapshot(_ context.Context, serviceID string, _ map[string]string, _ string, _ time.Time) (PlanningSnapshot, error) {
	if err := s.errFor[serviceID]; err != nil {
		return PlanningSnapshot{}, err
	}
	return s.snapshots[serviceID], nil
}

type m2SpyRenderer struct {
	kinds       []domain.RuleKind
	version     string
	renderCalls int
}

func (*m2SpyRenderer) ID() string { return keenetic.ID }
func (r *m2SpyRenderer) Version() string {
	if r.version != "" {
		return r.version
	}
	return keenetic.Version
}
func (r *m2SpyRenderer) Descriptor() domain.RendererDescriptor {
	return domain.RendererDescriptor{ID: keenetic.ID, Version: r.Version(), ContentType: keenetic.ContentType, FileExtension: keenetic.FileExtension}
}
func (r *m2SpyRenderer) SupportedRuleKinds() []domain.RuleKind {
	return append([]domain.RuleKind(nil), r.kinds...)
}
func (*m2SpyRenderer) ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	return keenetic.ProjectedRuleCount(plan)
}
func (r *m2SpyRenderer) Render(plan domain.RoutingPlan) ([]byte, error) {
	r.renderCalls++
	return keenetic.Render(plan)
}
func (*m2SpyRenderer) Validate(payload []byte) error { return keenetic.Validate(payload) }

type m2Output struct {
	calls   int
	payload []byte
}

func (o *m2Output) Put(_ context.Context, _ domain.RendererDescriptor, _ string, payload []byte) (string, bool, error) {
	o.calls++
	o.payload = append([]byte(nil), payload...)
	return `C:\data\artifacts\artifact.bat`, false, nil
}

func TestBuildServicesPreflightRejectsUnsupportedKindBeforeRenderer(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	definition := m2Definition("example")
	one := m2Sighting("example", "192.0.2.0", now.Add(time.Hour))
	two := m2Sighting("example", "192.0.2.1", now.Add(time.Hour))
	store := &m2Store{snapshots: map[string]PlanningSnapshot{"example": m2Snapshot(definition, []domain.Sighting{one, two})}}
	renderer := &m2SpyRenderer{kinds: []domain.RuleKind{domain.RuleIPv4}}
	output := &m2Output{}
	_, err := BuildServices(context.Background(), []domain.ServiceDefinition{definition}, map[string]map[string]string{"example": {}}, m2Target(), store, renderer, output, ClockFunc(func() time.Time { return now }))
	if !errors.Is(err, ErrPreflight) || renderer.renderCalls != 0 || output.calls != 0 {
		t.Fatalf("unsupported preflight err=%v renders=%d outputs=%d", err, renderer.renderCalls, output.calls)
	}
}

func TestBuildServicesPreflightRejectsWrongRendererVersionBeforeRenderer(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	definition := m2Definition("example")
	definition.Seeds = []domain.Seed{{Kind: domain.RuleIPv4, Value: "192.0.2.1", ComponentID: "web", SourceID: "manual:ip", SourceClass: domain.SourceManual}}
	store := &m2Store{snapshots: map[string]PlanningSnapshot{"example": m2Snapshot(definition, nil)}}
	renderer := &m2SpyRenderer{version: "keenetic-bat-ipv4-v2", kinds: []domain.RuleKind{domain.RuleIPv4, domain.RulePrefix4}}
	output := &m2Output{}
	_, err := BuildServices(context.Background(), []domain.ServiceDefinition{definition}, map[string]map[string]string{"example": {}}, m2Target(), store, renderer, output, ClockFunc(func() time.Time { return now }))
	if !errors.Is(err, ErrPreflight) || renderer.renderCalls != 0 || output.calls != 0 {
		t.Fatalf("version preflight err=%v renders=%d outputs=%d", err, renderer.renderCalls, output.calls)
	}
}

func TestBuildServicesGlobal1025LimitAndRequiredCoverageFailClosed(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	t.Run("global 1025", func(t *testing.T) {
		alpha := m2Definition("alpha")
		beta := m2Definition("beta")
		for i := 0; i < 513; i++ {
			alpha.Seeds = append(alpha.Seeds, m2IPv4Seed(i, "alpha"))
		}
		for i := 513; i < 1025; i++ {
			beta.Seeds = append(beta.Seeds, m2IPv4Seed(i, "beta"))
		}
		store := &m2Store{snapshots: map[string]PlanningSnapshot{"alpha": m2Snapshot(alpha, nil), "beta": m2Snapshot(beta, nil)}}
		renderer := &m2SpyRenderer{kinds: []domain.RuleKind{domain.RuleIPv4, domain.RulePrefix4}}
		output := &m2Output{}
		_, err := BuildServices(context.Background(), []domain.ServiceDefinition{beta, alpha}, map[string]map[string]string{"alpha": {}, "beta": {}}, m2Target(), store, renderer, output, ClockFunc(func() time.Time { return now }))
		if !errors.Is(err, ErrRuleLimit) || !errors.Is(err, ErrPreflight) || renderer.renderCalls != 0 || output.calls != 0 {
			t.Fatalf("1025 rule build err=%v renders=%d outputs=%d", err, renderer.renderCalls, output.calls)
		}
	})
	t.Run("required uncovered", func(t *testing.T) {
		definition := m2Definition("example")
		definition.Seeds = []domain.Seed{{Kind: domain.RuleDomainSuffix, Value: "example.com", ComponentID: "web", SourceID: "manual:domain", SourceClass: domain.SourceManual}}
		store := &m2Store{snapshots: map[string]PlanningSnapshot{"example": m2Snapshot(definition, nil)}}
		renderer := &m2SpyRenderer{kinds: []domain.RuleKind{domain.RuleIPv4, domain.RulePrefix4}}
		output := &m2Output{}
		_, err := BuildServices(context.Background(), []domain.ServiceDefinition{definition}, map[string]map[string]string{"example": {}}, m2Target(), store, renderer, output, ClockFunc(func() time.Time { return now }))
		if !errors.Is(err, ErrPartialCoverage) || renderer.renderCalls != 0 || output.calls != 0 {
			t.Fatalf("uncovered build err=%v renders=%d outputs=%d", err, renderer.renderCalls, output.calls)
		}
	})
}

func TestBuildServicesAppliesLineLimitAfterEquivalentRouteProjection(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	definition := domain.ServiceDefinition{ID: "example", CatalogRevision: "catalog-revision"}
	for i := 0; i < keenetic.MaxLines+1; i++ {
		componentID := fmt.Sprintf("component-%04d", i)
		definition.Components = append(definition.Components, domain.ComponentDefinition{ID: componentID, Required: true})
		seed := domain.Seed{Kind: domain.RuleIPv4, Value: "192.0.2.1", ComponentID: componentID, SourceID: fmt.Sprintf("manual:ipv4:%04d", i), SourceClass: domain.SourceManual}
		if i%2 == 1 {
			seed.Kind = domain.RulePrefix4
			seed.Value = "192.0.2.1/32"
			seed.SourceID = fmt.Sprintf("manual:prefix4:%04d", i)
		}
		definition.Seeds = append(definition.Seeds, seed)
	}
	store := &m2Store{snapshots: map[string]PlanningSnapshot{"example": m2Snapshot(definition, nil)}}
	renderer := &m2SpyRenderer{kinds: []domain.RuleKind{domain.RuleIPv4, domain.RulePrefix4}}
	output := &m2Output{}
	result, err := BuildServices(context.Background(), []domain.ServiceDefinition{definition}, map[string]map[string]string{"example": {}}, m2Target(), store, renderer, output, ClockFunc(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	if result.RuleCount != keenetic.MaxLines+1 || renderer.renderCalls != 1 || output.calls != 1 {
		t.Fatalf("projected build result=%#v renders=%d outputs=%d", result, renderer.renderCalls, output.calls)
	}
	parsed, err := keenetic.Parse(output.payload)
	if err != nil || len(parsed) != 1 || parsed[0].Prefix.String() != "192.0.2.1/32" {
		t.Fatalf("equivalent policy routes were not projected once: parsed=%#v err=%v", parsed, err)
	}
}

func TestBuildServicesRejectsDefaultRouteBeforeRenderer(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	definition := m2Definition("example")
	definition.Seeds = []domain.Seed{{Kind: domain.RulePrefix4, Value: "0.0.0.0/0", ComponentID: "web", SourceID: "manual:default", SourceClass: domain.SourceManual}}
	store := &m2Store{snapshots: map[string]PlanningSnapshot{"example": m2Snapshot(definition, nil)}}
	renderer := &m2SpyRenderer{kinds: []domain.RuleKind{domain.RuleIPv4, domain.RulePrefix4}}
	output := &m2Output{}
	_, err := BuildServices(context.Background(), []domain.ServiceDefinition{definition}, map[string]map[string]string{"example": {}}, m2Target(), store, renderer, output, ClockFunc(func() time.Time { return now }))
	// The default route is refused by policy, before the preflight it used to
	// reach: a destination that swallows the whole address space is not a
	// candidate the planner admits. The component is then uncovered, which is
	// what fails the build. What matters here is unchanged and stronger — no
	// renderer and no output ever sees it.
	if !errors.Is(err, ErrPartialCoverage) || renderer.renderCalls != 0 || output.calls != 0 {
		t.Fatalf("default route refusal err=%v renders=%d outputs=%d", err, renderer.renderCalls, output.calls)
	}
}

func TestBuildServicesTwoServiceMissingStateIsAllOrNothing(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	alpha := m2Definition("alpha")
	beta := m2Definition("beta")
	store := &m2Store{snapshots: map[string]PlanningSnapshot{"alpha": m2Snapshot(alpha, []domain.Sighting{m2Sighting("alpha", "192.0.2.1", now.Add(time.Hour))})}, errFor: map[string]error{"beta": errors.New("missing")}}
	renderer := &m2SpyRenderer{kinds: []domain.RuleKind{domain.RuleIPv4, domain.RulePrefix4}}
	output := &m2Output{}
	_, err := BuildServices(context.Background(), []domain.ServiceDefinition{beta, alpha}, map[string]map[string]string{"alpha": {}, "beta": {}}, m2Target(), store, renderer, output, ClockFunc(func() time.Time { return now }))
	if err == nil || renderer.renderCalls != 0 || output.calls != 0 {
		t.Fatalf("missing state was not all-or-nothing: err=%v renders=%d outputs=%d", err, renderer.renderCalls, output.calls)
	}
}

func TestBuildServicesEmitsExplicitPartialDiagnosticWithoutCatalogText(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	definition := m2Definition("example")
	definition.Title = "SENTINEL-TITLE"
	definition.Seeds = []domain.Seed{{Kind: domain.RuleDomainSuffix, Value: "example.com", ComponentID: "web", SourceID: "manual:domain", SourceClass: domain.SourceManual}}
	target := m2Target()
	target.ManualInstallationHint = "SENTINEL-HINT"
	store := &m2Store{snapshots: map[string]PlanningSnapshot{"example": m2Snapshot(definition, []domain.Sighting{m2Sighting("example", "192.0.2.1", now.Add(time.Hour))})}}
	output := &m2Output{}
	result, err := BuildServices(context.Background(), []domain.ServiceDefinition{definition}, map[string]map[string]string{"example": {}}, target, store, keenetic.Renderer{}, output, ClockFunc(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "partial_coverage" || result.Diagnostics[0].Count == 0 {
		t.Fatalf("partial diagnostics = %#v", result.Diagnostics)
	}
	if strings.Contains(string(output.payload), "SENTINEL") || output.calls != 1 {
		t.Fatalf("catalog text leaked into output: %q", output.payload)
	}
	parsed, err := keenetic.Parse(output.payload)
	if err != nil || len(parsed) != 1 || parsed[0].Prefix != netip.MustParsePrefix("192.0.2.1/32") {
		t.Fatalf("independent output projection=%#v err=%v", parsed, err)
	}
}

func TestBuildServicesParsedOutputExcludesStaleAndInjectedBroadMetadata(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	definition := m2Definition("example")
	fresh := m2Sighting("example", "192.0.2.1", now.Add(time.Hour))
	stale := m2Sighting("example", "192.0.2.2", now)
	stale.Validity = domain.ValidityStale
	broadResource, _ := domain.NewPrefixResourceFromString("104.16.0.0/12")
	broad := domain.Sighting{ServiceID: "example", ComponentID: "web", Resource: broadResource, SourceID: "rdap", SourceClass: domain.SourceMetadata, SourceRevision: "injected", Metadata: `{"owner":"SENTINEL-METADATA"}`, ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid}
	store := &m2Store{snapshots: map[string]PlanningSnapshot{"example": m2Snapshot(definition, []domain.Sighting{broad, stale, fresh})}}
	output := &m2Output{}
	_, err := BuildServices(context.Background(), []domain.ServiceDefinition{definition}, map[string]map[string]string{"example": {}}, m2Target(), store, keenetic.Renderer{}, output, ClockFunc(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := keenetic.Parse(output.payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || parsed[0].Prefix.String() != "192.0.2.1/32" || strings.Contains(string(output.payload), "104.16") || strings.Contains(string(output.payload), "192.0.2.2") || strings.Contains(string(output.payload), "SENTINEL") {
		t.Fatalf("unsafe stale/metadata candidate reached parsed output: parsed=%#v payload=%q", parsed, output.payload)
	}
}

func TestBuildServicesFreshEvidenceSurvivesAnExpiredSightingOfTheSameAddress(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	definition := m2Definition("example")
	fresh := m2Sighting("example", "192.0.2.1", now.Add(time.Hour))
	stale := fresh
	stale.SourceID = "old-source"
	stale.ValidUntil = now
	for _, sightings := range [][]domain.Sighting{{stale, fresh}, {fresh, stale}} {
		store := &m2Store{snapshots: map[string]PlanningSnapshot{"example": m2Snapshot(definition, sightings)}}
		output := &m2Output{}
		_, err := BuildServices(context.Background(), []domain.ServiceDefinition{definition}, map[string]map[string]string{"example": {}}, m2Target(), store, keenetic.Renderer{}, output, ClockFunc(func() time.Time { return now }))
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := keenetic.Parse(output.payload)
		if err != nil || len(parsed) != 1 || parsed[0].Prefix.String() != "192.0.2.1/32" {
			t.Fatalf("fresh evidence was lost: parsed=%#v err=%v", parsed, err)
		}
	}
}

func TestPreflightRejectsStaleAndInjectedSemanticHash(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	definition := m2Definition("example")
	definition.Seeds = []domain.Seed{{Kind: domain.RuleIPv4, Value: "192.0.2.1", ComponentID: "web", SourceID: "manual:ip", SourceClass: domain.SourceManual}}
	plan, err := planner.BuildPlanSet([]planner.ServiceInput{{Definition: definition}}, m2Target(), now)
	if err != nil {
		t.Fatal(err)
	}
	renderer := keenetic.Renderer{}
	injected := plan
	injected.SemanticHash = strings.Repeat("f", 64)
	if err := PreflightPlan(injected, m2Target(), renderer, now); !errors.Is(err, ErrPreflight) {
		t.Fatalf("injected semantic hash error=%v", err)
	}
	stale := plan
	stale.Rules = append([]domain.RouteRule(nil), plan.Rules...)
	expires := now
	stale.Rules[0].ExpiresAt = &expires
	stale.SemanticHash = planner.SemanticHash(stale, m2Target())
	if err := PreflightPlan(stale, m2Target(), renderer, now); !errors.Is(err, ErrPreflight) {
		t.Fatalf("stale plan error=%v", err)
	}
}

func m2Definition(id string) domain.ServiceDefinition {
	return domain.ServiceDefinition{ID: id, CatalogRevision: "catalog-revision", Components: []domain.ComponentDefinition{{ID: "web", Required: true}}}
}

func m2Target() domain.TargetProfile {
	return domain.TargetProfile{ID: "keenetic", ProfileKey: keenetic.Version, RendererID: keenetic.ID, Constraints: domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxRules: keenetic.MaxLines, MaxArtifactSize: keenetic.MaxArtifactSize}}
}

func m2Sighting(serviceID, address string, validUntil time.Time) domain.Sighting {
	resource, _ := domain.NewAddrResourceFromString(address)
	return domain.Sighting{ServiceID: serviceID, ComponentID: "web", Resource: resource, SourceID: "dns", SourceClass: domain.SourceObserved, SourceRevision: "v1", ValidUntil: validUntil, Validity: domain.ValidityValid}
}

func m2Snapshot(definition domain.ServiceDefinition, sightings []domain.Sighting) PlanningSnapshot {
	raw := domain.RawJSONTargetProfile()
	return PlanningSnapshot{Sightings: sightings, Profile: ProfileRecord{ProfileKey: raw.ProfileKey, ServiceID: definition.ID, TargetID: raw.ID, RendererID: raw.RendererID, CatalogRevision: definition.CatalogRevision}}
}

// m2IPv4Seed produces addresses no lossless collapse may merge: every value
// has an even final octet, so its /31 sibling is never present and the rule
// count the limit is asserted against survives planning.
func m2IPv4Seed(index int, service string) domain.Seed {
	third := index / 126
	fourth := (index % 126) * 2
	return domain.Seed{Kind: domain.RuleIPv4, Value: fmt.Sprintf("198.18.%d.%d", third, fourth), ComponentID: "web", SourceID: fmt.Sprintf("manual:%s:%d", service, index), SourceClass: domain.SourceManual}
}
