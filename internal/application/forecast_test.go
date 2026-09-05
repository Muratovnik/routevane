package application

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
)

// forecastTestService wires the shape the defect appears in: two services whose
// planned rules overflow one device and fit another, and one address both
// services contribute, so the renderer's collapse is visible in the answer.
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
	definitions := map[string]domain.ServiceDefinition{
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
	targets := map[string]domain.TargetProfile{
		"keenetic":  {ID: "keenetic", ProfileKey: keenetic.Version, RendererID: keenetic.ID, Constraints: bounded},
		"unbounded": {ID: "unbounded", ProfileKey: keenetic.Version, RendererID: keenetic.ID, Constraints: constraints},
	}
	service, err := NewPublicationService(PublicationConfig{
		Definitions: definitions,
		Categories:  map[string]domain.CategoryDefinition{"video": {ID: "video", Title: "Видео", Services: []string{"youtube"}}},
		Targets:     targets, TargetRevision: strings.Repeat("t", 64),
		Store: store, Files: files,
		Renderers: RendererRegistry{keenetic.ID: keenetic.Renderer{}},
		Sources:   SourceRegistry{domain.SourceDNS: publicationFakeSource{}},
		Clock:     ClockFunc(func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }),
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

// The forecast answers an overflowing pair with the real numbers instead of the
// refusal a build would raise. That is the whole point: the product knows both
// counts before the list exists, and a screen that only learned "it failed"
// would still be sending the operator to the first build to find out.
func TestForecastAnswersAnOverflowingTargetWithItsNumbers(t *testing.T) {
	service := forecastTestService(t, &publicationFakeStore{}, &publicationFakeFiles{})
	forecasts, err := service.ForecastComposition(context.Background(), ListComposition{Services: []string{"youtube", "discord"}}, []string{"keenetic"})
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
	// observed address. Both services observe it, so it is one line.
	if got.ProjectedRules != 4 {
		t.Fatalf("projected = %d, want the renderer's own count of 4", got.ProjectedRules)
	}
	wantPerService := []ServiceRuleForecast{{ServiceID: "discord", Rules: 1}, {ServiceID: "youtube", Rules: 3}}
	if !reflect.DeepEqual(got.PerService, wantPerService) {
		t.Fatalf("per service = %#v, want %#v", got.PerService, wantPerService)
	}
	// Category-first priority assigns the shared address to YouTube before projection; the
	// per-service shares now describe the same finished plan the device receives.
	sum := 0
	for _, share := range got.PerService {
		sum += share.Rules
	}
	if sum != 4 || sum != got.ProjectedRules {
		t.Fatalf("per service sum = %d, projected = %d", sum, got.ProjectedRules)
	}
}

// A target that declares no rule bound always fits, and it must not be reported
// as fitting inside a maximum of zero — a screen that read a limit there would
// invent one the device never stated.
func TestForecastReportsAnUnlimitedTargetAsFitting(t *testing.T) {
	service := forecastTestService(t, &publicationFakeStore{}, &publicationFakeFiles{})
	forecasts, err := service.ForecastComposition(context.Background(), ListComposition{Services: []string{"youtube", "discord"}}, nil)
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
	service := forecastTestService(t, &publicationFakeStore{}, &publicationFakeFiles{})
	forecasts, err := service.ForecastComposition(context.Background(), ListComposition{Categories: []string{"video"}}, []string{"unbounded"})
	if err != nil {
		t.Fatal(err)
	}
	want := []ServiceRuleForecast{{ServiceID: "youtube", Rules: 3}}
	if len(forecasts) != 1 || !reflect.DeepEqual(forecasts[0].PerService, want) || forecasts[0].ProjectedRules != 3 {
		t.Fatalf("forecast = %#v", forecasts[0])
	}
	// Excluding the category's only member leaves nothing to forecast. An empty
	// answer would read as "nothing to worry about" for a list that cannot be
	// created at all, so it is refused with the same words creation uses.
	if _, err := service.ForecastComposition(context.Background(), ListComposition{Categories: []string{"video"}, Exclusions: []string{"youtube"}}, nil); err == nil {
		t.Fatal("a composition resolving to no services was forecast rather than refused")
	}
	if _, err := service.ForecastComposition(context.Background(), ListComposition{Services: []string{"absent"}}, nil); err == nil {
		t.Fatal("an unknown service was forecast rather than refused")
	}
	if _, err := service.ForecastComposition(context.Background(), ListComposition{Services: []string{"youtube"}}, []string{"absent-device"}); err == nil {
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
func (s *forecastGuardStore) CreateList(context.Context, List) error { return s.refuse("CreateList") }
func (s *forecastGuardStore) UpdateList(context.Context, List) error { return s.refuse("UpdateList") }
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
// anything, so it must leave no list, output, attempt, artifact, or profile
// behind — including on the overflowing pair, where the equivalent build would
// record a failed attempt.
func TestForecastWritesNothing(t *testing.T) {
	store := &forecastGuardStore{publicationFakeStore: &publicationFakeStore{}}
	files := &publicationFakeFiles{}
	service := forecastTestService(t, store, files)
	if _, err := service.ForecastComposition(context.Background(), ListComposition{Services: []string{"youtube", "discord"}}, nil); err != nil {
		t.Fatal(err)
	}
	if len(store.writes) != 0 {
		t.Fatalf("the forecast wrote through %v", store.writes)
	}
	if files.puts != 0 {
		t.Fatalf("the forecast stored %d artifact(s)", files.puts)
	}
	if len(store.attempts) != 0 || len(store.published) != 0 || store.list.ID != "" || store.output.ID != "" {
		t.Fatalf("the forecast left state behind: %#v", store.publicationFakeStore)
	}
	// A refused forecast writes nothing either: the build path records a failed
	// attempt against an output, and a preview has no output to record against.
	if _, err := service.ForecastComposition(context.Background(), ListComposition{Services: []string{"absent"}}, nil); err == nil {
		t.Fatal("expected the refusal")
	}
	if len(store.writes) != 0 || len(store.attempts) != 0 {
		t.Fatalf("a refused forecast wrote through %v / %#v", store.writes, store.attempts)
	}
}

func TestForecastOverlapUsesOnlyThePreparedCutoffAndDistinctLists(t *testing.T) {
	store := &forecastGuardStore{publicationFakeStore: &publicationFakeStore{}}
	files := &publicationFakeFiles{}
	service := forecastTestService(t, store, files)
	service.config.Categories["other"] = domain.CategoryDefinition{ID: "other", Services: []string{"youtube"}}
	ctx := context.Background()
	read := func(composition ListComposition) CompositionForecast {
		t.Helper()
		result, err := service.ForecastComposition(ctx, composition, []string{"unbounded"})
		if err != nil {
			t.Fatal(err)
		}
		return result[0]
	}
	a := read(ListComposition{Services: []string{"youtube"}})
	b := read(ListComposition{Services: []string{"discord"}})
	ab := read(ListComposition{Services: []string{"youtube", "discord"}, Categories: []string{"video", "other"}})
	if len(a.Overlaps.Items) != 0 || len(b.Overlaps.Items) != 0 || ab.Overlaps.Truncated || len(ab.Overlaps.Items) != 1 {
		t.Fatalf("a=%#v b=%#v ab=%#v", a, b, ab)
	}
	item := ab.Overlaps.Items[0]
	if item.Kind != "duplicate" || item.Entry.Value != "198.51.100.8" || !slices.Equal(item.Entry.Services, []string{"discord", "youtube"}) {
		t.Fatalf("overlap=%#v", item)
	}
	if !reflect.DeepEqual(a, read(ListComposition{Services: []string{"youtube"}})) || !reflect.DeepEqual(b, read(ListComposition{Services: []string{"discord"}})) {
		t.Fatal("combined preview changed an individual list")
	}
	if got := read(ListComposition{Categories: []string{"video", "other"}}); len(got.Overlaps.Items) != 0 {
		t.Fatal("one list selected through two categories overlaps itself")
	}
	cutoff := service.config.Clock.Now().Add(2 * time.Hour)
	service.config.Clock = ClockFunc(func() time.Time { return cutoff })
	expired := read(ListComposition{Services: []string{"youtube", "discord"}})
	if len(expired.Overlaps.Items) != 0 || expired.ProjectedRules != 3 {
		t.Fatalf("expired evidence entered overlap/projection: %#v", expired)
	}
	if len(store.writes) != 0 || files.puts != 0 {
		t.Fatal("overlap preview wrote state or artifacts")
	}
}
