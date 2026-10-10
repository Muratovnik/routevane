package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	modern "modernc.org/sqlite"
)

func TestProfileBatchReadsMatchSingleProfileDetails(t *testing.T) {
	store := openProfileBatchStore(t)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	profiles := []application.Profile{
		{ID: strings.Repeat("1", 32), Name: "categories only", Categories: []string{"zeta", "alpha"}, Exclusions: []string{"excluded"}, CreatedAt: now, UpdatedAt: now},
		{ID: strings.Repeat("2", 32), Name: "every part", Lists: []string{"zeta", "empty", "alpha"}, Categories: []string{"video"}, Exclusions: []string{"excluded"}, Priority: []string{"zeta", "alpha", "empty"},
			ListDomains: map[string][]string{"alpha": {}, "empty": {}, "zeta": {"z.example", "a.example"}}, CreatedAt: now, UpdatedAt: now},
		{ID: strings.Repeat("3", 32), Name: "newest", Lists: []string{"other"}, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute)},
	}
	for _, profile := range profiles {
		if err := store.CreateProfile(t.Context(), profile); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.ExecContext(t.Context(), "UPDATE profile_list_domains SET domains_json='null' WHERE profile_id=? AND list_id='empty'", profiles[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProfileArchived(t.Context(), profiles[1].ID, now.Add(time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	shelf, err := store.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	assertProfileBatchOrder(t, shelf, profiles[2].ID, profiles[0].ID, profiles[1].ID)
	assertProfileBatchMatchesSingles(t, store, shelf)
	page, err := store.ProfilePage(t.Context(), "")
	if err != nil || page.NextCursor != "" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	if !reflect.DeepEqual(page.Profiles, shelf) {
		t.Fatalf("page differs from shelf:\npage=%#v\nshelf=%#v", page.Profiles, shelf)
	}
	after, err := store.ProfilePage(t.Context(), profiles[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	assertProfileBatchOrder(t, after.Profiles, profiles[1].ID)
	assertProfileBatchMatchesSingles(t, store, after.Profiles)
	if !after.Profiles[0].Archived() {
		t.Fatal("the cursor omitted archival state")
	}
	empty, err := store.ProfilePage(t.Context(), profiles[1].ID)
	if err != nil || empty.Profiles == nil || len(empty.Profiles) != 0 || empty.NextCursor != "" {
		t.Fatalf("empty final page=%#v err=%v", empty, err)
	}
	for _, cursor := range []string{"invalid", strings.Repeat("f", 32)} {
		if _, err := store.ProfilePage(t.Context(), cursor); !errors.Is(err, application.ErrNotFound) {
			t.Fatalf("unknown cursor %q error=%v", cursor, err)
		}
	}
}

func TestProfileBatchDetailsBoundQueriesAndKeepLimitsPerProfile(t *testing.T) {
	store := openProfileBatchStore(t)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for index := 1; index <= profileLimit+1; index++ {
		id := fmt.Sprintf("%032x", index)
		if err := store.CreateProfile(t.Context(), application.Profile{ID: id, Name: id, Lists: []string{"initial"}, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	// The public writer refuses oversized compositions. Directly stored extra
	// rows exercise the reader's existing truncation policy independently for
	// two profiles, so a global LIMIT 128 cannot satisfy this comparison.
	for index := 1; index <= 2; index++ {
		seedOversizedProfileDetails(t, store, fmt.Sprintf("%032x", index))
	}
	tx, err := store.db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	rows, err := profilePageRows(t.Context(), tx, "")
	if err != nil || len(rows) != profileLimit+1 {
		t.Fatalf("page boundary fixture count=%d err=%v", len(rows), err)
	}
	selected := rows[:profileLimit]
	probe := &profileBatchQueryProbe{profileQueryer: tx, maxQueries: 5}
	if err := readProfileDetails(t.Context(), probe, selected); err != nil {
		t.Fatal(err)
	}
	if probe.queries != 5 {
		t.Fatalf("a full page needed %d detail queries, want five", probe.queries)
	}
	for _, profile := range selected[:2] {
		want, err := readProfile(t.Context(), tx, profile.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(want.Lists) != 128 || len(want.Categories) != 128 || len(want.Exclusions) != 128 || len(want.Priority) != 128 || len(want.ListDomains) != 128 {
			t.Fatalf("single-profile limit fixture was not complete: %#v", want)
		}
		if !reflect.DeepEqual(profile, want) {
			t.Fatalf("per-profile limits or order changed:\ngot=%#v\nwant=%#v", profile, want)
		}
	}
	for _, profile := range selected[2:] {
		if !reflect.DeepEqual(profile.Lists, []string{"initial"}) || len(profile.Categories) != 0 || len(profile.Exclusions) != 0 || len(profile.Priority) != 0 || profile.ListDomains != nil {
			t.Fatalf("another profile's composition leaked into %s: %#v", profile.ID, profile)
		}
	}
	if err := readProfileDetails(t.Context(), probe, nil); err != nil || probe.queries != 5 {
		t.Fatalf("empty selection queried unrelated profiles: queries=%d err=%v", probe.queries, err)
	}

	// The former per-profile loop must fail the same query-count control.
	// This proves the positive check rejects N+1 work without a wall-clock
	// threshold or spending another thousand queries on the negative control.
	negative := &profileBatchQueryProbe{profileQueryer: tx, maxQueries: 5}
	var negativeErr error
	for _, row := range selected[:2] {
		profile := application.Profile{ID: row.ID}
		negativeErr = readComposition(t.Context(), negative, &profile)
		if negativeErr == nil {
			negativeErr = readListDomains(t.Context(), negative, &profile)
		}
		if negativeErr != nil {
			break
		}
	}
	if !errors.Is(negativeErr, errProfileBatchQueryBudget) || negative.queries != 6 {
		t.Fatalf("N+1 negative control queries=%d err=%v", negative.queries, negativeErr)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	page, err := store.ProfilePage(t.Context(), "")
	if err != nil || len(page.Profiles) != profileLimit || page.NextCursor != selected[profileLimit-1].ID {
		t.Fatalf("public page boundary count=%d cursor=%q err=%v", len(page.Profiles), page.NextCursor, err)
	}
	last, err := store.ProfilePage(t.Context(), page.NextCursor)
	if err != nil || len(last.Profiles) != 1 || last.Profiles[0].ID != rows[profileLimit].ID || last.NextCursor != "" {
		t.Fatalf("last page=%#v err=%v", last, err)
	}
	assertProfileBatchMatchesSingles(t, store, last.Profiles)
}

func TestProfileBatchReadsRejectInvalidStoredDomains(t *testing.T) {
	store := openProfileBatchStore(t)
	profile := profileBatchFixture()
	if err := store.CreateProfile(t.Context(), profile); err != nil {
		t.Fatal(err)
	}
	tooMany := make([]string, 65)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("domain-%d.example", i)
	}
	encoded, err := json.Marshal(tooMany)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, payload string }{
		{"malformed JSON", "[!"},
		{"non-array JSON", "{}"},
		{"duplicate domains", `["duplicate.example","duplicate.example"]`},
		{"unnormalized domain", `["UPPER.example"]`},
		{"too many domains", string(encoded)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.db.ExecContext(t.Context(), "UPDATE profile_list_domains SET domains_json=? WHERE profile_id=?", test.payload, profile.ID); err != nil {
				t.Fatal(err)
			}
			_, want := store.Profile(t.Context(), profile.ID)
			if want == nil {
				t.Fatal("single-profile oracle accepted the invalid fixture")
			}
			shelf, err := store.Profiles(t.Context())
			if err == nil || err.Error() != want.Error() || len(shelf) != 0 {
				t.Fatalf("shelf=%#v err=%v, want %v", shelf, err, want)
			}
			page, err := store.ProfilePage(t.Context(), "")
			if err == nil || err.Error() != want.Error() || len(page.Profiles) != 0 || page.NextCursor != "" {
				t.Fatalf("page=%#v err=%v, want %v", page, err, want)
			}
		})
	}
	if _, err := store.db.ExecContext(t.Context(), "UPDATE profile_list_domains SET domains_json='[]' WHERE profile_id=?", profile.ID); err != nil {
		t.Fatal(err)
	}
	if shelf, err := store.Profiles(t.Context()); err != nil || len(shelf) != 1 {
		t.Fatalf("valid restored override failed: profiles=%d err=%v", len(shelf), err)
	}
}

func TestProfileBatchDetailsPreserveReadErrors(t *testing.T) {
	store := openProfileBatchStore(t)
	profile := profileBatchFixture()
	if err := store.CreateProfile(t.Context(), profile); err != nil {
		t.Fatal(err)
	}
	for _, part := range []struct {
		name  string
		at    int
		label string
	}{
		{"composition", 1, "read profile composition: "},
		{"domains", 5, "read profile list domains: "},
	} {
		t.Run(part.name, func(t *testing.T) {
			t.Run("query failure", func(t *testing.T) {
				failure := errors.New("profile read unavailable")
				probe := &profileBatchQueryProbe{profileQueryer: store.db, at: part.at, intercept: func(context.Context, string, ...any) (*sql.Rows, error) { return nil, failure }}
				err := readProfileDetails(t.Context(), probe, []application.Profile{{ID: profile.ID}})
				assertProfileBatchReadError(t, err, failure, part.label)
			})
			t.Run("row iteration failure", func(t *testing.T) {
				query := `SELECT ?, 'kept' UNION ALL SELECT ?, json_extract('[!', '$')`
				if part.name == "domains" {
					query = `SELECT ?, 'kept', '[]' UNION ALL SELECT ?, 'other', json_extract('[!', '$')`
				}
				probe := &profileBatchQueryProbe{profileQueryer: store.db, at: part.at, intercept: func(ctx context.Context, _ string, _ ...any) (*sql.Rows, error) {
					return store.db.QueryContext(ctx, query, profile.ID, profile.ID)
				}}
				profiles := []application.Profile{{ID: profile.ID}}
				err := readProfileDetails(t.Context(), probe, profiles)
				var sqliteError *modern.Error
				if !errors.As(err, &sqliteError) || !strings.HasPrefix(err.Error(), part.label) {
					t.Fatalf("row error=%v, want a wrapped SQLite failure", err)
				}
				if part.name == "composition" && !reflect.DeepEqual(profiles[0].Lists, []string{"kept"}) {
					t.Fatal("iteration control failed before reading its first row")
				}
				if part.name == "domains" {
					if _, read := profiles[0].ListDomains["kept"]; !read {
						t.Fatal("iteration control failed before reading its first row")
					}
				}
			})
			t.Run("caller cancellation", func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				probe := &profileBatchQueryProbe{profileQueryer: store.db, at: part.at, intercept: func(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
					cancel()
					return store.db.QueryContext(ctx, query, args...)
				}}
				err := readProfileDetails(ctx, probe, []application.Profile{{ID: profile.ID}})
				assertProfileBatchReadError(t, err, context.Canceled, part.label)
			})
		})
	}
	t.Run("closed connection", func(t *testing.T) {
		conn, err := store.db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		err = readProfileDetails(t.Context(), conn, []application.Profile{{ID: profile.ID}})
		assertProfileBatchReadError(t, err, sql.ErrConnDone, "read profile composition: ")
	})
	if profiles, err := store.Profiles(t.Context()); err != nil || len(profiles) != 1 {
		t.Fatalf("valid read after failures: profiles=%d err=%v", len(profiles), err)
	}
}

func TestProfileBatchDetailsKeepTheProfileRowSnapshot(t *testing.T) {
	store := openProfileBatchStore(t)
	profile := profileBatchFixture()
	if err := store.CreateProfile(t.Context(), profile); err != nil {
		t.Fatal(err)
	}
	want, err := store.Profile(t.Context(), profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	profiles, err := profilePageRows(t.Context(), tx, "")
	if err != nil {
		t.Fatal(err)
	}
	writer, err := sql.Open("sqlite", store.path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writer.SetMaxOpenConns(1)
	if err := configureConnection(t.Context(), writer, false); err != nil {
		t.Fatal(err)
	}
	write, err := writer.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback()
	for _, statement := range []string{
		"UPDATE profiles SET name='changed' WHERE id=?",
		"DELETE FROM profile_lists WHERE profile_id=?",
		"INSERT INTO profile_lists(profile_id,list_id) VALUES (?,'changed')",
		`UPDATE profile_list_domains SET domains_json='["changed.example"]' WHERE profile_id=?`,
	} {
		if _, err := write.ExecContext(t.Context(), statement, profile.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := write.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := readProfileDetails(t.Context(), tx, profiles); err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || !reflect.DeepEqual(profiles[0], want) {
		t.Fatalf("mixed profile snapshots:\nprofiles=%#v\nwant=%#v", profiles, want)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Profile(t.Context(), profile.ID)
	if err != nil || updated.Name != "changed" || !reflect.DeepEqual(updated.Lists, []string{"changed"}) || !reflect.DeepEqual(updated.ListDomains["alpha"], []string{"changed.example"}) {
		t.Fatalf("writer did not publish its new state: profile=%#v err=%v", updated, err)
	}
}

func seedOversizedProfileDetails(t *testing.T, store *Store, profileID string) {
	t.Helper()
	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(t.Context(), "DELETE FROM profile_lists WHERE profile_id=?", profileID); err != nil {
		t.Fatal(err)
	}
	for _, part := range []struct {
		insert string
		prefix string
	}{
		{"INSERT INTO profile_lists(profile_id,list_id) VALUES ", "list"},
		{"INSERT INTO profile_categories(profile_id,category_id) VALUES ", "category"},
		{"INSERT INTO profile_exclusions(profile_id,list_id) VALUES ", "excluded"},
		{"INSERT INTO profile_list_domains(profile_id,list_id,domains_json) VALUES ", "domains"},
		{"INSERT INTO profile_list_priorities(profile_id,list_id,position) VALUES ", "priority"},
	} {
		count := 129
		if part.prefix == "priority" {
			count = 128 // The schema itself bounds positions to 0..127.
		}
		values := make([]string, count)
		args := make([]any, 0, count*3)
		for i := range values {
			values[i] = "(?,?)"
			args = append(args, profileID, fmt.Sprintf("%s-%03d", part.prefix, i))
			if part.prefix == "domains" {
				values[i] = "(?,?,?)"
				args = append(args, "[]")
			}
			if part.prefix == "priority" {
				values[i] = "(?,?,?)"
				args = append(args, count-1-i)
			}
		}
		if _, err := tx.ExecContext(t.Context(), part.insert+strings.Join(values, ","), args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func assertProfileBatchReadError(t *testing.T, err, want error, label string) {
	t.Helper()
	if !errors.Is(err, want) || !strings.HasPrefix(err.Error(), label) || errors.Is(err, application.ErrNotFound) {
		t.Fatalf("read error=%v, want %q wrapping %v", err, label, want)
	}
}

func assertProfileBatchOrder(t *testing.T, profiles []application.Profile, ids ...string) {
	t.Helper()
	got := make([]string, len(profiles))
	for i := range profiles {
		got[i] = profiles[i].ID
	}
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("profile order=%v, want %v", got, ids)
	}
}

func assertProfileBatchMatchesSingles(t *testing.T, store *Store, profiles []application.Profile) {
	t.Helper()
	for _, profile := range profiles {
		want, err := store.Profile(t.Context(), profile.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(profile, want) {
			t.Fatalf("batch profile differs from independent read:\ngot=%#v\nwant=%#v", profile, want)
		}
	}
}

func profileBatchFixture() application.Profile {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	return application.Profile{ID: strings.Repeat("1", 32), Name: "profile", Lists: []string{"alpha"}, ListDomains: map[string][]string{"alpha": {}}, CreatedAt: now, UpdatedAt: now}
}

func openProfileBatchStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.Context(), newDataRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

var errProfileBatchQueryBudget = errors.New("too many profile detail queries")

type profileBatchQueryProbe struct {
	profileQueryer
	queries    int
	maxQueries int
	at         int
	intercept  func(context.Context, string, ...any) (*sql.Rows, error)
}

func (p *profileBatchQueryProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	p.queries++
	if p.maxQueries > 0 && p.queries > p.maxQueries {
		return nil, errProfileBatchQueryBudget
	}
	if p.queries == p.at && p.intercept != nil {
		return p.intercept(ctx, query, args...)
	}
	return p.profileQueryer.QueryContext(ctx, query, args...)
}
