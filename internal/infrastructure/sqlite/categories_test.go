package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

func categoryTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// A category the operator created survives the round trip with its membership,
// is renamed without losing it, and takes the membership with it when deleted:
// a surviving verdict row would attach itself to whatever later took the id.
func TestCustomCategoriesAndTheirMembershipSurviveWriteReadRenameAndDeletion(t *testing.T) {
	store := categoryTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	category := application.CustomCategory{ID: "custom-1234567890abcdef", Title: "Мои списки", CreatedAt: now, UpdatedAt: now}
	memberships := []application.CategoryMembership{
		{CategoryID: category.ID, ListID: "custom-fedcba0987654321", State: application.MembershipAdded, UpdatedAt: now},
		{CategoryID: category.ID, ListID: "youtube", State: application.MembershipAdded, UpdatedAt: now},
	}
	if err := store.CreateCustomCategory(ctx, category, memberships); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateCustomCategory(ctx, category, nil); !errors.Is(err, application.ErrIdentityCollision) {
		t.Fatalf("duplicate err = %v", err)
	}
	overlay, err := store.CategoryOverlay(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(overlay.Categories, []application.CustomCategory{category}) {
		t.Fatalf("categories = %#v", overlay.Categories)
	}
	if !reflect.DeepEqual(overlay.Memberships, memberships) {
		t.Fatalf("memberships = %#v", overlay.Memberships)
	}

	// A rename restates no membership, so the stored verdicts must survive it
	// untouched: an edit that rewrote what it never mentioned would silently
	// change every route naming the category.
	later := now.Add(time.Hour)
	if err := store.UpdateCategory(ctx, application.CategoryWrite{CategoryID: category.ID, Title: "Мои списки 2", UpdatedAt: later}); err != nil {
		t.Fatal(err)
	}
	overlay, err = store.CategoryOverlay(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlay.Categories) != 1 || overlay.Categories[0].Title != "Мои списки 2" || !overlay.Categories[0].CreatedAt.Equal(now) {
		t.Fatalf("renamed = %#v", overlay.Categories)
	}
	if !reflect.DeepEqual(overlay.Memberships, memberships) {
		t.Fatalf("a rename rewrote membership: %#v", overlay.Memberships)
	}

	// A restated membership replaces what is stored, an empty one clears it.
	replacement := []application.CategoryMembership{{CategoryID: category.ID, ListID: "discord", State: application.MembershipAdded, UpdatedAt: later}}
	if err := store.UpdateCategory(ctx, application.CategoryWrite{CategoryID: category.ID, Title: "Мои списки 2", Memberships: replacement, ReplaceMemberships: true, UpdatedAt: later}); err != nil {
		t.Fatal(err)
	}
	overlay, _ = store.CategoryOverlay(ctx)
	if !reflect.DeepEqual(overlay.Memberships, replacement) {
		t.Fatalf("replaced = %#v", overlay.Memberships)
	}

	missing := application.LibraryRemoval{Kind: application.RemovalCategory, ID: "custom-aaaaaaaaaaaaaaaa", RemovedAt: later}
	if err := store.RemoveFromLibrary(ctx, missing); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing deletion err = %v", err)
	}
	if err := store.RemoveFromLibrary(ctx, application.LibraryRemoval{Kind: application.RemovalCategory, ID: category.ID, RemovedAt: later}); err != nil {
		t.Fatal(err)
	}
	overlay, err = store.CategoryOverlay(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlay.Categories) != 0 || len(overlay.Memberships) != 0 {
		t.Fatalf("deletion left state behind: %#v", overlay)
	}
	// Deleting an operator-created category deletes its rows; it never records
	// a removal, which would subtract an identity that no longer exists and
	// would outlive whatever later took it.
	if len(overlay.Removals) != 0 {
		t.Fatalf("an operator-created category recorded a removal: %#v", overlay.Removals)
	}
}

