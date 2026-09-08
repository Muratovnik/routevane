package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
	"github.com/Muratovnik/routevane/internal/renderers/singbox"
)

type seedOnlyStore struct{ *publicationFakeStore }

func (s *seedOnlyStore) ReadPlanningSnapshot(_ context.Context, listID string, _ map[string]string, _ string, _ time.Time) (PlanningSnapshot, error) {
	raw := domain.RawJSONTargetDefinition()
	return PlanningSnapshot{Format: FormatRecord{
		FormatKey: raw.FormatKey, ListID: listID, TargetID: raw.ID,
		RendererID: raw.RendererID, CatalogRevision: strings.Repeat("c", 64),
	}}, nil
}

// A renderer limit applies to the plan after profile priority removes a lower
// rule wholly covered by a higher one. Forecast and publication therefore make
// the same decision, while a genuinely disjoint pair is still refused before
// an artifact can replace the previous valid one.
func TestProfileBuildAppliesRuleLimitAfterPriorityOmission(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	profile := Profile{
		ID: strings.Repeat("1", 32), Name: "priority", Lists: []string{"broad", "host"},
		Priority: []string{"broad", "host"}, CreatedAt: now, UpdatedAt: now,
	}
	output := Output{
		ID: strings.Repeat("2", 32), ProfileID: profile.ID, TargetID: "bounded",
		FormatKey: keenetic.Version, RendererID: keenetic.ID, RendererVersion: keenetic.Version,
		TargetRevision: strings.Repeat("t", 64), CreatedAt: now,
	}
	base := &publicationFakeStore{profile: profile, output: output}
	store := &seedOnlyStore{publicationFakeStore: base}
	files := &publicationFakeFiles{}
	revision := strings.Repeat("c", 64)
	definitions := map[string]domain.ListDefinition{
		"broad": seedDefinition("broad", domain.RulePrefix4, "8.8.8.0/24", revision),
		"host":  seedDefinition("host", domain.RuleIPv4, "8.8.8.8", revision),
	}
	target := domain.TargetDefinition{ID: "bounded-keenetic", FormatKey: keenetic.Version, RendererID: keenetic.ID, Constraints: domain.TargetConstraints{
		SupportsIPv4: true, SupportsPrefixes: true, MaxRules: 1, MaxArtifactSize: keenetic.MaxArtifactSize,
	}}
	singboxTarget := domain.TargetDefinition{ID: "bounded-singbox", FormatKey: singbox.Version, RendererID: singbox.ID, Constraints: domain.TargetConstraints{
		SupportsIPv4: true, SupportsPrefixes: true, MaxRules: 1, MaxArtifactSize: singbox.MaxArtifactSize,
	}}
	output.TargetID = target.ID
	base.output = output
	publication, err := NewPublicationService(PublicationConfig{
		Definitions: definitions, Targets: map[string]domain.TargetDefinition{target.ID: target, singboxTarget.ID: singboxTarget},
		TargetRevision: strings.Repeat("t", 64), Store: store, Files: files,
		Renderers: RendererRegistry{keenetic.ID: keenetic.Renderer{}, singbox.ID: singbox.Renderer{}},
		Sources:   SourceRegistry{domain.SourceDNS: publicationFakeSource{}},
		Clock:     ClockFunc(func() time.Time { return now }), Entropy: bytes.NewReader(bytes.Repeat([]byte{0x51}, 128)),
	})
	if err != nil {
		t.Fatal(err)
	}

	forecast, err := publication.ForecastComposition(context.Background(), ProfileComposition{
		Lists: profile.Lists, Priority: profile.Priority,
	}, []string{target.ID, singboxTarget.ID})
	if err != nil || len(forecast) != 2 {
		t.Fatalf("covered forecast = %#v, err = %v", forecast, err)
	}
	for _, item := range forecast {
		if !item.Fits || item.ProjectedRules != 1 {
			t.Fatalf("covered forecast = %#v", forecast)
		}
	}
	built, err := publication.Build(context.Background(), output.ID)
	if err != nil || built.Summary.RuleCount != 1 || len(base.published) != 1 {
		t.Fatalf("covered build = %#v, err = %v, publications = %d", built, err, len(base.published))
	}
	previousArtifact := base.output.LatestArtifactID
	previousWrites := files.puts

	disjoint := definitions["host"]
	disjoint.Seeds[0].Value = "9.9.9.9"
	publication.config.Definitions["host"] = disjoint
	forecast, err = publication.ForecastComposition(context.Background(), ProfileComposition{
		Lists: profile.Lists, Priority: profile.Priority,
	}, []string{target.ID, singboxTarget.ID})
	if err != nil || len(forecast) != 2 {
		t.Fatalf("overflow forecast = %#v, err = %v", forecast, err)
	}
	for _, item := range forecast {
		if item.Fits || item.ProjectedRules != 2 {
			t.Fatalf("overflow forecast = %#v", forecast)
		}
	}
	if _, err := publication.Build(context.Background(), output.ID); !errors.Is(err, ErrRuleLimit) {
		t.Fatalf("overflow build error = %v", err)
	}
	if base.output.LatestArtifactID != previousArtifact || files.puts != previousWrites || len(base.published) != 1 {
		t.Fatalf("failed candidate replaced previous artifact: output=%#v writes=%d publications=%d", base.output, files.puts, len(base.published))
	}
}

