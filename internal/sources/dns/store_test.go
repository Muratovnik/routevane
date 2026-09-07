package dns

import (
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

func TestMemoryStoreLifecycleAndFreshness(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	resource, _ := domain.NewAddrResourceFromString("192.0.2.1")
	store := NewMemoryStore()
	store.AddSighting(domain.Sighting{ListID: "example", ComponentID: "web", Resource: resource, SourceID: "dns", SourceRevision: "v1", FirstSeen: now.Add(-time.Hour), LastSeen: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour), Validity: domain.ValidityValid, ObservationCount: 1})
	store.AddSighting(domain.Sighting{ListID: "example", ComponentID: "web", Resource: resource, SourceID: "dns", SourceRevision: "v1", FirstSeen: now, LastSeen: now, ValidUntil: now.Add(2 * time.Hour), Validity: domain.ValidityValid, ObservationCount: 1})
	fresh := store.FreshSightings(now.Add(time.Hour))
	if len(fresh) != 1 || fresh[0].FirstSeen != now.Add(-time.Hour) || fresh[0].ObservationCount != 2 {
		t.Fatalf("fresh = %#v", fresh)
	}
	expired := store.Sightings(now.Add(2 * time.Hour))
	if len(expired) != 1 || expired[0].Validity != domain.ValidityStale || len(store.FreshSightings(now.Add(2*time.Hour))) != 0 {
		t.Fatalf("expiry equality did not become stale: %#v", expired)
	}

	reappeared := NewMemoryStore()
	reappeared.AddSighting(domain.Sighting{ListID: "example", ComponentID: "web", Resource: resource, SourceID: "dns", SourceRevision: "v2", FirstSeen: now, LastSeen: now, ValidUntil: now, Validity: domain.ValidityStale, ObservationCount: 1})
	reappeared.AddSighting(domain.Sighting{ListID: "example", ComponentID: "web", Resource: resource, SourceID: "dns", SourceRevision: "v2", FirstSeen: now, LastSeen: now.Add(time.Minute), ValidUntil: now.Add(time.Hour), Validity: domain.ValidityStale, ObservationCount: 1})
	if got := reappeared.FreshSightings(now); len(got) != 1 || got[0].Validity != domain.ValidityValid {
		t.Fatalf("stale re-observation did not become valid: %#v", got)
	}
}