// A catalog category has no title row: its overlay is membership only, and a
// write that carried a title for it would be storing a second answer to a
// question the catalog already answers.
func TestACatalogCategoryStoresMembershipWithoutATitleRow(t *testing.T) {
	store := categoryTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	memberships := []application.CategoryMembership{
		{CategoryID: "video", ListID: "roblox", State: application.MembershipRemoved, UpdatedAt: now},
		{CategoryID: "video", ListID: "twitch", State: application.MembershipAdded, UpdatedAt: now},
	}
	if err := store.UpdateCategory(ctx, application.CategoryWrite{CategoryID: "video", Memberships: memberships, ReplaceMemberships: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	overlay, err := store.CategoryOverlay(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlay.Categories) != 0 {
		t.Fatalf("a catalog category gained a stored title: %#v", overlay.Categories)
	}
	if !reflect.DeepEqual(overlay.Memberships, memberships) {
		t.Fatalf("memberships = %#v", overlay.Memberships)
	}
	if err := store.UpdateCategory(ctx, application.CategoryWrite{CategoryID: "video", Title: "Видео", UpdatedAt: now}); err == nil {
		t.Fatal("a catalog category accepted a title")
	}
	// It does accept deletion, and the deletion is a removal record rather
	// than an edit of the shipped file (ADR 0029): the membership it owned
	// goes with it, and the record is what a later catalog load subtracts.
	if err := store.RemoveFromLibrary(ctx, application.LibraryRemoval{Kind: application.RemovalCategory, ID: "video", RemovedAt: now}); err != nil {
		t.Fatal(err)
	}
	overlay, err = store.CategoryOverlay(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []application.CatalogRemoval{{Kind: application.RemovalCategory, ID: "video", RemovedAt: now}}
	if !reflect.DeepEqual(overlay.Removals, want) {
		t.Fatalf("removals = %#v", overlay.Removals)
	}
	if len(overlay.Memberships) != 0 {
		t.Fatalf("a deleted category kept its membership: %#v", overlay.Memberships)
	}
}

func TestCategoryWritesRefuseWhatTheGrammarForbids(t *testing.T) {
	store := categoryTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	base := application.CustomCategory{ID: "custom-1234567890abcdef", Title: "X", CreatedAt: now, UpdatedAt: now}
	categories := []struct {
		name   string
		mutate func(application.CustomCategory) application.CustomCategory
	}{
		{"unreserved identity", func(c application.CustomCategory) application.CustomCategory { c.ID = "shipped-id"; return c }},
		{"empty title", func(c application.CustomCategory) application.CustomCategory { c.Title = ""; return c }},
		{"zero creation", func(c application.CustomCategory) application.CustomCategory {
			c.CreatedAt = time.Time{}
			return c
		}},
		{"title beyond the bound", func(c application.CustomCategory) application.CustomCategory {
			c.Title = string(make([]rune, maxCategoryTitleLength+1))
			return c
		}},
	}
	for _, tc := range categories {
		if err := store.CreateCustomCategory(ctx, tc.mutate(base), nil); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
	memberships := []struct {
		name string
		row  application.CategoryMembership
	}{
		{"removed on an operator category", application.CategoryMembership{CategoryID: base.ID, ListID: "youtube", State: application.MembershipRemoved, UpdatedAt: now}},
		{"unknown state", application.CategoryMembership{CategoryID: base.ID, ListID: "youtube", State: "quarantined", UpdatedAt: now}},
		{"foreign category", application.CategoryMembership{CategoryID: "video", ListID: "youtube", State: application.MembershipAdded, UpdatedAt: now}},
		{"invalid service", application.CategoryMembership{CategoryID: base.ID, ListID: "Not A Slug", State: application.MembershipAdded, UpdatedAt: now}},
		{"zero moment", application.CategoryMembership{CategoryID: base.ID, ListID: "youtube", State: application.MembershipAdded}},
	}
	for _, tc := range memberships {
		if err := store.CreateCustomCategory(ctx, base, []application.CategoryMembership{tc.row}); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
	// The schema refuses the meaningless combination too, not only the Go
	// validation above it: an operator category has no catalog membership to
	// remove, so a hand-edited store cannot introduce one either.
	if err := store.CreateCustomCategory(ctx, base, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(
		`INSERT INTO category_memberships(category_id,list_id,state,updated_at_ns) VALUES(?,?,?,?)`,
		base.ID, "youtube", "removed", now.UnixNano()); err == nil {
		t.Fatal("the schema accepted a removed verdict on an operator category")
	}
	if _, err := store.db.Exec(`UPDATE custom_categories SET id=? WHERE id=?`, "custom-aaaaaaaaaaaaaaaa", base.ID); err == nil {
		t.Fatal("identity rewrite was accepted")
	}
}
