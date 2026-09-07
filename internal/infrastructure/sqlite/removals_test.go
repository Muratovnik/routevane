package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

// populateLibrary gives one list every kind of state a list can own and gives a
// second list the same, so a deletion can be shown to take exactly one list's
// rows and leave the other's alone.
func populateLibrary(t *testing.T, store *Store, now time.Time) {
	t.Helper()
	statements := []string{
		`INSERT INTO custom_lists(id,title,domains_json,created_at_ns,updated_at_ns) VALUES('custom-1234567890abcdef','Мои сайты','["a.example"]',1,1)`,
		`INSERT INTO custom_sources(id,list_id,url,format,created_at_ns,updated_at_ns) VALUES('feed-1234567890abcdef','youtube','https://example.test/feed.txt','text',1,1)`,
		`INSERT INTO custom_sources(id,list_id,url,format,created_at_ns,updated_at_ns) VALUES('feed-fedcba0987654321','discord','https://example.test/other.txt','text',1,1)`,
		`INSERT INTO list_disabled_sources(list_id,source_id) VALUES('youtube','dns')`,
		`INSERT INTO list_disabled_sources(list_id,source_id) VALUES('discord','dns')`,
		`INSERT INTO list_domain_verdicts(list_id,domain,verdict) VALUES('youtube','a.example','include')`,
		`INSERT INTO list_domain_verdicts(list_id,domain,verdict) VALUES('discord','b.example','exclude')`,
	}
	for _, statement := range statements {
		if _, err := store.db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	memberships := []application.CategoryMembership{
		{CategoryID: "custom-1234567890abcdef", ListID: "youtube", State: application.MembershipAdded, UpdatedAt: now},
		{CategoryID: "custom-1234567890abcdef", ListID: "discord", State: application.MembershipAdded, UpdatedAt: now},
	}
	category := application.CustomCategory{ID: "custom-1234567890abcdef", Title: "Мои списки", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateCustomCategory(context.Background(), category, memberships); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateCategory(context.Background(), application.CategoryWrite{
		CategoryID: "video", ReplaceMemberships: true, UpdatedAt: now,
		Memberships: []application.CategoryMembership{{CategoryID: "video", ListID: "youtube", State: application.MembershipRemoved, UpdatedAt: now}},
	}); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, store *Store, query string, args ...any) int {
	t.Helper()
	var count int
	if err := store.db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return count
}

// Deleting a catalog list records the removal and takes exactly what that list
// owned: its verdicts, its source overrides, its feeds and every membership row
// naming it. Another list's identical rows are untouched — a cascade that took
// them would delete state the operator never asked to delete.
func TestRemovingACatalogListRecordsItAndTakesOnlyWhatThatListOwned(t *testing.T) {
	store := categoryTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	populateLibrary(t, store, now)

	if err := store.RemoveFromLibrary(ctx, application.LibraryRemoval{Kind: application.RemovalList, ID: "youtube", RemovedAt: now}); err != nil {
		t.Fatal(err)
	}
	overlay, err := store.CategoryOverlay(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []application.CatalogRemoval{{Kind: application.RemovalList, ID: "youtube", RemovedAt: now}}
	if !reflect.DeepEqual(overlay.Removals, want) {
		t.Fatalf("removals = %#v", overlay.Removals)
	}
	for _, gone := range []struct {
		what  string
		query string
	}{
		{"verdicts", `SELECT count(*) FROM list_domain_verdicts WHERE list_id='youtube'`},
		{"source overrides", `SELECT count(*) FROM list_disabled_sources WHERE list_id='youtube'`},
		{"feeds", `SELECT count(*) FROM custom_sources WHERE list_id='youtube'`},
		{"membership", `SELECT count(*) FROM category_memberships WHERE list_id='youtube'`},
	} {
		if count := countRows(t, store, gone.query); count != 0 {
			t.Errorf("the deleted list kept its %s: %d", gone.what, count)
		}
	}
	for _, kept := range []struct {
		what  string
		query string
	}{
		{"verdicts", `SELECT count(*) FROM list_domain_verdicts WHERE list_id='discord'`},
		{"source overrides", `SELECT count(*) FROM list_disabled_sources WHERE list_id='discord'`},
		{"feeds", `SELECT count(*) FROM custom_sources WHERE list_id='discord'`},
		{"membership", `SELECT count(*) FROM category_memberships WHERE list_id='discord'`},
	} {
		if count := countRows(t, store, kept.query); count != 1 {
			t.Errorf("another list lost its %s: %d", kept.what, count)
		}
	}
	// The category the deleted list belonged to is still there, holding what
	// remains: deleting a list is not deleting the category that held it.
	if len(overlay.Categories) != 1 {
		t.Fatalf("categories = %#v", overlay.Categories)
	}
	if len(overlay.Memberships) != 1 || overlay.Memberships[0].ListID != "discord" {
		t.Fatalf("memberships = %#v", overlay.Memberships)
	}
	// Recording is idempotent and keeps the first moment: a second deletion is
	// a caller that read a stale library, not a new fact.
	later := now.Add(time.Hour)
	if err := store.RemoveFromLibrary(ctx, application.LibraryRemoval{Kind: application.RemovalList, ID: "youtube", RemovedAt: later}); err != nil {
		t.Fatal(err)
	}
	overlay, _ = store.CategoryOverlay(ctx)
	if !reflect.DeepEqual(overlay.Removals, want) {
		t.Fatalf("a second deletion rewrote the record: %#v", overlay.Removals)
	}
}

// Deleting a category with its lists is one transaction. A failure partway
// through leaves nothing behind: the category the statement already deleted
// comes back with the lists that were never deleted at all.
func TestACategoryDeletionWithItsListsIsOneTransaction(t *testing.T) {
	store := categoryTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	populateLibrary(t, store, now)

	// The second list does not exist, so its deletion fails after the category
	// row and the first list are already gone inside the transaction.
	failing := application.LibraryRemoval{
		Kind: application.RemovalCategory, ID: "custom-1234567890abcdef",
		Lists:     []string{"youtube", "custom-fedcba0987654321"},
		RemovedAt: now,
	}
	if err := store.RemoveFromLibrary(ctx, failing); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	overlay, err := store.CategoryOverlay(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlay.Categories) != 1 || len(overlay.Removals) != 0 {
		t.Fatalf("a failed removal landed: %#v", overlay)
	}
	if len(overlay.Memberships) != 3 {
		t.Fatalf("a failed removal changed membership: %#v", overlay.Memberships)
	}
	if count := countRows(t, store, `SELECT count(*) FROM list_domain_verdicts WHERE list_id='youtube'`); count != 1 {
		t.Fatalf("a failed removal deleted a list's verdicts: %d", count)
	}

	// The same removal without the absent list lands whole: the category's own
	// rows and every row the list it took owned.
	whole := failing
	whole.Lists = []string{"youtube"}
	if err := store.RemoveFromLibrary(ctx, whole); err != nil {
		t.Fatal(err)
	}
	overlay, err = store.CategoryOverlay(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The operator's category left no record; the catalog list it took did.
	want := []application.CatalogRemoval{{Kind: application.RemovalList, ID: "youtube", RemovedAt: now}}
	if len(overlay.Categories) != 0 || !reflect.DeepEqual(overlay.Removals, want) {
		t.Fatalf("overlay = %#v", overlay)
	}
	if len(overlay.Memberships) != 0 {
		t.Fatalf("membership survived both its category and its list: %#v", overlay.Memberships)
	}
}

// The schema refuses a removal record for an operator-created identity, not
// only the Go path above it: such a record would subtract an object that is
// already gone and would outlive whatever later took the identity.
func TestTheSchemaRefusesARemovalRecordTheOverlayCannotMean(t *testing.T) {
	store := categoryTestStore(t)
	stamp := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	refused := []struct {
		name string
		kind string
		id   string
	}{
		{"operator-created list", "list", "custom-1234567890abcdef"},
		{"operator-created category", "category", "custom-fedcba0987654321"},
		{"unknown kind", "route", "youtube"},
	}
	for _, tc := range refused {
		if _, err := store.db.Exec(
			`INSERT INTO catalog_removals(kind,id,removed_at) VALUES(?,?,?)`, tc.kind, tc.id, stamp); err == nil {
			t.Errorf("%s: the schema accepted the record", tc.name)
		}
	}
}

func TestLibraryRemovalsRefuseWhatTheGrammarForbids(t *testing.T) {
	store := categoryTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	base := application.LibraryRemoval{Kind: application.RemovalCategory, ID: "video", RemovedAt: now}
	cases := []struct {
		name   string
		mutate func(application.LibraryRemoval) application.LibraryRemoval
	}{
		{"unknown kind", func(r application.LibraryRemoval) application.LibraryRemoval { r.Kind = "route"; return r }},
		{"invalid identity", func(r application.LibraryRemoval) application.LibraryRemoval { r.ID = "Not A Slug"; return r }},
		{"zero moment", func(r application.LibraryRemoval) application.LibraryRemoval {
			r.RemovedAt = time.Time{}
			return r
		}},
		{"lists on a list deletion", func(r application.LibraryRemoval) application.LibraryRemoval {
			r.Kind, r.ID, r.Lists = application.RemovalList, "youtube", []string{"discord"}
			return r
		}},
		{"invalid held list", func(r application.LibraryRemoval) application.LibraryRemoval {
			r.Lists = []string{"Not A Slug"}
			return r
		}},
		{"repeated held list", func(r application.LibraryRemoval) application.LibraryRemoval {
			r.Lists = []string{"discord", "discord"}
			return r
		}},
	}
	for _, tc := range cases {
		if err := store.RemoveFromLibrary(ctx, tc.mutate(base)); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
	if overlay, err := store.CategoryOverlay(ctx); err != nil || len(overlay.Removals) != 0 {
		t.Fatalf("a refused removal landed: %#v err=%v", overlay.Removals, err)
	}
}
