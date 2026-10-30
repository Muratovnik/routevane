package application

import (
	"context"
	"fmt"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// List freshness answers when a list's automatic sources were last read and
// what that read changed. It is derived from the stored source cycles and
// observations, never stored itself, so the library, the list card and a
// refresh answer read the same facts through one function.
//
// Every source stamps each value a successful cycle reports with that cycle's
// time, and the store keeps the earliest first-seen and the latest last-seen
// stamp (ADR 0006). One refresh reads every enabled source of a list at one
// time. So the values a read brought are those first seen at that time, and
// the values it dropped are those its previous successful read reported that
// no source reported now.

// List freshness states. A list with no enabled automatic source has nothing
// to read; the other states describe its enabled sources together.
const (
	ListFreshnessNone   = "none"
	ListFreshnessNever  = "never"
	ListFreshnessFailed = "failed"
	ListFreshnessStale  = "stale"
	ListFreshnessFresh  = "fresh"
)

// ListFreshness is one list's latest read. LastReadAt is the latest
// successful read of any enabled source, nil when there is none. Added counts
// the distinct values the enabled sources reported for the first time at that
// read. Removed counts the distinct values a source read then had reported at
// its previous successful read and no enabled source reported at that read;
// they stay in plans until their own observations expire, so Removed says
// what the sources withdrew, not what has left the list yet. Both count the
// automatic material only, whatever the definition or a verdict says about
// the same value.
type ListFreshness struct {
	State      string     `json:"state"`
	LastReadAt *time.Time `json:"last_read_at"`
	Added      int        `json:"added"`
	Removed    int        `json:"removed"`
}

// ObservationStamp is the part of one stored sighting freshness reads. Value
// is the resource's kind and canonical value, the identity two sources share.
type ObservationStamp struct {
	SourceID       string
	SourceRevision string
	Value          string
	FirstSeen      time.Time
	LastSeen       time.Time
	ValidUntil     time.Time
	Invalid        bool
}

// ListObservations is every list's stored source history in one coherent
// read: the catalog revision its format record was refreshed for, the health
// of each source identity and the observation stamps. It is not filtered by
// the definition; the reader applies the effective source revisions.
type ListObservations struct {
	CatalogRevision string
	SourceHealth    []SourceRunState
	Stamps          []ObservationStamp
}

func snapshotObservations(snapshot PlanningSnapshot) ListObservations {
	stamps := make([]ObservationStamp, 0, len(snapshot.Sightings))
	for _, sighting := range snapshot.Sightings {
		stamps = append(stamps, ObservationStamp{
			SourceID: sighting.SourceID, SourceRevision: sighting.SourceRevision,
			Value:     ObservationValue(sighting.Resource.Kind, sighting.Resource.CanonicalValue()),
			FirstSeen: sighting.FirstSeen, LastSeen: sighting.LastSeen, ValidUntil: sighting.ValidUntil,
			Invalid: sighting.Validity == domain.ValidityInvalid,
		})
	}
	return ListObservations{CatalogRevision: snapshot.Format.CatalogRevision, SourceHealth: snapshot.SourceHealth, Stamps: stamps}
}

// ObservationValue is the identity freshness counts a value by.
func ObservationValue(kind domain.ResourceKind, canonical string) string {
	return string(kind) + "\x00" + canonical
}

// sourceHealth finds one source identity's cycles; ok is false when the
// source was never run at this revision.
func sourceHealth(observations ListObservations, sourceID, revision string) (SourceRunState, bool) {
	for _, state := range observations.SourceHealth {
		if state.SourceID == sourceID && state.SourceRevision == revision {
			return state, true
		}
	}
	return SourceRunState{}, false
}

// enabledSourceState is the card's state of one enabled source: unread,
// failed, stale or ready. A source is stale when the catalog moved past the
// list's stored format or when a value its latest successful read reported is
// no longer valid. A value an earlier read reported and a later one withdrew
// expires on its own and says nothing about the latest read. An empty
// successful read is a legitimate current read.
func enabledSourceState(observations ListObservations, definition domain.ListDefinition, sourceID string, cutoff time.Time) string {
	if observations.CatalogRevision == "" {
		return "unread"
	}
	if observations.CatalogRevision != definition.CatalogRevision {
		return "stale"
	}
	revision := sourceRevisions(definition)[sourceID]
	state, ok := sourceHealth(observations, sourceID, revision)
	if !ok {
		return "unread"
	}
	if state.LastRunFailed() {
		return "failed"
	}
	if state.LastSuccessAt.IsZero() || state.LastSuccessAt.After(cutoff) {
		return "unread"
	}
	for _, stamp := range observations.Stamps {
		if stamp.SourceID != sourceID || stamp.SourceRevision != revision || !stamp.LastSeen.Equal(state.LastSuccessAt) {
			continue
		}
		if validity := domain.LifecycleAt(stamp.ValidUntil, cutoff, stamp.Invalid); validity == domain.ValidityStale || validity == domain.ValidityArchived {
			return "stale"
		}
	}
	return "ready"
}

// listFreshness reads one list's freshness against its effective definition:
// only enabled sources at their declared revision count, so a disabled source
// or a replaced feed neither dates nor changes the list.
func listFreshness(observations ListObservations, definition domain.ListDefinition, cutoff time.Time) ListFreshness {
	freshness := ListFreshness{State: ListFreshnessNone}
	if len(definition.Sources) == 0 {
		return freshness
	}
	revisions := sourceRevisions(definition)
	states := make([]string, 0, len(definition.Sources))
	var lastRead time.Time
	for _, source := range definition.Sources {
		states = append(states, enabledSourceState(observations, definition, source.ID, cutoff))
		state, ok := sourceHealth(observations, source.ID, revisions[source.ID])
		if ok && !state.LastSuccessAt.IsZero() && !state.LastSuccessAt.After(cutoff) && state.LastSuccessAt.After(lastRead) {
			lastRead = state.LastSuccessAt
		}
	}
	freshness.State = combinedFreshnessState(states)
	if lastRead.IsZero() {
		return freshness
	}
	lastRead = lastRead.UTC()
	freshness.LastReadAt = &lastRead

	// Sources read at the latest read, with the time each was read before.
	previous := make(map[string]time.Time, len(definition.Sources))
	for _, source := range definition.Sources {
		if state, ok := sourceHealth(observations, source.ID, revisions[source.ID]); ok && state.LastSuccessAt.Equal(lastRead) {
			previous[source.ID] = state.PreviousSuccessAt
		}
	}
	firstSeen := make(map[string]time.Time)
	reportedNow := make(map[string]struct{})
	withdrawn := make(map[string]struct{})
	for _, stamp := range observations.Stamps {
		if revision, active := revisions[stamp.SourceID]; !active || revision != stamp.SourceRevision || stamp.Invalid {
			continue
		}
		if earliest, seen := firstSeen[stamp.Value]; !seen || stamp.FirstSeen.Before(earliest) {
			firstSeen[stamp.Value] = stamp.FirstSeen
		}
		if stamp.LastSeen.Equal(lastRead) {
			reportedNow[stamp.Value] = struct{}{}
		}
		if before, read := previous[stamp.SourceID]; read && !before.IsZero() && stamp.LastSeen.Equal(before) {
			withdrawn[stamp.Value] = struct{}{}
		}
	}
	for _, earliest := range firstSeen {
		if earliest.Equal(lastRead) {
			freshness.Added++
		}
	}
	for value := range withdrawn {
		if _, still := reportedNow[value]; !still {
			freshness.Removed++
		}
	}
	return freshness
}

// combinedFreshnessState folds the enabled sources' states into the list's:
// a failure is reported first, a list none of whose sources was ever read is
// never read, and a list is fresh only when every enabled source is ready —
// the same condition as the card's observed flag.
func combinedFreshnessState(states []string) string {
	if len(states) == 0 {
		return ListFreshnessNone
	}
	unread, ready := 0, 0
	for _, state := range states {
		switch state {
		case "failed":
			return ListFreshnessFailed
		case "unread":
			unread++
		case "ready":
			ready++
		}
	}
	switch {
	case unread == len(states):
		return ListFreshnessNever
	case ready == len(states):
		return ListFreshnessFresh
	default:
		return ListFreshnessStale
	}
}

// ListDetailsWithFreshness is the library listing with each list's latest
// read. Every list's history comes from one store read, however many lists
// the library holds.
func (s *PublicationService) ListDetailsWithFreshness(ctx context.Context) ([]ListDetail, error) {
	details := s.ListDetails()
	cutoff := s.config.Clock.Now().UTC()
	if cutoff.IsZero() {
		return nil, fmt.Errorf("clock returned zero time")
	}
	observations, err := s.config.Store.ReadListObservations(ctx, domain.RawJSONTargetDefinition().FormatKey)
	if err != nil {
		return nil, fmt.Errorf("read list observations: %w", err)
	}
	for i := range details {
		definition, ok := s.definition(details[i].ID)
		if !ok {
			continue
		}
		freshness := listFreshness(observations[details[i].ID], definition, cutoff)
		details[i].Freshness = &freshness
	}
	return details, nil
}
