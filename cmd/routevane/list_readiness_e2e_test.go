package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/catalogyaml"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
)

// Only the network is controlled: shipped definitions, refresh orchestration,
// persisted observations, contents and forecast all use the product path.
type readinessSource struct{ failID, dataID string }

func (s *readinessSource) Observe(_ context.Context, request application.SourceRequest) (application.SourceResult, error) {
	if request.SourceID == s.failID {
		return application.SourceResult{}, errors.New("feed unavailable")
	}
	if request.SourceID == s.dataID {
		resource, err := domain.NewAddrResourceFromString("198.51.100.42")
		if err != nil {
			return application.SourceResult{}, err
		}
		return application.SourceResult{Sightings: []domain.Sighting{{
			ListID: request.ListID, ComponentID: request.ComponentID, Resource: resource,
			SourceID: request.SourceID, SourceClass: request.SourceClass, SourceRevision: request.SourceRevision,
			FirstSeen: request.ObservedAt, LastSeen: request.ObservedAt, ValidUntil: request.ObservedAt.Add(time.Hour),
			ObservationCount: 1, Validity: domain.ValidityValid,
		}}}, nil
	}
	return application.SourceResult{}, nil
}

func TestShippedListReadinessAndCatalogUpgradePersisted(t *testing.T) {
	ctx := context.Background()
	catalog, err := catalogyaml.Load(ctx, filepath.Join("..", "..", "catalog"))
	if err != nil {
		t.Fatal(err)
	}
	definition := catalog.Lists["grok"]
	if len(definition.Sources) < 2 {
		t.Fatal("scenario requires Grok's multiple shipped sources")
	}
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := &readinessSource{failID: definition.Sources[1].ID, dataID: definition.Sources[0].ID}
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	target, _ := catalog.Target("keenetic")
	config := application.PublicationConfig{
		Definitions: map[string]domain.ListDefinition{"grok": definition},
		Targets:     map[string]domain.TargetDefinition{target.ID: target}, TargetRevision: catalog.TargetRevision,
		Store: store, Files: filesystem.PublishedStore{DataRoot: root},
		Renderers: application.RendererRegistry{keenetic.ID: keenetic.Renderer{}},
		Sources:   application.SourceRegistry{domain.SourceHTTP: source},
		Clock:     application.ClockFunc(func() time.Time { return now }),
	}
	service, err := application.NewPublicationService(config)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want bool, state string, extraRows int) {
		t.Helper()
		contents, readErr := service.ListContents(ctx, "grok")
		if readErr != nil || contents.Observed != want || len(contents.Rows) != len(definition.Seeds)+extraRows {
			t.Fatalf("contents=%+v err=%v", contents, readErr)
		}
		if state != "" {
			for _, item := range contents.Sources {
				if item.ID == definition.Sources[1].ID && item.State != state {
					t.Fatalf("source=%+v want=%s", item, state)
				}
			}
		}
	}
	check(false, "unread", 0)
	if _, err := service.RefreshListByID(ctx, "grok"); !errors.Is(err, application.ErrSourceFailed) {
		t.Fatalf("partial refresh=%v", err)
	}
	check(false, "failed", 1)
	source.failID = ""
	now = now.Add(time.Minute)
	if _, err := service.RefreshListByID(ctx, "grok"); err != nil {
		t.Fatal(err)
	}
	check(true, "ready", 1) // The second source is successfully empty.

	definition.CatalogRevision = strings.Repeat("d", 64)
	config.Definitions = map[string]domain.ListDefinition{"grok": definition}
	service, err = application.NewPublicationService(config)
	if err != nil {
		t.Fatal(err)
	}
	check(false, "stale", 1)
	composition := application.ProfileComposition{Lists: []string{"grok"}}
	forecasts, err := service.ForecastComposition(ctx, composition, []string{target.ID})
	if err != nil || len(forecasts) != 1 || len(forecasts[0].IncompleteLists) != 1 {
		t.Fatalf("upgrade forecast=%+v err=%v", forecasts, err)
	}
	now = now.Add(time.Minute)
	if _, err := service.RefreshListByID(ctx, "grok"); err != nil {
		t.Fatal(err)
	}
	check(true, "ready", 1)
	forecasts, err = service.ForecastComposition(ctx, composition, []string{target.ID})
	if err != nil || len(forecasts) != 1 || len(forecasts[0].IncompleteLists) != 0 {
		t.Fatalf("refreshed forecast=%+v err=%v", forecasts, err)
	}
	// The persisted success remains in history after its observations expire.
	// It must not suppress the card's next refresh or certify only seed rows.
	now = now.Add(2 * time.Hour)
	check(false, "ready", 0)
}