func seedDefinition(id string, kind domain.RuleKind, value, revision string) domain.ListDefinition {
	return domain.ListDefinition{
		ID: id, Title: id, CatalogRevision: revision,
		Components: []domain.ComponentDefinition{{ID: "main", Required: true}},
		Seeds:      []domain.Seed{{Kind: kind, Value: value, ComponentID: "main", SourceID: "manual:seed", SourceClass: domain.SourceManual}},
	}
}

type scheduleSourceFunc func(context.Context, SourceRequest) (SourceResult, error)

func (f scheduleSourceFunc) Observe(ctx context.Context, request SourceRequest) (SourceResult, error) {
	return f(ctx, request)
}

// The shelf remains capped for its current transport, but integrity and timer
// consumers enumerate their own complete populations. The oldest profile is
// deliberately the 201st row the shelf would omit.
func TestIntegrityAndSchedulingDoNotReuseTheProfileShelfPage(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	oldest := Profile{
		ID: fmt.Sprintf("%032x", 1), Name: "oldest", Lists: []string{"example"}, Categories: []string{"video"},
		RefreshInterval: RefreshDaily, CreatedAt: now, UpdatedAt: now,
	}
	all := []Profile{oldest}
	for index := 2; index <= 201; index++ {
		all = append(all, Profile{
			ID: fmt.Sprintf("%032x", index), Name: fmt.Sprintf("profile-%d", index), Lists: []string{"example"},
			RefreshInterval: RefreshOff, CreatedAt: now.Add(time.Duration(index) * time.Minute), UpdatedAt: now,
		})
	}
	store := &publicationFakeStore{profile: oldest, profiles: all}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, keenetic.Renderer{}, bytes.NewReader(bytes.Repeat([]byte{0x61}, 128)))
	revision := publication.config.Definitions["example"].CatalogRevision
	publication.config.Definitions["unused"] = seedDefinition("unused", domain.RuleIPv4, "9.9.9.9", revision)
	publication.config.Categories = map[string]domain.CategoryDefinition{
		"video": {ID: "video", Title: "Video", Lists: []string{"example"}},
		"spare": {ID: "spare", Title: "Spare", Lists: []string{"unused"}},
	}

	var inUse ListInUseError
	if err := publication.RemoveList(context.Background(), "example"); !errors.As(err, &inUse) {
		t.Fatalf("old reference did not protect list: %v", err)
	}
	if len(inUse.Profiles) != 201 || inUse.Profiles[0].ID != oldest.ID {
		t.Fatalf("references = %d, first = %#v", len(inUse.Profiles), inUse.Profiles[0])
	}
	var categoryInUse CategoryInUseError
	if err := publication.RemoveCategory(context.Background(), "video", CategoryListsDetach); !errors.As(err, &categoryInUse) {
		t.Fatalf("old reference did not protect category: %v", err)
	}
	if len(categoryInUse.Profiles) != 1 || categoryInUse.Profiles[0].ID != oldest.ID {
		t.Fatalf("category references = %#v", categoryInUse.Profiles)
	}
	if err := publication.RemoveCategory(context.Background(), "spare", CategoryListsDetach); err != nil {
		t.Fatalf("unused category could not be removed: %v", err)
	}
	if err := publication.RemoveList(context.Background(), "unused"); err != nil {
		t.Fatalf("unused list could not be removed: %v", err)
	}
	runs, err := publication.RunDueRefreshes(context.Background())
	if err != nil || len(runs) != 1 || runs[0].ProfileID != oldest.ID {
		t.Fatalf("scheduled runs = %#v, err = %v", runs, err)
	}
}

