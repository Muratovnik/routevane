package application

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func archivedTestService(t *testing.T) (*PublicationService, *publicationFakeStore) {
	t.Helper()
	profile, output := testProfileAndOutput()
	store := &publicationFakeStore{profile: profile, output: output}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x52}, 256)))
	if _, err := publication.ArchiveProfile(context.Background(), profile.ID); err != nil {
		t.Fatal(err)
	}
	if !store.profile.Archived() {
		t.Fatal("archiving did not reach the store")
	}
	return publication, store
}

// The whole point of archiving is that one set of operations stops and another
// keeps working. Every write is refused through the same guard, so a profile
// added later cannot forget it.
func TestAnArchivedProfileRefusesEveryWrite(t *testing.T) {
	publication, store := archivedTestService(t)
	ctx := context.Background()
	profileID := store.profile.ID

	writes := map[string]func() error{
		"edit": func() error {
			_, err := publication.UpdateProfile(ctx, profileID, "renamed", ProfileComposition{Lists: []string{"example"}})
			return err
		},
		"schedule": func() error {
			_, err := publication.SetProfileRefreshInterval(ctx, profileID, RefreshDaily)
			return err
		},
		"refresh": func() error {
			_, err := publication.Refresh(ctx, profileID)
			return err
		},
		"add an output": func() error {
			_, err := publication.AddOutput(ctx, profileID, "keenetic")
			return err
		},
		"build": func() error {
			_, err := publication.Build(ctx, store.output.ID)
			return err
		},
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			if err := write(); !errors.Is(err, ErrProfileArchived) {
				t.Fatalf("%s on an archived profile = %v, want ErrProfileArchived", name, err)
			}
		})
	}
	if len(store.published) != 0 {
		t.Fatalf("an archived profile published %d times", len(store.published))
	}
}

// Reading is not writing. An archived profile is still fully readable, and the
// file it already published is still served: nothing was deleted (ADR 0004).
func TestAnArchivedProfileStaysReadable(t *testing.T) {
	publication, store := archivedTestService(t)
	ctx := context.Background()

	read, err := publication.Profile(ctx, store.profile.ID)
	if err != nil || !read.Archived() {
		t.Fatalf("read = %#v, err = %v", read, err)
	}
	cards, err := publication.ProfileCards(ctx)
	if err != nil || len(cards) != 1 {
		t.Fatalf("cards = %#v, err = %v", cards, err)
	}
	if cards[0].ArchivedAt.IsZero() || len(cards[0].Outputs) != 1 {
		t.Fatalf("an archived card must still state its outputs and its date: %#v", cards[0])
	}
	if len(cards[0].Resolved) == 0 {
		t.Fatal("an archived profile still resolves to what it publishes")
	}
}

// Restoring returns the profile to the shelf and to every write it refused. It
// does not rebuild: what the profile publishes is what it published when it left.
func TestRestoringReturnsTheProfileToTheShelf(t *testing.T) {
	publication, store := archivedTestService(t)
	ctx := context.Background()

	restored, err := publication.RestoreProfile(ctx, store.profile.ID)
	if err != nil || restored.Archived() {
		t.Fatalf("restored = %#v, err = %v", restored, err)
	}
	if store.profile.Archived() {
		t.Fatal("restoring did not reach the store")
	}
	if len(store.published) != 0 {
		t.Fatal("restoring must not publish anything by itself")
	}
	if _, err := publication.UpdateProfile(ctx, store.profile.ID, "renamed", ProfileComposition{Lists: []string{"example"}}); err != nil {
		t.Fatalf("a restored profile must accept an edit: %v", err)
	}
}

// Asking for the state a profile is already in is not an error: the operator asked
// for a state, not for a transition. The stored moment must not move, or the
// profile would appear to have been archived again.
func TestArchivingTwiceKeepsTheFirstMoment(t *testing.T) {
	publication, store := archivedTestService(t)
	first := store.profile.ArchivedAt

	again, err := publication.ArchiveProfile(context.Background(), store.profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !again.ArchivedAt.Equal(first) || !store.profile.ArchivedAt.Equal(first) {
		t.Fatalf("archived at %v, was %v", store.profile.ArchivedAt, first)
	}

	restored, err := publication.RestoreProfile(context.Background(), store.profile.ID)
	if err != nil || restored.Archived() {
		t.Fatalf("restored = %#v, err = %v", restored, err)
	}
	if _, err := publication.RestoreProfile(context.Background(), store.profile.ID); err != nil {
		t.Fatalf("restoring a shelved profile = %v", err)
	}
}

// The timer skips an archived profile before it judges it due, so the profile is
// never marked as a failed refresh it was never going to attempt.
func TestTheTimerSkipsAnArchivedProfileWithoutMarkingIt(t *testing.T) {
	publication, store := archivedTestService(t)
	ctx := context.Background()
	if err := publication.SetDefaultRefreshInterval(ctx, RefreshDaily); err != nil {
		t.Fatal(err)
	}

	runs, err := publication.RunDueRefreshes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("the timer touched an archived profile: %#v", runs)
	}
	if store.profile.LastRefreshFailed || !store.profile.LastRefreshedAt.IsZero() {
		t.Fatalf("the timer wrote to an archived profile: %#v", store.profile)
	}
}

// A missing profile is not an archived one, and neither is a malformed identity.
func TestArchivingAnUnknownProfileIsNotFound(t *testing.T) {
	profile, output := testProfileAndOutput()
	store := &publicationFakeStore{profile: profile, output: output}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x52}, 256)))
	if _, err := publication.ArchiveProfile(context.Background(), "not-a-profile"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archiving a malformed identity = %v", err)
	}
	if _, err := publication.RestoreProfile(context.Background(), strings.Repeat("9", 31)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restoring a malformed identity = %v", err)
	}
	if !store.profile.ArchivedAt.Equal(time.Time{}) {
		t.Fatal("a refused request still wrote to the store")
	}
}
