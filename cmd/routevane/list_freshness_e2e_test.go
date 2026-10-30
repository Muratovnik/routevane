package main

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
	"github.com/Muratovnik/routevane/internal/renderers/rawjson"
)

// freshnessFeed is the only fake: it answers the domains it is told to, or
// fails, stamping every value with the cycle's own time like the real sources.
type freshnessFeed struct {
	domains []string
	fail    bool
}

func (f *freshnessFeed) Observe(_ context.Context, request application.SourceRequest) (application.SourceResult, error) {
	if f.fail {
		return application.SourceResult{}, errors.New("feed unavailable")
	}
	sightings := make([]domain.Sighting, 0, len(f.domains))
	for _, value := range f.domains {
		resource, err := domain.NewDomainResource(value)
		if err != nil {
			return application.SourceResult{}, err
		}
		sightings = append(sightings, domain.Sighting{
			ListID: request.ListID, ComponentID: request.ComponentID, Resource: resource,
			SourceID: request.SourceID, SourceClass: request.SourceClass, SourceRevision: request.SourceRevision,
			FirstSeen: request.ObservedAt, LastSeen: request.ObservedAt, ValidUntil: request.ObservedAt.Add(time.Hour),
			ObservationCount: 1, Validity: domain.ValidityValid,
		})
	}
	return application.SourceResult{Sightings: sightings}, nil
}

