package application

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/renderers/amnezia"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
	"github.com/Muratovnik/routevane/internal/renderers/mikrotik"
	"github.com/Muratovnik/routevane/internal/renderers/openwrtnftset"
	"github.com/Muratovnik/routevane/internal/renderers/singbox"
)

// forecastTestService wires the shape the defect appears in: two lists whose
// planned rules overflow one device and fit another, and one address both
// lists contribute, so the renderer's collapse is visible in the answer.
//
// The bounded target's limit is deliberately small rather than a real device's
// 1024: the arithmetic that matters is projected against maximum, and a test
// that had to plan a thousand rules to show it would be measuring the fixture.
func forecastTestService(t *testing.T, store interface {
	ObservationStore
	PublicationRepository
}, files PublishedArtifacts) *PublicationService {
	t.Helper()
	revision := strings.Repeat("c", 64)
	definitions := map[string]domain.ListDefinition{
		"youtube": {
			ID: "youtube", Title: "YouTube", CatalogRevision: revision,
			Components: []domain.ComponentDefinition{{ID: "web", Required: true}},
			Seeds: []domain.Seed{
				{Kind: domain.RuleIPv4, Value: "192.0.2.1", ComponentID: "web", SourceID: "manual:seed", SourceClass: domain.SourceManual},
				{Kind: domain.RuleIPv4, Value: "192.0.2.2", ComponentID: "web", SourceID: "manual:seed", SourceClass: domain.SourceManual},
			},
		},
		"discord": {
			ID: "discord", Title: "Discord", CatalogRevision: revision,
			Components: []domain.ComponentDefinition{{ID: "web", Required: true}},
			Seeds: []domain.Seed{
				{Kind: domain.RuleIPv4, Value: "192.0.2.10", ComponentID: "web", SourceID: "manual:seed", SourceClass: domain.SourceManual},
			},
		},
	}
	constraints := domain.TargetConstraints{SupportsIPv4: true, SupportsPrefixes: true, MaxArtifactSize: keenetic.MaxArtifactSize}
	bounded := constraints
	bounded.MaxRules = 2
	targets := map[string]domain.TargetDefinition{
		"keenetic":  {ID: "keenetic", FormatKey: keenetic.Version, RendererID: keenetic.ID, Constraints: bounded},
		"unbounded": {ID: "unbounded", FormatKey: keenetic.Version, RendererID: keenetic.ID, Constraints: constraints},
	}
	publication, err := NewPublicationService(PublicationConfig{
		Definitions: definitions,
		Categories:  map[string]domain.CategoryDefinition{"video": {ID: "video", Title: "Видео", Lists: []string{"youtube"}}},
		Targets:     targets, TargetRevision: strings.Repeat("t", 64),
		Store: store, Files: files,
		Renderers: RendererRegistry{keenetic.ID: keenetic.Renderer{}},
		Sources:   SourceRegistry{domain.SourceDNS: publicationFakeSource{}},
		Clock:     ClockFunc(func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }),
	})
	if err != nil {
		t.Fatal(err)
	}
	return publication
}

// The forecast answers an overflowing pair with the real numbers instead of the
// refusal a build would raise. That is the whole point: the product knows both
// counts before the profile exists, and a screen that only learned "it failed"
// would still be sending the operator to the first build to find out.
func TestForecastAnswersAnOverflowingTargetWithItsNumbers(t *testing.T) {
	publication := forecastTestService(t, &publicationFakeStore{}, &publicationFakeFiles{})
	forecasts, err := publication.ForecastComposition(context.Background(), ProfileComposition{Lists: []string{"youtube", "discord"}}, []string{"keenetic"})
	if err != nil {
		t.Fatal(err)
	}
	if len(forecasts) != 1 {
		t.Fatalf("forecasts = %#v", forecasts)
	}
	got := forecasts[0]
	if got.TargetID != "keenetic" || got.MaximumRules != 2 || got.Fits {
		t.Fatalf("forecast = %#v", got)
	}
	// Four canonical BAT lines: three distinct seeded addresses plus the one
	// observed address. Both lists observe it, so it is one line.
	if got.ProjectedRules != 4 {
		t.Fatalf("projected = %d, want the renderer's own count of 4", got.ProjectedRules)
	}
	wantPerList := []ListRuleForecast{{ListID: "discord", Rules: 1}, {ListID: "youtube", Rules: 3}}
	if !reflect.DeepEqual(got.PerList, wantPerList) {
		t.Fatalf("per list = %#v, want %#v", got.PerList, wantPerList)
	}
	// Category-first priority assigns the shared address to YouTube before projection; the
	// per-list shares now describe the same finished plan the device receives.
	sum := 0
	for _, share := range got.PerList {
		sum += share.Rules
	}
	if sum != 4 || sum != got.ProjectedRules {
		t.Fatalf("per list sum = %d, projected = %d", sum, got.ProjectedRules)
	}
}

