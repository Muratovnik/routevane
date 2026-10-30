package application

import (
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// Freshness counts values, not observations: two sources of one list share a
// value, and a value one source dropped while another still reports it has
// not been withdrawn from the list.
func TestListFreshnessCountsDistinctValuesAcrossSources(t *testing.T) {
	t0 := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	definition := domain.ListDefinition{
		ID: "example", CatalogRevision: "catalog",
		Sources: []domain.SourceDefinition{{ID: "one", Revision: "r1"}, {ID: "two", Revision: "r2"}},
	}
	stamp := func(source, revision, value string, first, last time.Time) ObservationStamp {
		return ObservationStamp{
			SourceID: source, SourceRevision: revision, Value: ObservationValue(domain.ResourceDomain, value),
			FirstSeen: first, LastSeen: last, ValidUntil: last.Add(time.Hour),
		}
	}
	observations := ListObservations{
		CatalogRevision: "catalog",
		SourceHealth: []SourceRunState{
			{SourceID: "one", SourceRevision: "r1", LastSuccessAt: t1, PreviousSuccessAt: t0},
			{SourceID: "two", SourceRevision: "r2", LastSuccessAt: t1, PreviousSuccessAt: t0},
			// A replaced revision's history neither dates nor changes the list.
			{SourceID: "two", SourceRevision: "old", LastSuccessAt: t1.Add(time.Hour)},
		},
		Stamps: []ObservationStamp{
			// New at t1 from both sources: one added value.
			stamp("one", "r1", "new.example", t1, t1),
			stamp("two", "r2", "new.example", t1, t1),
			// New to source two, but source one had it before: not added.
			stamp("one", "r1", "known.example", t0, t1),
			stamp("two", "r2", "known.example", t1, t1),
			// Dropped by source one, still reported by source two: not removed.
			stamp("one", "r1", "kept.example", t0, t0),
			stamp("two", "r2", "kept.example", t0, t1),
			// Dropped by both: one removed value.
			stamp("one", "r1", "gone.example", t0, t0),
			stamp("two", "r2", "gone.example", t0, t0),
			// Withdrawn before the previous read: not this read's change.
			stamp("one", "r1", "older.example", t0.Add(-time.Hour), t0.Add(-time.Hour)),
			// Another revision's material is not read.
			stamp("two", "old", "elsewhere.example", t1.Add(time.Hour), t1.Add(time.Hour)),
		},
	}
	got := listFreshness(observations, definition, t1.Add(time.Second))
	if got.State != ListFreshnessFresh || got.LastReadAt == nil || !got.LastReadAt.Equal(t1) || got.Added != 1 || got.Removed != 1 {
		t.Fatalf("freshness = %+v", got)
	}
}

func TestCombinedFreshnessStateOrdersFailureBeforeAge(t *testing.T) {
	for _, testCase := range []struct {
		states []string
		want   string
	}{
		{nil, ListFreshnessNone},
		{[]string{"unread", "unread"}, ListFreshnessNever},
		{[]string{"unread", "failed"}, ListFreshnessFailed},
		{[]string{"ready", "failed"}, ListFreshnessFailed},
		{[]string{"ready", "unread"}, ListFreshnessStale},
		{[]string{"ready", "stale"}, ListFreshnessStale},
		{[]string{"ready", "ready"}, ListFreshnessFresh},
	} {
		if got := combinedFreshnessState(testCase.states); got != testCase.want {
			t.Errorf("states %v = %q, want %q", testCase.states, got, testCase.want)
		}
	}
}
