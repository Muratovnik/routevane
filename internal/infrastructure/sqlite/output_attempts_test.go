package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

// A database written before version 18 holds failed attempts that name no
// lists. The column that lets an attempt name them is added without rewriting
// the attempt, so each earlier attempt keeps reading back exactly as it was.
func TestVersionSeventeenAttemptsReadBackWithoutLists(t *testing.T) {
	const previousVersion = 17
	root := newDataRoot(t)
	path := filepath.Join(root, DatabaseName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	previous := &Store{db: db, path: path}
	if err := previous.initialize(context.Background(), migrations[:previousVersion]); err == nil {
		t.Fatal("a database short of the current version was accepted")
	}
	profileID, outputID := strings.Repeat("1", 32), strings.Repeat("3", 32)
	for _, statement := range []string{
		`INSERT INTO profiles(id,name,created_at_ns,updated_at_ns) VALUES('` + profileID + `','Legacy',1,1)`,
		`INSERT INTO outputs(id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES('` + outputID + `','` + profileID + `','keenetic','keenetic-bat-ipv4-v1','keenetic-route-bat','keenetic-bat-ipv4-v1','legacy',1)`,
		`INSERT INTO output_attempts(output_id,status,code,projected_rules,maximum_rules,completed_at_ns) VALUES('` + outputID + `','failed','partial_coverage',0,0,5)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("migrating a version-17 database: %v", err)
	}
	defer store.Close()
	attempt, err := store.LatestOutputAttempt(context.Background(), outputID)
	if err != nil || attempt.Code != "partial_coverage" || len(attempt.UncoveredLists) != 0 || attempt.CompletedAt.UnixNano() != 5 {
		t.Fatalf("an earlier attempt changed: %#v err=%v", attempt, err)
	}
	// An attempt stays immutable under the new column.
	if _, err := store.db.Exec(`UPDATE output_attempts SET uncovered_lists_json='[]'`); err == nil {
		t.Fatal("an attempt was rewritten")
	}
}

func TestFailedAttemptKeepsTheListsItNames(t *testing.T) {
	ctx := context.Background()
	f := newRetirementFixture(t)
	completed := retirementNow.Add(24 * time.Hour)
	lists := []application.UncoveredList{{ID: "alpha", Title: "Альфа"}, {ID: "custom-0102030405060708", Title: "Мой список"}}
	record := func(attempt application.OutputAttempt) error {
		attempt.OutputID, attempt.CompletedAt = f.output.ID, completed
		return f.store.RecordOutputAttempt(ctx, attempt)
	}

	if err := record(application.OutputAttempt{Status: "failed", Code: application.BuildFailurePartialCoverage, UncoveredLists: lists}); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.LatestOutputAttempt(ctx, f.output.ID)
	if err != nil || !reflect.DeepEqual(got.UncoveredLists, lists) || got.Code != application.BuildFailurePartialCoverage {
		t.Fatalf("stored attempt = %#v err=%v", got, err)
	}

	// A later failure of another kind names no list, and an attempt that names
	// none reads back with none rather than an empty list.
	completed = completed.Add(time.Minute)
	if err := record(application.OutputAttempt{Status: "failed", Code: application.BuildFailureRuleLimit, ProjectedRules: 3, MaximumRules: 2}); err != nil {
		t.Fatal(err)
	}
	if got, err = f.store.LatestOutputAttempt(ctx, f.output.ID); err != nil || got.UncoveredLists != nil || got.ProjectedRules != 3 {
		t.Fatalf("a later failure inherited lists: %#v err=%v", got, err)
	}

	tooMany := make([]application.UncoveredList, maxStoredUncoveredLists+1)
	for i := range tooMany {
		tooMany[i] = application.UncoveredList{ID: fmt.Sprintf("list-%04d", i), Title: "t"}
	}
	for name, attempt := range map[string]application.OutputAttempt{
		"lists on another failure": {Status: "failed", Code: application.BuildFailureRuleLimit, UncoveredLists: lists},
		"lists on a success":       {Status: "success", ArtifactID: strings.Repeat("9", 32), UncoveredLists: lists},
		"an identity that is not a slug": {Status: "failed", Code: application.BuildFailurePartialCoverage,
			UncoveredLists: []application.UncoveredList{{ID: "Not A Slug", Title: "x"}}},
		"a blank title": {Status: "failed", Code: application.BuildFailurePartialCoverage,
			UncoveredLists: []application.UncoveredList{{ID: "alpha"}}},
		"an oversize title": {Status: "failed", Code: application.BuildFailurePartialCoverage,
			UncoveredLists: []application.UncoveredList{{ID: "alpha", Title: strings.Repeat("я", maxStoredUncoveredTitle+1)}}},
		"too many lists": {Status: "failed", Code: application.BuildFailurePartialCoverage, UncoveredLists: tooMany},
	} {
		if err := record(attempt); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
	// The schema states the same rule for a writer that bypasses the store.
	if _, err := f.store.db.Exec(`INSERT INTO output_attempts(output_id,status,code,completed_at_ns,uncovered_lists_json) VALUES(?,'failed','rule_limit',9,'[{"id":"alpha","title":"x"}]')`, f.output.ID); err == nil {
		t.Error("the schema accepted lists on another failure")
	}
	if _, err := f.store.db.Exec(`INSERT INTO output_attempts(output_id,status,code,completed_at_ns,uncovered_lists_json) VALUES(?,'failed','partial_coverage',9,'{"not":"an array"}')`, f.output.ID); err == nil {
		t.Error("the schema accepted lists that are not an array")
	}
}
