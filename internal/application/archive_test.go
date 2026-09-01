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
	list, output := testListAndOutput()
	store := &publicationFakeStore{list: list, output: output}
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x52}, 256)))
	if _, err := service.ArchiveList(context.Background(), list.ID); err != nil {
		t.Fatal(err)
	}
	if !store.list.Archived() {
		t.Fatal("archiving did not reach the store")
	}
	return service, store
}

// The whole point of archiving is that one set of operations stops and another
// keeps working. Every write is refused through the same guard, so a route
// added later cannot forget it.
func TestAnArchivedListRefusesEveryWrite(t *testing.T) {
	service, store := archivedTestService(t)
	ctx := context.Background()
	listID := store.list.ID

	writes := map[string]func() error{
		"edit": func() error {
			_, err := service.UpdateList(ctx, listID, "renamed", ListComposition{Services: []string{"example"}})
			return err
		},
		"schedule": func() error {
			_, err := service.SetListRefreshInterval(ctx, listID, RefreshDaily)
			return err
		},
		"refresh": func() error {
			_, err := service.Refresh(ctx, listID)
			return err
		},
		"add an output": func() error {
			_, err := service.AddOutput(ctx, listID, "keenetic")
			return err
		},
		"build": func() error {
			_, err := service.Build(ctx, store.output.ID)
			return err
		},
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			if err := write(); !errors.Is(err, ErrListArchived) {
				t.Fatalf("%s on an archived list = %v, want ErrListArchived", name, err)
			}
		})
	}
	if len(store.published) != 0 {
		t.Fatalf("an archived list published %d times", len(store.published))
	}
}

// Reading is not writing. An archived list is still fully readable, and the
// file it already published is still served: nothing was deleted (ADR 0004).
func TestAnArchivedListStaysReadable(t *testing.T) {
	service, store := archivedTestService(t)
	ctx := context.Background()

	read, err := service.List(ctx, store.list.ID)
	if err != nil || !read.Archived() {
		t.Fatalf("read = %#v, err = %v", read, err)
	}
	cards, err := service.ListCards(ctx)
	if err != nil || len(cards) != 1 {
		t.Fatalf("cards = %#v, err = %v", cards, err)
	}
	if cards[0].ArchivedAt.IsZero() || len(cards[0].Outputs) != 1 {
		t.Fatalf("an archived card must still state its outputs and its date: %#v", cards[0])
	}
	if len(cards[0].Resolved) == 0 {
		t.Fatal("an archived list still resolves to what it publishes")
	}
}

// Restoring returns the list to the shelf and to every write it refused. It
// does not rebuild: what the list publishes is what it published when it left.
func TestRestoringReturnsTheListToTheShelf(t *testing.T) {
	service, store := archivedTestService(t)
	ctx := context.Background()

	restored, err := service.RestoreList(ctx, store.list.ID)
	if err != nil || restored.Archived() {
		t.Fatalf("restored = %#v, err = %v", restored, err)
	}
	if store.list.Archived() {
		t.Fatal("restoring did not reach the store")
	}
	if len(store.published) != 0 {
		t.Fatal("restoring must not publish anything by itself")
	}
	if _, err := service.UpdateList(ctx, store.list.ID, "renamed", ListComposition{Services: []string{"example"}}); err != nil {
		t.Fatalf("a restored list must accept an edit: %v", err)
	}
}

// Asking for the state a list is already in is not an error: the operator asked
// for a state, not for a transition. The stored moment must not move, or the
// list would appear to have been archived again.
func TestArchivingTwiceKeepsTheFirstMoment(t *testing.T) {
	service, store := archivedTestService(t)
	first := store.list.ArchivedAt

	again, err := service.ArchiveList(context.Background(), store.list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !again.ArchivedAt.Equal(first) || !store.list.ArchivedAt.Equal(first) {
		t.Fatalf("archived at %v, was %v", store.list.ArchivedAt, first)
	}

	restored, err := service.RestoreList(context.Background(), store.list.ID)
	if err != nil || restored.Archived() {
		t.Fatalf("restored = %#v, err = %v", restored, err)
	}
	if _, err := service.RestoreList(context.Background(), store.list.ID); err != nil {
		t.Fatalf("restoring a shelved list = %v", err)
	}
}

// The timer skips an archived list before it judges it due, so the list is
// never marked as a failed refresh it was never going to attempt.
func TestTheTimerSkipsAnArchivedListWithoutMarkingIt(t *testing.T) {
	service, store := archivedTestService(t)
	ctx := context.Background()
	if err := service.SetDefaultRefreshInterval(ctx, RefreshDaily); err != nil {
		t.Fatal(err)
	}

	runs, err := service.RunDueRefreshes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("the timer touched an archived list: %#v", runs)
	}
	if store.list.LastRefreshFailed || !store.list.LastRefreshedAt.IsZero() {
		t.Fatalf("the timer wrote to an archived list: %#v", store.list)
	}
}

// A missing list is not an archived one, and neither is a malformed identity.
func TestArchivingAnUnknownListIsNotFound(t *testing.T) {
	list, output := testListAndOutput()
	store := &publicationFakeStore{list: list, output: output}
	service := newPublicationTestService(t, store, &publicationFakeFiles{}, mutatingRenderer{}, bytes.NewReader(bytes.Repeat([]byte{0x52}, 256)))
	if _, err := service.ArchiveList(context.Background(), "not-a-list"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archiving a malformed identity = %v", err)
	}
	if _, err := service.RestoreList(context.Background(), strings.Repeat("9", 31)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restoring a malformed identity = %v", err)
	}
	if !store.list.ArchivedAt.Equal(time.Time{}) {
		t.Fatal("a refused request still wrote to the store")
	}
}
