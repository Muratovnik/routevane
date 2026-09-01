package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

func TestPlanningSnapshotReportsSourceHealthForGraceDecisions(t *testing.T) {
	root := newDataRoot(t)
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	t0 := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	service := "example"
	dnsRevision, feedRevision := "dns-revision-1", "feed-revision-1"
	resource, err := domain.NewAddrResourceFromString("192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	profileJSON, err := json.Marshal(domain.RawJSONTargetProfile())
	if err != nil {
		t.Fatal(err)
	}
	profile := ProfileRecord{ProfileKey: "raw-v1", ServiceID: service, TargetID: "raw-json", RendererID: "raw-json", CatalogRevision: strings.Repeat("a", 64), ConfigJSON: profileJSON, UpdatedAt: t0}
	sighting := domain.Sighting{ServiceID: service, ComponentID: "web", Resource: resource, SourceID: "dns-main", SourceClass: domain.SourceObserved, SourceRevision: dnsRevision, FirstSeen: t0, LastSeen: t0, ValidUntil: t0.Add(time.Hour), ObservationCount: 1, Validity: domain.ValidityValid}
	if err := store.ApplySuccess(context.Background(), SuccessCycle{ServiceID: service, SourceID: "dns-main", SourceRevision: dnsRevision, StartedAt: t0, CompletedAt: t0, Sightings: []domain.Sighting{sighting}, Profile: &profile}); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplySuccess(context.Background(), SuccessCycle{ServiceID: service, SourceID: "official-feed", SourceRevision: feedRevision, StartedAt: t0, CompletedAt: t0}); err != nil {
		t.Fatal(err)
	}

	active := map[string]string{"dns-main": dnsRevision, "official-feed": feedRevision}
	snapshot, err := store.ReadPlanningSnapshot(context.Background(), service, active, "raw-v1", t0)
	if err != nil {
		t.Fatal(err)
	}
	health := healthByID(t, snapshot.SourceHealth)
	if len(health) != 2 {
		t.Fatalf("health = %#v", snapshot.SourceHealth)
	}
	for id, state := range health {
		if !state.LastSuccessAt.Equal(t0) || !state.LastFailureAt.IsZero() || state.LastRunFailed() {
			t.Fatalf("healthy source %q = %#v", id, state)
		}
	}
	if len(application.DegradedSources(snapshot.SourceHealth, active, t0)) != 0 {
		t.Fatal("a healthy source must not be degraded")
	}

	failedAt := t0.Add(2 * time.Hour)
	if err := store.RecordFailure(context.Background(), FailureCycle{ServiceID: service, SourceID: "official-feed", SourceRevision: feedRevision, StartedAt: failedAt, CompletedAt: failedAt, ErrorCode: application.SourceErrorCode(domain.SourceHTTP)}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = store.ReadPlanningSnapshot(context.Background(), service, active, "raw-v1", failedAt)
	if err != nil {
		t.Fatal(err)
	}
	health = healthByID(t, snapshot.SourceHealth)
	feed := health["official-feed"]
	if !feed.LastSuccessAt.Equal(t0) || !feed.LastFailureAt.Equal(failedAt) || !feed.LastRunFailed() {
		t.Fatalf("failed source = %#v", feed)
	}
	if health["dns-main"].LastRunFailed() {
		t.Fatalf("one failing source must not mark another degraded: %#v", health["dns-main"])
	}
	degraded := application.DegradedSources(snapshot.SourceHealth, active, failedAt)
	if len(degraded) != 1 || degraded[0].SourceID != "official-feed" {
		t.Fatalf("degraded = %#v", degraded)
	}
	if !degraded[0].GraceUntil.Equal(t0.Add(application.SourceGracePeriod)) {
		t.Fatalf("grace window = %v", degraded[0].GraceUntil)
	}

	// After the window closes the source is no longer degraded, so its expired
	// observations stop being routable instead of surviving indefinitely.
	afterWindow := t0.Add(application.SourceGracePeriod + time.Minute)
	snapshot, err = store.ReadPlanningSnapshot(context.Background(), service, active, "raw-v1", afterWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(application.DegradedSources(snapshot.SourceHealth, active, afterWindow)) != 0 {
		t.Fatal("grace must expire with the window")
	}

	// A recovered cycle clears the degraded state without deleting history.
	recoveredAt := failedAt.Add(time.Hour)
	if err := store.ApplySuccess(context.Background(), SuccessCycle{ServiceID: service, SourceID: "official-feed", SourceRevision: feedRevision, StartedAt: recoveredAt, CompletedAt: recoveredAt}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = store.ReadPlanningSnapshot(context.Background(), service, active, "raw-v1", recoveredAt)
	if err != nil {
		t.Fatal(err)
	}
	health = healthByID(t, snapshot.SourceHealth)
	if health["official-feed"].LastRunFailed() || !health["official-feed"].LastFailureAt.Equal(failedAt) {
		t.Fatalf("recovered source = %#v", health["official-feed"])
	}
	if len(application.DegradedSources(snapshot.SourceHealth, active, recoveredAt)) != 0 {
		t.Fatal("a recovered source must not stay degraded")
	}
}

func TestPlanningSnapshotSeparatesHealthPerSourceRevision(t *testing.T) {
	root := newDataRoot(t)
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	t0 := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	profileJSON, err := json.Marshal(domain.RawJSONTargetProfile())
	if err != nil {
		t.Fatal(err)
	}
	profile := ProfileRecord{ProfileKey: "raw-v1", ServiceID: "example", TargetID: "raw-json", RendererID: "raw-json", CatalogRevision: strings.Repeat("a", 64), ConfigJSON: profileJSON, UpdatedAt: t0}
	if err := store.ApplySuccess(context.Background(), SuccessCycle{ServiceID: "example", SourceID: "official-feed", SourceRevision: "old", StartedAt: t0, CompletedAt: t0, Profile: &profile}); err != nil {
		t.Fatal(err)
	}
	failedAt := t0.Add(time.Hour)
	if err := store.RecordFailure(context.Background(), FailureCycle{ServiceID: "example", SourceID: "official-feed", SourceRevision: "new", StartedAt: failedAt, CompletedAt: failedAt, ErrorCode: "http_feed_failed"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ReadPlanningSnapshot(context.Background(), "example", map[string]string{"official-feed": "new"}, "raw-v1", failedAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.SourceHealth) != 2 {
		t.Fatalf("each revision keeps its own row: %#v", snapshot.SourceHealth)
	}
	// The active revision never succeeded, so nothing is being kept alive and
	// the superseded revision's success cannot open a grace window for it.
	degraded := application.DegradedSources(snapshot.SourceHealth, map[string]string{"official-feed": "new"}, failedAt)
	if len(degraded) != 0 {
		t.Fatalf("degraded = %#v", degraded)
	}
}

func healthByID(t *testing.T, states []SourceRunState) map[string]SourceRunState {
	t.Helper()
	result := make(map[string]SourceRunState, len(states))
	for _, state := range states {
		if _, duplicate := result[state.SourceID]; duplicate {
			t.Fatalf("duplicate health row for %q", state.SourceID)
		}
		result[state.SourceID] = state
	}
	return result
}
