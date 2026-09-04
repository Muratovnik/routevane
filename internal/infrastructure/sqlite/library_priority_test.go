package sqlite

import (
	"context"
	"reflect"
	"testing"
)

func TestLibraryPriorityRoundTripsAndRejectsMalformedReplacement(t *testing.T) {
	store := categoryTestStore(t)
	ctx := context.Background()
	want := []string{"youtube", "example", "telegram"}
	if err := store.SetDefaultPriority(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := store.DefaultPriority(ctx)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("priority=%#v err=%v", got, err)
	}
	if err := store.SetDefaultPriority(ctx, []string{"youtube", "youtube"}); err == nil {
		t.Fatal("duplicate library priority accepted")
	}
	got, err = store.DefaultPriority(ctx)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("malformed replacement changed stored order=%#v err=%v", got, err)
	}
	if err := store.SetDefaultPriority(ctx, []string{"bad id"}); err == nil {
		t.Fatal("invalid service id accepted")
	}
	// Empty is a valid sparse store operation (the application enforces a full
	// permutation); this boundary may still clear stale rows during import.
	if err := store.SetDefaultPriority(ctx, nil); err != nil {
		t.Fatal("empty library priority replacement failed")
	}
}

func TestLibraryPrioritySchemaExistsAfterFreshMigration(t *testing.T) {
	store := categoryTestStore(t)
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='library_service_priorities'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("library priority table count=%d", count)
	}
}
