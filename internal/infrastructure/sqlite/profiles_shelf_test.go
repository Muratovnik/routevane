package sqlite

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

func shelfIDs(page application.ProfilePage) []string {
	ids := make([]string, 0, len(page.Profiles))
	for _, profile := range page.Profiles {
		ids = append(ids, profile.ID)
	}
	return ids
}

// A shelf is one half of the library read newest first, and the archived count
// is the library's own: it ignores which half was asked for and where the
// cursor stands, and a deleted profile is on neither half nor in the count.
func TestProfileShelvesSplitTheLibraryAndCountArchivedProfiles(t *testing.T) {
	ctx := t.Context()
	store := openProfileBatchStore(t)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	id := func(digit string) string { return strings.Repeat(digit, 32) }
	// Newest first the order is 5, 4, 3, 2, 1: 2 and 4 are archived, and 3 is
	// archived and then deleted.
	for index, digit := range []string{"1", "2", "3", "4", "5"} {
		created := now.Add(time.Duration(index) * time.Minute)
		if err := store.CreateProfile(ctx, application.Profile{ID: id(digit), Name: "profile " + digit, Lists: []string{"alpha"}, CreatedAt: created, UpdatedAt: created}); err != nil {
			t.Fatal(err)
		}
	}
	archivedAt := now.Add(time.Hour)
	for _, digit := range []string{"2", "3", "4"} {
		if err := store.SetProfileArchived(ctx, id(digit), archivedAt, archivedAt); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeleteProfile(ctx, id("3"), archivedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	read := func(shelf application.ProfileShelf, afterID string) application.ProfilePage {
		t.Helper()
		page, err := store.ProfilePage(ctx, shelf, afterID)
		if err != nil {
			t.Fatalf("shelf %q after %q: %v", shelf, afterID, err)
		}
		return page
	}
	for _, test := range []struct {
		shelf   application.ProfileShelf
		afterID string
		want    []string
	}{
		{application.ProfileShelfActive, "", []string{id("5"), id("1")}},
		{application.ProfileShelfArchived, "", []string{id("4"), id("2")}},
		{application.ProfileShelfAll, "", []string{id("5"), id("4"), id("2"), id("1")}},
		// A cursor is a position, not a row of the shelf being read: the active
		// shelf continues after an archived profile, and the archived shelf after
		// an active one.
		{application.ProfileShelfActive, id("4"), []string{id("1")}},
		{application.ProfileShelfArchived, id("5"), []string{id("4"), id("2")}},
		{application.ProfileShelfArchived, id("4"), []string{id("2")}},
		{application.ProfileShelfArchived, id("2"), nil},
		// The deleted profile still positions a page.
		{application.ProfileShelfActive, id("3"), []string{id("1")}},
	} {
		page := read(test.shelf, test.afterID)
		if got := shelfIDs(page); !slices.Equal(got, test.want) {
			t.Errorf("shelf %q after %q read %v, want %v", test.shelf, test.afterID, got, test.want)
		}
		if page.ArchivedCount != 2 {
			t.Errorf("shelf %q after %q counted %d archived profiles, want 2", test.shelf, test.afterID, page.ArchivedCount)
		}
	}
	if _, err := store.ProfilePage(ctx, application.ProfileShelf("deleted"), ""); err == nil {
		t.Fatal("an unknown shelf was read")
	}
}

// The filter is applied before the page bound. A shelf with a few profiles is
// not hidden behind two hundred of the other kind, which is what a screen
// filtering already-loaded rows would do.
func TestProfileShelfFilterAppliesBeforeThePageBound(t *testing.T) {
	ctx := t.Context()
	store := openProfileBatchStore(t)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	total := profileLimit + 2
	for index := 1; index <= total; index++ {
		profileID := fmt.Sprintf("%032x", index)
		// The single active profile is the oldest, so an unfiltered first page
		// would not contain it.
		created := now.Add(time.Duration(total-index) * time.Second)
		if err := store.CreateProfile(ctx, application.Profile{ID: profileID, Name: profileID, Lists: []string{"alpha"}, CreatedAt: created, UpdatedAt: created}); err != nil {
			t.Fatal(err)
		}
		if index != total {
			if err := store.SetProfileArchived(ctx, profileID, now.Add(time.Hour), now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	}
	oldest := fmt.Sprintf("%032x", total)
	if all, err := store.ProfilePage(ctx, application.ProfileShelfAll, ""); err != nil || slices.Contains(shelfIDs(all), oldest) {
		t.Fatalf("the fixture is not a control: the unfiltered first page holds the active profile (err=%v)", err)
	}
	active, err := store.ProfilePage(ctx, application.ProfileShelfActive, "")
	if err != nil || !slices.Equal(shelfIDs(active), []string{oldest}) || active.NextCursor != "" {
		t.Fatalf("active shelf=%v next=%q err=%v", shelfIDs(active), active.NextCursor, err)
	}
	archived, err := store.ProfilePage(ctx, application.ProfileShelfArchived, "")
	if err != nil || len(archived.Profiles) != profileLimit || archived.NextCursor == "" {
		t.Fatalf("archived shelf holds %d profiles, next=%q err=%v", len(archived.Profiles), archived.NextCursor, err)
	}
	if want := total - 1; archived.ArchivedCount != want || active.ArchivedCount != want {
		t.Fatalf("archived count %d / %d, want %d on both shelves", archived.ArchivedCount, active.ArchivedCount, want)
	}
	// The cursor continues the archived shelf with what the first page left.
	rest, err := store.ProfilePage(ctx, application.ProfileShelfArchived, archived.NextCursor)
	if err != nil || len(rest.Profiles) != 1 || rest.NextCursor != "" {
		t.Fatalf("archived continuation holds %d profiles, next=%q err=%v", len(rest.Profiles), rest.NextCursor, err)
	}
}
