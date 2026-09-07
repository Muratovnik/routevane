package application

import (
	"context"
	"fmt"
	"time"
)

// Archiving takes a list off the shelf without taking anything away from the
// devices already fed by it. The published file keeps serving, the subscription
// keeps resolving, and nothing is deleted (ADR 0004): what stops is change.
//
// That is the whole invariant, and it is enforced in one place rather than
// screen by screen. An archived list that could still be edited, rescheduled,
// bound to a new format or rebuilt would let its stored composition drift away
// from the bytes its subscribers keep receiving, and the surface would have no
// honest way to say which of the two it was showing.

// Archived reports whether this list has left the shelf.
func (l Profile) Archived() bool { return !l.ArchivedAt.IsZero() }

// writable refuses every mutation an archived list must not accept. Reads are
// not routed through it: an archived list is fully readable, and its outputs
// are fully downloadable.
func (l Profile) writable() error {
	if l.Archived() {
		return ErrProfileArchived
	}
	return nil
}

// ArchiveProfile takes a list off the shelf. Archiving one that is already
// archived changes nothing and reports the list as it stands: the operator
// asked for a state, not for a transition, and refusing would only be a way of
// saying the state is already the one they wanted.
func (s *PublicationService) ArchiveProfile(ctx context.Context, id string) (Profile, error) {
	return s.setArchived(ctx, id, true)
}

// RestoreProfile puts a list back on the shelf. It does not refresh or rebuild:
// what the list publishes is what it published when it was archived, and
// changing that is the operator's next decision rather than this one's side
// effect.
func (s *PublicationService) RestoreProfile(ctx context.Context, id string) (Profile, error) {
	return s.setArchived(ctx, id, false)
}

func (s *PublicationService) setArchived(ctx context.Context, id string, archived bool) (Profile, error) {
	profile, err := s.Profile(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	if profile.Archived() == archived {
		return profile, nil
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return Profile{}, fmt.Errorf("clock returned zero time")
	}
	archivedAt := time.Time{}
	if archived {
		archivedAt = now
	}
	if err := s.config.Store.SetProfileArchived(ctx, profile.ID, archivedAt, now); err != nil {
		return Profile{}, err
	}
	profile.ArchivedAt = archivedAt
	profile.UpdatedAt = now
	return profile, nil
}