// A target that declares no rule bound always fits, and it must not be reported
// as fitting inside a maximum of zero — a screen that read a limit there would
// invent one the device never stated.
func TestForecastReportsAnUnlimitedTargetAsFitting(t *testing.T) {
	publication := forecastTestService(t, &publicationFakeStore{}, &publicationFakeFiles{})
	forecasts, err := publication.ForecastComposition(context.Background(), ProfileComposition{Lists: []string{"youtube", "discord"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// An empty target set asks about the whole catalog, in a stable order.
	if len(forecasts) != 2 || forecasts[0].TargetID != "keenetic" || forecasts[1].TargetID != "unbounded" {
		t.Fatalf("forecasts = %#v", forecasts)
	}
	unbounded := forecasts[1]
	if unbounded.MaximumRules != 0 || !unbounded.Fits || unbounded.ProjectedRules != 4 {
		t.Fatalf("unbounded forecast = %#v", unbounded)
	}
	// The same composition on the same catalog projects the same count on both
	// devices; only the verdict differs, because only the bound differs.
	if forecasts[0].ProjectedRules != unbounded.ProjectedRules || forecasts[0].Fits {
		t.Fatalf("bounded = %#v unbounded = %#v", forecasts[0], unbounded)
	}
}

// A category reference is resolved before the forecast plans, so a screen
// forecasts the collection the operator picked rather than the ids they typed.
func TestForecastResolvesCategoriesAndExclusionsBeforePlanning(t *testing.T) {
	publication := forecastTestService(t, &publicationFakeStore{}, &publicationFakeFiles{})
	forecasts, err := publication.ForecastComposition(context.Background(), ProfileComposition{Categories: []string{"video"}}, []string{"unbounded"})
	if err != nil {
		t.Fatal(err)
	}
	want := []ListRuleForecast{{ListID: "youtube", Rules: 3}}
	if len(forecasts) != 1 || !reflect.DeepEqual(forecasts[0].PerList, want) || forecasts[0].ProjectedRules != 3 {
		t.Fatalf("forecast = %#v", forecasts[0])
	}
	// Excluding the category's only member leaves nothing to forecast. An empty
	// answer would read as "nothing to worry about" for a profile that cannot be
	// created at all, so it is refused with the same words creation uses.
	if _, err := publication.ForecastComposition(context.Background(), ProfileComposition{Categories: []string{"video"}, Exclusions: []string{"youtube"}}, nil); err == nil {
		t.Fatal("a composition resolving to no lists was forecast rather than refused")
	}
	if _, err := publication.ForecastComposition(context.Background(), ProfileComposition{Lists: []string{"absent"}}, nil); err == nil {
		t.Fatal("an unknown list was forecast rather than refused")
	}
	if _, err := publication.ForecastComposition(context.Background(), ProfileComposition{Lists: []string{"youtube"}}, []string{"absent-device"}); err == nil {
		t.Fatal("an unknown target was forecast rather than refused")
	}
}

// forecastGuardStore fails the forecast the moment it writes. Every read stays
// the real fake's, because the forecast must plan from the same snapshots a
// build reads; every write records its name and refuses, so a persisted
// forecast shows up as a named write rather than as a stray row.
type forecastGuardStore struct {
	*publicationFakeStore
	writes []string
}

func (s *forecastGuardStore) refuse(name string) error {
	s.writes = append(s.writes, name)
	return fmt.Errorf("a read-only forecast wrote through %s", name)
}

func (s *forecastGuardStore) Publish(context.Context, PublicationCandidate) (Output, PlanSnapshotRecord, ArtifactBuildRecord, error) {
	return Output{}, PlanSnapshotRecord{}, ArtifactBuildRecord{}, s.refuse("Publish")
}
func (s *forecastGuardStore) RecordOutputAttempt(context.Context, OutputAttempt) error {
	return s.refuse("RecordOutputAttempt")
}

// ApplySuccess is the profile write: a refresh cycle carries the effective
// target profile with it, and a forecast that ran one would rewrite what the
// next build reads back.
func (s *forecastGuardStore) ApplySuccess(context.Context, SuccessCycle) error {
	return s.refuse("ApplySuccess")
}
func (s *forecastGuardStore) RecordFailure(context.Context, FailureCycle) error {
	return s.refuse("RecordFailure")
}
func (s *forecastGuardStore) CreateProfile(context.Context, Profile) error {
	return s.refuse("CreateList")
}
func (s *forecastGuardStore) UpdateProfile(context.Context, Profile) error {
	return s.refuse("UpdateList")
}
func (s *forecastGuardStore) CreateOutput(context.Context, NewOutput) error {
	return s.refuse("CreateOutput")
}
func (s *forecastGuardStore) CreateSubscription(context.Context, string, string, [32]byte, time.Time) error {
	return s.refuse("CreateSubscription")
}
func (s *forecastGuardStore) PutSetting(context.Context, string, string, time.Time) error {
	return s.refuse("PutSetting")
}

// The forecast is a read. It answers before the operator has committed to
// anything, so it must leave no profile, output, attempt, artifact, or
// effective format behind — including on the overflowing pair, where the equivalent build would
// record a failed attempt.
func TestForecastWritesNothing(t *testing.T) {
	store := &forecastGuardStore{publicationFakeStore: &publicationFakeStore{}}
	files := &publicationFakeFiles{}
	publication := forecastTestService(t, store, files)
	if _, err := publication.ForecastComposition(context.Background(), ProfileComposition{Lists: []string{"youtube", "discord"}}, nil); err != nil {
		t.Fatal(err)
	}
	if len(store.writes) != 0 {
		t.Fatalf("the forecast wrote through %v", store.writes)
	}
	if files.puts != 0 {
		t.Fatalf("the forecast stored %d artifact(s)", files.puts)
	}
	if len(store.attempts) != 0 || len(store.published) != 0 || store.profile.ID != "" || store.output.ID != "" {
		t.Fatalf("the forecast left state behind: %#v", store.publicationFakeStore)
	}
	// A refused forecast writes nothing either: the build path records a failed
	// attempt against an output, and a preview has no output to record against.
	if _, err := publication.ForecastComposition(context.Background(), ProfileComposition{Lists: []string{"absent"}}, nil); err == nil {
		t.Fatal("expected the refusal")
	}
	if len(store.writes) != 0 || len(store.attempts) != 0 {
		t.Fatalf("a refused forecast wrote through %v / %#v", store.writes, store.attempts)
	}
}

func TestForecastOverlapUsesOnlyThePreparedCutoffAndDistinctLists(t *testing.T) {
	store := &forecastGuardStore{publicationFakeStore: &publicationFakeStore{}}
	files := &publicationFakeFiles{}
	publication := forecastTestService(t, store, files)
	publication.config.Categories["other"] = domain.CategoryDefinition{ID: "other", Lists: []string{"youtube"}}
	ctx := context.Background()
	read := func(composition ProfileComposition) CompositionForecast {
		t.Helper()
		result, err := publication.ForecastComposition(ctx, composition, []string{"unbounded"})
		if err != nil {
			t.Fatal(err)
		}
		return result[0]
	}
	a := read(ProfileComposition{Lists: []string{"youtube"}})
	b := read(ProfileComposition{Lists: []string{"discord"}})
	ab := read(ProfileComposition{Lists: []string{"youtube", "discord"}, Categories: []string{"video", "other"}})
	if len(a.Overlaps.Items) != 0 || len(b.Overlaps.Items) != 0 || ab.Overlaps.Truncated || len(ab.Overlaps.Items) != 1 {
		t.Fatalf("a=%#v b=%#v ab=%#v", a, b, ab)
	}
	item := ab.Overlaps.Items[0]
	if item.Kind != "duplicate" || item.Entry.Value != "198.51.100.8" || !slices.Equal(item.Entry.Lists, []string{"discord", "youtube"}) {
		t.Fatalf("overlap=%#v", item)
	}
	if !reflect.DeepEqual(a, read(ProfileComposition{Lists: []string{"youtube"}})) || !reflect.DeepEqual(b, read(ProfileComposition{Lists: []string{"discord"}})) {
		t.Fatal("combined preview changed an individual list")
	}
	if got := read(ProfileComposition{Categories: []string{"video", "other"}}); len(got.Overlaps.Items) != 0 {
		t.Fatal("one list selected through two categories overlaps itself")
	}
	cutoff := publication.config.Clock.Now().Add(2 * time.Hour)
	publication.config.Clock = ClockFunc(func() time.Time { return cutoff })
	expired := read(ProfileComposition{Lists: []string{"youtube", "discord"}})
	if len(expired.Overlaps.Items) != 0 || expired.ProjectedRules != 3 {
		t.Fatalf("expired evidence entered overlap/projection: %#v", expired)
	}
	if len(store.writes) != 0 || files.puts != 0 {
		t.Fatal("overlap preview wrote state or artifacts")
	}
}

// An unread or unsupported member must not blank a complete format or the
// overlap facts available for other members. Publication still refuses it.
func TestForecastRetainsKnownListsWithoutClaimingPartialCompositionFits(t *testing.T) {
	store := &forecastGuardStore{publicationFakeStore: &publicationFakeStore{}}
	publication := forecastTestService(t, store, &publicationFakeFiles{})
	missing := publication.config.Definitions["discord"]
	missing.ID = "missing"
	missing.Components = append(missing.Components, domain.ComponentDefinition{ID: "required", Required: true})
	publication.config.Definitions[missing.ID] = missing
	ctx := context.Background()
	composition := ProfileComposition{Lists: []string{"youtube", "discord", "missing"}, Priority: []string{"youtube", "discord", "missing"}}
	answer, err := publication.ForecastComposition(ctx, composition, []string{"unbounded"})
	if err != nil {
		t.Fatal(err)
	}
	got := answer[0]
	if got.Fits || !slices.Equal(got.IncompleteLists, []string{"missing"}) || got.ProjectedRules != 4 || len(got.PerList) != 2 || len(got.Overlaps.Items) != 1 {
		t.Fatalf("partial forecast lost known rules/overlaps or claimed completeness: %#v", got)
	}
	target, renderer, _ := publication.target("unbounded")
	_, _, err = publication.prepareProfile(ctx, Profile{Lists: composition.Lists}, target, renderer)
	if !errors.Is(err, ErrPartialCoverage) {
		t.Fatalf("publication accepted incomplete coverage: %v", err)
	}
	if len(store.writes) != 0 {
		t.Fatalf("preview wrote: %v", store.writes)
	}
	// The IP-only target has no usable rules when IPv4 is unsupported. Its
	// failure cannot remove the other target's complete result.
	unsupported := publication.config.Targets["keenetic"]
	unsupported.Constraints.SupportsIPv4 = false
	publication.config.Targets["keenetic"] = unsupported
	answer, err = publication.ForecastComposition(ctx, ProfileComposition{Lists: []string{"youtube", "discord"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(answer) != 2 || len(answer[0].IncompleteLists) != 2 || answer[0].Fits || !answer[1].Fits || answer[1].ProjectedRules != 4 {
		t.Fatalf("one unavailable format erased another: %#v", answer)
	}
}

func TestForecastProjectionCountsEmptyAndOverflowWithoutAllowingInvalidArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		renderer   Renderer
		limit      int
		domainOnly bool
	}{
		{"amnezia", amnezia.Renderer{}, amnezia.MaxEntries, false},
		{"mikrotik", mikrotik.Renderer{}, mikrotik.MaxLines, false},
		{"singbox", singbox.Renderer{}, singbox.MaxEntries, false},
		{"openwrt", openwrtnftset.Renderer{}, openwrtnftset.MaxLines, true},
		{"keenetic", keenetic.Renderer{}, 0, false},
		{"keenetic-dns", keeneticdns.Renderer{}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			empty := domain.RoutingPlan{}
			if count, err := tc.renderer.ProjectedRuleCount(empty); err != nil || count != 0 {
				t.Fatalf("zero count = %d, %v", count, err)
			}
			if _, err := tc.renderer.Render(empty); err == nil {
				t.Fatal("empty artifact accepted")
			}
			if tc.limit == 0 {
				return
			}
			plan := domain.RoutingPlan{}
			for i := 0; i <= tc.limit; i++ {
				var rule domain.RouteRule
				var err error
				if tc.domainOnly {
					rule, err = domain.NewDomainRule(domain.RuleDomainSuffix, fmt.Sprintf("host%d.example.com", i), "example", "web", domain.SourceManual, nil, nil)
				} else {
					rule, err = domain.NewAddrRule(netip.AddrFrom4([4]byte{198, 18, byte(i / 256), byte(i % 256)}), "example", "web", domain.SourceManual, nil, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				plan.Rules = append(plan.Rules, rule)
			}
			if count, err := tc.renderer.ProjectedRuleCount(plan); err != nil || count != tc.limit+1 {
				t.Fatalf("overflow count = %d, %v", count, err)
			}
			if _, err := tc.renderer.Render(plan); err == nil {
				t.Fatal("oversized artifact accepted")
			}
		})
	}
}
