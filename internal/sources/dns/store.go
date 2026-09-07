package dns

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

// MemoryStore is the in-memory lifecycle stage for the DNS vertical slice. It
// retains observations and relations only; accepted/rejected policy decisions
// remain local to each planner BuildPlan call.
type MemoryStore struct {
	mu        sync.RWMutex
	sightings map[string]domain.Sighting
	relations map[string]domain.Relation
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sightings: make(map[string]domain.Sighting), relations: make(map[string]domain.Relation)}
}

func (s *MemoryStore) AddSighting(value domain.Sighting) {
	if s == nil {
		return
	}
	key := sightingKey(value)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sightings == nil {
		s.sightings = make(map[string]domain.Sighting)
	}
	if prior, ok := s.sightings[key]; ok {
		if prior.FirstSeen.IsZero() || (!value.FirstSeen.IsZero() && value.FirstSeen.Before(prior.FirstSeen)) {
			prior.FirstSeen = value.FirstSeen
		}
		if value.LastSeen.After(prior.LastSeen) {
			prior.LastSeen = value.LastSeen
		}
		if value.ValidUntil.After(prior.ValidUntil) {
			prior.ValidUntil = value.ValidUntil
		}
		prior.ObservationCount += value.ObservationCount
		if prior.ObservationCount == 0 {
			prior.ObservationCount = 1
		}
		if value.Validity == domain.ValidityValid {
			prior.Validity = domain.ValidityValid
		}
		prior.ID = prior.Fingerprint()
		s.sightings[key] = prior
		return
	}
	if value.ObservationCount == 0 {
		value.ObservationCount = 1
	}
	if value.ID == "" {
		value.ID = value.Fingerprint()
	}
	s.sightings[key] = value
}

func (s *MemoryStore) AddRelation(value domain.Relation) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.relations == nil {
		s.relations = make(map[string]domain.Relation)
	}
	key := value.Fingerprint()
	if prior, ok := s.relations[key]; ok {
		if prior.FirstSeen.IsZero() || (!value.FirstSeen.IsZero() && value.FirstSeen.Before(prior.FirstSeen)) {
			prior.FirstSeen = value.FirstSeen
		}
		if value.LastSeen.After(prior.LastSeen) {
			prior.LastSeen = value.LastSeen
		}
		if value.ValidUntil.After(prior.ValidUntil) {
			prior.ValidUntil = value.ValidUntil
		}
		s.relations[key] = prior
		return
	}
	s.relations[key] = value
}

func (s *MemoryStore) Add(result []domain.Sighting, relations []domain.Relation) {
	for _, sighting := range result {
		s.AddSighting(sighting)
	}
	for _, relation := range relations {
		s.AddRelation(relation)
	}
}

// Sightings materializes validity at the supplied cutoff while preserving the
// invalid and archived lifecycle states. A previously stale sighting with a
// newly observed future validity window becomes valid again.
func (s *MemoryStore) Sightings(cutoff time.Time) []domain.Sighting {
	if s == nil {
		return []domain.Sighting{}
	}
	s.mu.RLock()
	out := make([]domain.Sighting, 0, len(s.sightings))
	for _, sighting := range s.sightings {
		copy := sighting
		if copy.IsFresh(cutoff) {
			copy.Validity = domain.ValidityValid
		} else if copy.Validity != domain.ValidityInvalid && copy.Validity != domain.ValidityArchived {
			copy.Validity = domain.ValidityStale
		}
		out = append(out, copy)
	}
	s.mu.RUnlock()
	slices.SortFunc(out, func(a, b domain.Sighting) int { return cmp.Compare(sightingKey(a), sightingKey(b)) })
	return out
}

func (s *MemoryStore) FreshSightings(cutoff time.Time) []domain.Sighting {
	all := s.Sightings(cutoff)
	out := make([]domain.Sighting, 0, len(all))
	for _, sighting := range all {
		if sighting.IsFresh(cutoff) {
			out = append(out, sighting)
		}
	}
	return out
}

func (s *MemoryStore) Relations(cutoff time.Time) []domain.Relation {
	if s == nil {
		return []domain.Relation{}
	}
	s.mu.RLock()
	out := make([]domain.Relation, 0, len(s.relations))
	for _, relation := range s.relations {
		if relation.IsFresh(cutoff) {
			out = append(out, relation)
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(out, func(a, b domain.Relation) int { return cmp.Compare(a.Fingerprint(), b.Fingerprint()) })
	return out
}

func sightingKey(value domain.Sighting) string {
	return strings.Join([]string{value.ListID, value.ComponentID, value.Resource.Kind.String(), value.Resource.CanonicalValue(), value.SourceID, value.SourceRevision}, "\x00")
}