// TestListFreshnessReportsWhenSourcesWereReadAndWhatTheyChanged drives the
// product path — refresh orchestration, the SQLite store, the contents card,
// the library listing and the refresh answer — with a controlled clock and a
// fake feed, and holds all three to one account of each read.
func TestListFreshnessReportsWhenSourcesWereReadAndWhatTheyChanged(t *testing.T) {
	ctx := context.Background()
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	revision := strings.Repeat("c", 64)
	fed := domain.ListDefinition{
		ID: "fed", Title: "Fed", CatalogRevision: revision,
		Components: []domain.ComponentDefinition{{ID: "web"}},
		Sources: []domain.SourceDefinition{{
			ID: "vendor", Type: domain.SourceHTTP, ComponentID: "web", URL: "https://feeds.example/list.txt",
			Format: domain.FeedFormatText, Class: domain.SourceCommunity, Revision: strings.Repeat("a", 64),
		}},
	}
	plain := domain.ListDefinition{
		ID: "plain", Title: "Plain", CatalogRevision: revision,
		Components: []domain.ComponentDefinition{{ID: "web"}},
		Seeds:      []domain.Seed{{Kind: domain.RuleDomainSuffix, Value: "plain.example", ComponentID: "web", SourceID: "catalog:plain", SourceClass: domain.SourceManual}},
	}
	feed := &freshnessFeed{}
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	target := domain.RawJSONTargetDefinition()
	service, err := application.NewPublicationService(application.PublicationConfig{
		Definitions: map[string]domain.ListDefinition{fed.ID: fed, plain.ID: plain},
		Targets:     map[string]domain.TargetDefinition{target.ID: target},
		Store:       store, Files: filesystem.PublishedStore{DataRoot: root},
		Renderers: application.RendererRegistry{rawjson.ID: rawjson.Renderer{}},
		Sources:   application.SourceRegistry{domain.SourceHTTP: feed},
		Clock:     application.ClockFunc(func() time.Time { return now }),
	})
	if err != nil {
		t.Fatal(err)
	}
	at := func(value time.Time) *time.Time { return &value }

	// read checks the card and the library against one expected account, and
	// returns the card's only source for the history fields.
	read := func(step string, want map[string]application.ListFreshness) application.ListContentsSource {
		t.Helper()
		details, err := service.ListDetailsWithFreshness(ctx)
		if err != nil {
			t.Fatalf("%s: listing: %v", step, err)
		}
		for _, detail := range details {
			expected, named := want[detail.ID]
			if !named {
				continue
			}
			if detail.Freshness == nil || !reflect.DeepEqual(*detail.Freshness, expected) {
				t.Fatalf("%s: listing freshness of %q = %+v, want %+v", step, detail.ID, detail.Freshness, expected)
			}
			contents, err := service.ListContents(ctx, detail.ID)
			if err != nil {
				t.Fatalf("%s: contents of %q: %v", step, detail.ID, err)
			}
			if !reflect.DeepEqual(contents.Freshness, expected) {
				t.Fatalf("%s: card freshness of %q = %+v, want %+v", step, detail.ID, contents.Freshness, expected)
			}
		}
		contents, err := service.ListContents(ctx, fed.ID)
		if err != nil || len(contents.Sources) != 1 {
			t.Fatalf("%s: contents=%+v err=%v", step, contents, err)
		}
		return contents.Sources[0]
	}
	refresh := func(step string, wantAdded, wantRemoved int) {
		t.Helper()
		answer, err := service.RefreshListByID(ctx, fed.ID)
		if err != nil {
			t.Fatalf("%s: refresh: %v", step, err)
		}
		if answer.Added != wantAdded || answer.Removed != wantRemoved {
			t.Fatalf("%s: refresh changed +%d/-%d, want +%d/-%d", step, answer.Added, answer.Removed, wantAdded, wantRemoved)
		}
	}
	none := application.ListFreshness{State: application.ListFreshnessNone}

	// Never read: nothing dates the list, and a list without automatic
	// sources has nothing to read at all.
	source := read("never", map[string]application.ListFreshness{
		fed.ID: {State: application.ListFreshnessNever}, plain.ID: none,
	})
	if source.LastSuccessAt != nil || source.LastFailureAt != nil || source.LastError != "" {
		t.Fatalf("never-read source = %+v", source)
	}

	// The first read brings everything it reports.
	t1 := now
	feed.domains = []string{"a.example", "b.example"}
	refresh("first read", 2, 0)
	source = read("first read", map[string]application.ListFreshness{
		fed.ID: {State: application.ListFreshnessFresh, LastReadAt: at(t1), Added: 2}, plain.ID: none,
	})
	if !reflect.DeepEqual(source.LastSuccessAt, at(t1)) || source.LastFailureAt != nil {
		t.Fatalf("first-read source = %+v", source)
	}

	// A read that returns the same set changes nothing.
	now = now.Add(time.Minute)
	t2 := now
	refresh("same read", 0, 0)
	read("same read", map[string]application.ListFreshness{fed.ID: {State: application.ListFreshnessFresh, LastReadAt: at(t2)}})

	// A new value is added once; the value the feed stopped listing is
	// counted as removed even though its observation is still valid.
	now = now.Add(time.Minute)
	t3 := now
	feed.domains = []string{"a.example", "c.example"}
	refresh("changed read", 1, 1)
	read("changed read", map[string]application.ListFreshness{
		fed.ID: {State: application.ListFreshnessFresh, LastReadAt: at(t3), Added: 1, Removed: 1},
	})

	// A failure after success: the list reports the failure, keeps the date
	// and change of its last successful read, and the refresh itself read
	// nothing new.
	now = now.Add(time.Minute)
	t4 := now
	feed.fail = true
	refresh("failed read", 0, 0)
	source = read("failed read", map[string]application.ListFreshness{
		fed.ID: {State: application.ListFreshnessFailed, LastReadAt: at(t3), Added: 1, Removed: 1},
	})
	if !reflect.DeepEqual(source.LastSuccessAt, at(t3)) || !reflect.DeepEqual(source.LastFailureAt, at(t4)) || source.LastError != "http_feed_failed" || source.State != "failed" {
		t.Fatalf("failed source = %+v", source)
	}

	// Recovery past the validity of the withdrawn value: b.example's
	// observation expired, but the latest read is current, so the list is
	// fresh rather than stale. The failed cycle is not a read, so the change
	// is measured against the read before it.
	now = t2.Add(2 * time.Hour)
	t5 := now
	feed.fail = false
	refresh("recovered read", 0, 0)
	source = read("recovered read", map[string]application.ListFreshness{
		fed.ID: {State: application.ListFreshnessFresh, LastReadAt: at(t5)},
	})
	if source.State != "ready" || !reflect.DeepEqual(source.LastFailureAt, at(t4)) {
		t.Fatalf("recovered source = %+v", source)
	}

	// Unrefreshed past the validity of what the latest read reported: stale.
	now = t5.Add(2 * time.Hour)
	read("expired read", map[string]application.ListFreshness{
		fed.ID: {State: application.ListFreshnessStale, LastReadAt: at(t5)},
	})

	// Switching the only source off leaves nothing automatic to date.
	if err := service.SetListSourceEnabled(ctx, fed.ID, "vendor", false); err != nil {
		t.Fatal(err)
	}
	read("source off", map[string]application.ListFreshness{fed.ID: none})
}