func TestHeldScheduledRefreshPreservesNewerWeeklyAndDailyIntervals(t *testing.T) {
	tests := []struct {
		name       string
		captured   RefreshInterval
		operator   RefreshInterval
		observeErr error
		cancel     bool
	}{
		{name: "failure keeps weekly", captured: RefreshDaily, operator: RefreshWeekly, observeErr: errors.New("source failed")},
		{name: "cancellation keeps daily", captured: RefreshWeekly, operator: RefreshDaily, cancel: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile, _ := testProfileAndOutput()
			profile.RefreshInterval = test.captured
			store := &publicationFakeStore{profile: profile}
			publication := newPublicationTestService(t, store, &publicationFakeFiles{}, keenetic.Renderer{}, bytes.NewReader(bytes.Repeat([]byte{0x72}, 128)))
			definition := publication.config.Definitions["example"]
			definition.Sources = []domain.SourceDefinition{{ID: "held", Type: domain.SourceDNS, ComponentID: "web", Revision: "v1"}}
			publication.config.Definitions["example"] = definition
			started := make(chan struct{})
			release := make(chan struct{})
			publication.config.Sources[domain.SourceDNS] = scheduleSourceFunc(func(ctx context.Context, _ SourceRequest) (SourceResult, error) {
				close(started)
				<-release
				if test.cancel {
					return SourceResult{}, ctx.Err()
				}
				return SourceResult{}, test.observeErr
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type result struct {
				runs []ScheduledRun
				err  error
			}
			finished := make(chan result, 1)
			go func() {
				runs, err := publication.RunDueRefreshes(ctx)
				finished <- result{runs: runs, err: err}
			}()
			<-started
			updated, err := publication.SetProfileRefreshInterval(context.Background(), profile.ID, test.operator)
			if err != nil || updated.RefreshInterval != test.operator {
				close(release)
				t.Fatalf("operator update = %#v, err = %v", updated, err)
			}
			if test.cancel {
				cancel()
			}
			close(release)
			got := <-finished
			if test.cancel {
				if !errors.Is(got.err, context.Canceled) {
					t.Fatalf("cancelled schedule error = %v", got.err)
				}
			} else if got.err != nil || len(got.runs) != 1 || !got.runs[0].Failed() {
				t.Fatalf("failed schedule = %#v, err = %v", got.runs, got.err)
			}
			if store.profile.RefreshInterval != test.operator {
				t.Fatalf("operator interval was restored: %#v", store.profile)
			}
			if !test.cancel {
				if store.profile.LastRefreshedAt.IsZero() || !store.profile.LastRefreshFailed {
					t.Fatalf("failure metadata = %#v", store.profile)
				}
				again, err := publication.RunDueRefreshes(context.Background())
				if err != nil || len(again) != 0 {
					t.Fatalf("weekly interval did not control next run: %#v, err = %v", again, err)
				}
			}
		})
	}
}

type sourceFunc func(context.Context, SourceRequest) (SourceResult, error)

func (f sourceFunc) Observe(ctx context.Context, request SourceRequest) (SourceResult, error) {
	return f(ctx, request)
}

// Source completion owns only run metadata. An operator preference written
// while Observe is active remains authoritative after that run completes.
func TestScheduledCompletionPreservesAnIntervalChangedDuringRefresh(t *testing.T) {
	profile, _ := testProfileAndOutput()
	profile.RefreshInterval = RefreshDaily
	store := &publicationFakeStore{profile: profile}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, keenetic.Renderer{}, bytes.NewReader(bytes.Repeat([]byte{0x62}, 128)))
	definition := publication.config.Definitions["example"]
	definition.Sources = []domain.SourceDefinition{{ID: "dns-main", Type: domain.SourceDNS, ComponentID: "web", Revision: "v1"}}
	publication.config.Definitions["example"] = definition
	changed := false
	publication.config.Sources[domain.SourceDNS] = sourceFunc(func(_ context.Context, _ SourceRequest) (SourceResult, error) {
		updated, err := publication.SetProfileRefreshInterval(context.Background(), profile.ID, RefreshOff)
		if err != nil {
			return SourceResult{}, err
		}
		changed = updated.RefreshInterval == RefreshOff
		return SourceResult{}, nil
	})

	runs, err := publication.RunDueRefreshes(context.Background())
	if err != nil || len(runs) != 1 || runs[0].Failed() || !changed {
		t.Fatalf("runs = %#v, changed = %v, err = %v", runs, changed, err)
	}
	if store.profile.RefreshInterval != RefreshOff || store.profile.LastRefreshedAt.IsZero() || store.profile.LastRefreshFailed {
		t.Fatalf("scheduled completion rewrote operator state: %#v", store.profile)
	}
	next, err := publication.RunDueRefreshes(context.Background())
	if err != nil || len(next) != 0 {
		t.Fatalf("disabled profile ran again: runs=%#v err=%v", next, err)
	}
}
