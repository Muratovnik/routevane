package application

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Deletion is the step after archival. The service refuses a profile still on
// the shelf before anything reaches storage, and answers the moment of a
// deletion it carried out.
func TestDeleteProfileRequiresArchivalBeforeItReachesStorage(t *testing.T) {
	profile, output := testProfileAndOutput()
	store := &publicationFakeStore{profile: profile, output: output}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{1}, 64)))

	if _, err := publication.DeleteProfile(context.Background(), profile.ID); !errors.Is(err, ErrProfileNotArchived) {
		t.Fatalf("deleting a profile on the shelf err=%v", err)
	}
	if len(store.deletedProfiles) != 0 {
		t.Fatalf("a refused deletion reached storage: %v", store.deletedProfiles)
	}
	if _, err := publication.DeleteProfile(context.Background(), "not-an-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting a malformed identity err=%v", err)
	}

	if _, err := publication.ArchiveProfile(context.Background(), profile.ID); err != nil {
		t.Fatal(err)
	}
	deletedAt, err := publication.DeleteProfile(context.Background(), profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !deletedAt.Equal(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)) || len(store.deletedProfiles) != 1 || store.deletedProfiles[0] != profile.ID {
		t.Fatalf("deleted at %v, stored %v", deletedAt, store.deletedProfiles)
	}
}

// An archived profile is frozen, its formats included: neither removing one
// nor reissuing its link reaches storage.
func TestAnArchivedProfileRefusesFormatRemovalAndReissue(t *testing.T) {
	profile, output := testProfileAndOutput()
	profile.ArchivedAt = time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := &publicationFakeStore{profile: profile, output: output, tokenID: strings.Repeat("1", 32)}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{1}, 64)))

	if _, err := publication.RemoveOutput(context.Background(), output.ID); !errors.Is(err, ErrProfileArchived) {
		t.Fatalf("removing a format of an archived profile err=%v", err)
	}
	if _, err := publication.ReissueSubscription(context.Background(), output.ID); !errors.Is(err, ErrProfileArchived) {
		t.Fatalf("reissuing for an archived profile err=%v", err)
	}
	if len(store.removedOutputs) != 0 || store.tokenID != strings.Repeat("1", 32) {
		t.Fatalf("a refused retirement reached storage: removed=%v token=%q", store.removedOutputs, store.tokenID)
	}
}

// A reissued link is a fresh bearer token minted like the first one: the
// caller receives the complete token once, and storage receives only its
// identity and hash. A colliding identity is retried with new entropy.
func TestReissueReturnsTheNewTokenOnceAndStoresOnlyItsHash(t *testing.T) {
	profile, output := testProfileAndOutput()
	store := &publicationFakeStore{profile: profile, output: output, tokenID: strings.Repeat("1", 32), collisions: 1}
	entropy := append(bytes.Repeat([]byte{0x21}, 48), bytes.Repeat([]byte{0x32}, 48)...)
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(entropy))

	token, err := publication.ReissueSubscription(context.Background(), output.ID)
	if err != nil {
		t.Fatal(err)
	}
	tokenID, hash, ok := parseToken(token)
	if !ok || tokenID != strings.Repeat("32", 16) || tokenID != store.tokenID || hash != store.tokenHash {
		t.Fatalf("token=%q parsed=%q stored=%q ok=%v", token, tokenID, store.tokenID, ok)
	}
	if bytes.Contains(store.tokenHash[:], []byte(token)) {
		t.Fatal("the plaintext token reached storage")
	}

	missing := &publicationFakeStore{profile: profile, output: output}
	never := newPublicationTestService(t, missing, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{1}, 64)))
	if _, err := never.ReissueSubscription(context.Background(), output.ID); !errors.Is(err, ErrSubscriptionMissing) {
		t.Fatalf("reissuing a link that was never issued err=%v", err)
	}
}

// A profile deleted while the timer was running it has no schedule left to
// record. The tick still reports it and goes on to the next profile instead of
// abandoning every profile after it.
func TestAProfileDeletedDuringATickDoesNotStopTheTick(t *testing.T) {
	live, _ := testProfileAndOutput()
	live.RefreshInterval = RefreshDaily
	gone := live
	gone.ID = strings.Repeat("9", 32)
	gone.CreatedAt = live.CreatedAt.Add(time.Second)
	// The fake records results only for the profile it holds, exactly as the
	// store refuses a deleted one: the first profile vanished mid-tick.
	store := &publicationFakeStore{profile: live, profiles: []Profile{gone, live}}
	publication := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x41}, 256)))

	runs, err := publication.RunDueRefreshes(context.Background())
	if err != nil {
		t.Fatalf("a vanished profile stopped the tick: %v", err)
	}
	if len(runs) != 2 || runs[0].ProfileID != gone.ID || runs[1].ProfileID != live.ID {
		t.Fatalf("runs = %#v", runs)
	}
	if store.profile.LastRefreshedAt.IsZero() {
		t.Fatal("the live profile after the vanished one was not recorded")
	}
}
