package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

type listReadinessStore struct {
	*publicationFakeStore
	snapshot PlanningSnapshot
}

func (s *listReadinessStore) ReadPlanningSnapshot(context.Context, string, map[string]string, string, time.Time) (PlanningSnapshot, error) {
	return s.snapshot, nil
}

func TestListReadinessRequiresCurrentSuccessfulSources(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	good := []SourceRunState{
		{SourceID: "vendor", SourceRevision: strings.Repeat("a", 64), LastSuccessAt: now},
		{SourceID: "dns-main", SourceRevision: strings.Repeat("b", 64), LastSuccessAt: now},
	}
	for _, tc := range []struct {
		name       string
		health     []SourceRunState
		oldCatalog bool
		want       bool
	}{
		{"format alone is not a source read", nil, false, false},
		{"one successful source is partial", good[:1], false, false},
		{"successful empty sources are read", good, false, true},
		{"previous catalog needs refresh", good, true, false},
		{"previous source revision is not current", []SourceRunState{good[0], {SourceID: "dns-main", SourceRevision: "old", LastSuccessAt: now}}, false, false},
		{"latest failure is not success", []SourceRunState{good[0], {SourceID: "dns-main", SourceRevision: strings.Repeat("b", 64), LastSuccessAt: now.Add(-time.Hour), LastFailureAt: now}}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &publicationFakeStore{}
			service := tuningTestService(t, fake)
			raw := domain.RawJSONTargetDefinition()
			revision := strings.Repeat("c", 64)
			if tc.oldCatalog {
				revision = "previous"
			}
			store := &listReadinessStore{publicationFakeStore: fake, snapshot: PlanningSnapshot{
				Format:       FormatRecord{ListID: "example", FormatKey: raw.FormatKey, TargetID: raw.ID, RendererID: raw.RendererID, CatalogRevision: revision},
				SourceHealth: tc.health,
			}}
			service.config.Store = store
			contents, err := service.ListContents(context.Background(), "example")
			if err != nil {
				t.Fatal(err)
			}
			if contents.Observed != tc.want {
				t.Fatalf("observed=%v, want %v; health=%+v", contents.Observed, tc.want, tc.health)
			}
			if len(contents.Rows) != 2 {
				t.Fatalf("static rows lost: %+v", contents.Rows)
			}
		})
	}
}

func TestForecastTreatsPreviousCatalogAsRefreshableCoverage(t *testing.T) {
	service := forecastTestService(t, &publicationFakeStore{}, &publicationFakeFiles{})
	definition := service.config.Definitions["youtube"]
	definition.CatalogRevision = strings.Repeat("d", 64)
	service.config.Definitions["youtube"] = definition
	forecasts, err := service.ForecastComposition(context.Background(), ProfileComposition{Lists: []string{"youtube", "discord"}}, []string{"unbounded"})
	if err != nil {
		t.Fatal(err)
	}
	if len(forecasts) != 1 || !reflect.DeepEqual(forecasts[0].IncompleteLists, []string{"youtube"}) || forecasts[0].Fits || len(forecasts[0].PerList) != 1 || forecasts[0].PerList[0].ListID != "discord" {
		t.Fatalf("current list must remain calculable: %+v", forecasts)
	}
	// Control: a catalog that matches the stored observation revision remains
	// fully calculable. The persisted refresh path is checked separately.
	definition.CatalogRevision = strings.Repeat("c", 64)
	service.config.Definitions["youtube"] = definition
	forecasts, err = service.ForecastComposition(context.Background(), ProfileComposition{Lists: []string{"youtube", "discord"}}, []string{"unbounded"})
	if err != nil || len(forecasts) != 1 || len(forecasts[0].IncompleteLists) != 0 || !forecasts[0].Fits {
		t.Fatalf("recovery: %+v, %v", forecasts, err)
	}
}

func TestWrongObservationIdentityDoesNotBecomeRefreshableCoverage(t *testing.T) {
	fake := &publicationFakeStore{}
	service := tuningTestService(t, fake)
	raw := domain.RawJSONTargetDefinition()
	service.config.Store = &listReadinessStore{publicationFakeStore: fake, snapshot: PlanningSnapshot{
		Format: FormatRecord{ListID: "example", FormatKey: raw.FormatKey, TargetID: "wrong-target", RendererID: raw.RendererID, CatalogRevision: "old"},
	}}
	if _, err := service.ListContents(context.Background(), "example"); !errors.Is(err, ErrFormatMismatch) {
		t.Fatalf("contents identity error=%v", err)
	}
	_, err := service.ForecastComposition(context.Background(), ProfileComposition{Lists: []string{"example"}}, nil)
	if !errors.Is(err, ErrFormatMismatch) || errors.Is(err, ErrObservationRevisionChanged) {
		t.Fatalf("wrong identity must remain an error, got %v", err)
	}
}
