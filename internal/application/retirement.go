package application

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Retirement is the one way an object leaves Routevane for good (ADR 0051). A
// profile is deleted only after it was archived, and a format is removed only
// from a profile that is still on the shelf. Either way nothing published is
// destroyed: the files, plans and attempts stay immutable where they are, while
// every subscription of the retired formats stops working, their bindings to a
// device are cleared, and no read, timer or deployment reaches them again.
//
// Router state a retired format already delivered is not touched. Removing it
// needs the device, and the ownership ledgers that would authorize it stay
// exactly as the last successful deployment left them.

// DeleteProfile retires an archived profile and every format it still has, and
// reports the moment it did so.
func (s *PublicationService) DeleteProfile(ctx context.Context, id string) (time.Time, error) {
	profile, err := s.Profile(ctx, id)
	if err != nil {
		return time.Time{}, err
	}
	if !profile.Archived() {
		return time.Time{}, ErrProfileNotArchived
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return time.Time{}, fmt.Errorf("clock returned zero time")
	}
	if err := s.config.Store.DeleteProfile(ctx, profile.ID, now); err != nil {
		return time.Time{}, err
	}
	return now, nil
}

// RemoveOutput retires one format of a profile that is not archived, and
// reports the moment it did so.
func (s *PublicationService) RemoveOutput(ctx context.Context, id string) (time.Time, error) {
	output, err := s.writableOutput(ctx, id)
	if err != nil {
		return time.Time{}, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return time.Time{}, fmt.Errorf("clock returned zero time")
	}
	if err := s.config.Store.RemoveOutput(ctx, output.ID, now); err != nil {
		return time.Time{}, err
	}
	return now, nil
}

// ReissueSubscription replaces the subscription link of a published output.
// The previous token stops working in the same transaction that stores the new
// one, and the complete new token is returned here and nowhere else: like the
// first one, it is never stored, logged or readable again.
func (s *PublicationService) ReissueSubscription(ctx context.Context, outputID string) (string, error) {
	output, err := s.writableOutput(ctx, outputID)
	if err != nil {
		return "", err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return "", fmt.Errorf("clock returned zero time")
	}
	for attempt := 0; attempt < 8; attempt++ {
		tokenID, token, tokenHash, err := issueSubscriptionToken(s.config.Entropy)
		if err != nil {
			return "", err
		}
		if err := s.config.Store.ReissueSubscription(ctx, output.ID, tokenID, tokenHash, now); err != nil {
			if errors.Is(err, ErrIdentityCollision) {
				continue
			}
			return "", err
		}
		return token, nil
	}
	return "", ErrIdentityCollision
}
