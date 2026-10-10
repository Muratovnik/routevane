package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	modern "modernc.org/sqlite"
)

func TestFreshBootstrapMatchesHistoricalReplay(t *testing.T) {
	replayed := newBootstrapTestStore(t)
	if err := replayed.initialize(t.Context(), migrations); err != nil {
		t.Fatalf("historical replay: %v", err)
	}
	root := newDataRoot(t)
	fresh, err := Open(t.Context(), root)
	if err != nil {
		t.Fatalf("fresh bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = fresh.Close() })

	// Compare the actual historical result, not the required-column verifier:
	// that verifier deliberately does not describe every constraint or index.
	want := readBootstrapSchema(t, replayed.db)
	if difference := bootstrapSchemaDifference(want, readBootstrapSchema(t, fresh.db)); difference != "" {
		t.Fatal(difference)
	}
	for _, store := range []*Store{replayed, fresh} {
		var invalidTimestamps int
		if err := store.db.QueryRowContext(t.Context(), "SELECT count(*) FROM schema_migrations WHERE applied_at_ns <= 0").Scan(&invalidTimestamps); err != nil || invalidTimestamps != 0 {
			t.Fatalf("migration timestamps: invalid=%d err=%v", invalidTimestamps, err)
		}
		assertBootstrapSchemaBehavior(t, store.db)
	}
	want = readBootstrapSchema(t, replayed.db)
	if difference := bootstrapSchemaDifference(want, readBootstrapSchema(t, fresh.db)); difference != "" {
		t.Fatal(difference)
	}
	if err := fresh.Close(); err != nil {
		t.Fatal(err)
	}
	fresh, err = OpenExisting(t.Context(), root)
	if err != nil {
		t.Fatalf("reopen bootstrap: %v", err)
	}
	if difference := bootstrapSchemaDifference(want, readBootstrapSchema(t, fresh.db)); difference != "" {
		t.Fatalf("reopen changed schema or data: %s", difference)
	}

	// A real missing secondary index passes the existing shape verifier. The
	// independent comparison must still detect it, or it is not a parity test.
	if _, err := fresh.db.ExecContext(t.Context(), "DROP INDEX sightings_list_source_idx"); err != nil {
		t.Fatal(err)
	}
	if err := verifySchema(t.Context(), fresh.db); err != nil {
		t.Fatalf("negative control should pass the existing shape verifier: %v", err)
	}
	if bootstrapSchemaDifference(want, readBootstrapSchema(t, fresh.db)) == "" {
		t.Fatal("schema comparison accepted a missing secondary index")
	}
}

func TestFreshBootstrapRefusesEveryUnversionedSchemaObject(t *testing.T) {
	for _, test := range []struct {
		name string
		sql  string
	}{
		{"populated table", "CREATE TABLE occupied(value TEXT) STRICT; INSERT INTO occupied VALUES ('retained')"},
		{"view only", "CREATE VIEW occupied AS SELECT 'retained' AS value"},
		{"internal table only", "CREATE TABLE discarded(id INTEGER PRIMARY KEY AUTOINCREMENT); DROP TABLE discarded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newBootstrapTestStore(t)
			if _, err := store.db.ExecContext(t.Context(), test.sql); err != nil {
				t.Fatal(err)
			}
			before := readBootstrapSchema(t, store.db)
			if err := store.initializeCurrent(t.Context()); !errors.Is(err, ErrIncompatibleSchema) {
				t.Fatalf("unversioned nonempty schema error=%v", err)
			}
			if difference := bootstrapSchemaDifference(before, readBootstrapSchema(t, store.db)); difference != "" {
				t.Fatalf("refusal changed existing state: %s", difference)
			}
		})
	}
}

func TestSchemaBootstrapRechecksVersionAndObjectsInItsTransaction(t *testing.T) {
	for _, statement := range []string{
		"PRAGMA user_version = 1",
		"CREATE VIEW occupied AS SELECT 'retained' AS value",
	} {
		t.Run(statement, func(t *testing.T) {
			store := newBootstrapTestStore(t)
			if _, err := store.db.ExecContext(t.Context(), statement); err != nil {
				t.Fatal(err)
			}
			before := readBootstrapSchema(t, store.db)
			// Call the transaction directly with an already stale empty-v0
			// assumption; the earlier initialization check cannot protect it.
			err := store.applyBootstrap(t.Context(), bootstrapSchemaSQL)
			if err == nil || !strings.Contains(err.Error(), "database changed during initialization") || errors.Is(err, ErrIncompatibleSchema) {
				t.Fatalf("stale bootstrap state error=%v", err)
			}
			if difference := bootstrapSchemaDifference(before, readBootstrapSchema(t, store.db)); difference != "" {
				t.Fatalf("stale bootstrap changed existing state: %s", difference)
			}
		})
	}
}

func TestSchemaBootstrapRollsBackDDLAndPartialHistory(t *testing.T) {
	for _, test := range []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "SQL failure after creating a table",
			sql:  "CREATE TABLE partial(value TEXT) STRICT; INVALID SQL",
			want: "execute schema bootstrap",
		},
		{
			name: "history failure after earlier ledger rows",
			sql:  "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY CHECK(version <> 8), applied_at_ns INTEGER NOT NULL) STRICT; CREATE TABLE partial(value TEXT) STRICT",
			want: "record bootstrap migration 8",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newBootstrapTestStore(t)
			err := store.initializeWithBootstrap(t.Context(), migrations, test.sql)
			if err == nil || !strings.Contains(err.Error(), test.want) || errors.Is(err, ErrIncompatibleSchema) {
				t.Fatalf("bootstrap error=%v, want %q", err, test.want)
			}
			assertBootstrapRemainsEmptyAfterReopen(t, store)
		})
	}
}

func TestSchemaBootstrapPreservesCancellationAfterDDLStarts(t *testing.T) {
	assertSchemaCancellationAfterDDL(t, func(ctx context.Context, store *Store, statement string) error {
		return store.initializeWithBootstrap(ctx, migrations, statement)
	})
}

func TestSchemaMigrationPreservesCancellationAfterDDLStarts(t *testing.T) {
	assertSchemaCancellationAfterDDL(t, func(ctx context.Context, store *Store, statement string) error {
		return store.applyMigration(ctx, migration{version: 1, sql: statement})
	})
}

func assertSchemaCancellationAfterDDL(t *testing.T, initialize func(context.Context, *Store, string) error) {
	t.Helper()
	if err := bootstrapBarrierRegistration(); err != nil {
		t.Fatal(err)
	}
	barrier := &bootstrapTestBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	bootstrapActiveBarrier.Store(barrier)
	t.Cleanup(func() { bootstrapActiveBarrier.Store(nil) })
	release := sync.OnceFunc(func() { close(barrier.release) })
	t.Cleanup(release)
	store := newBootstrapTestStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- initialize(ctx, store,
			"CREATE TABLE partial(value TEXT) STRICT; SELECT "+bootstrapBarrierFunction+"();")
	}()
	select {
	case <-barrier.entered:
		// The preceding CREATE has executed in the checkpoint transaction.
		// Cancel during the next statement before allowing it to finish.
		cancel()
		release()
	case err := <-done:
		t.Fatalf("bootstrap did not reach the post-DDL barrier: %v", err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) || errors.Is(err, ErrIncompatibleSchema) {
		t.Fatalf("interrupted bootstrap error=%v, want wrapped context cancellation", err)
	}
	assertBootstrapRemainsEmptyAfterReopen(t, store)
}

func TestSchemaBootstrapRequiresItsHistoricalMigrationPrefix(t *testing.T) {
	broken := slices.Clone(migrations)
	broken[bootstrapSchemaVersion-1].version = bootstrapSchemaVersion - 1
	for _, set := range [][]migration{migrations[:bootstrapSchemaVersion-1], broken} {
		store := newBootstrapTestStore(t)
		if err := store.initializeWithBootstrap(t.Context(), set, bootstrapSchemaSQL); !errors.Is(err, ErrIncompatibleSchema) {
			t.Fatalf("invalid checkpoint migration sequence error=%v", err)
		}
		assertBootstrapRemainsEmptyAfterReopen(t, store)
	}
}

func assertBootstrapSchemaBehavior(t *testing.T, db *sql.DB) {
	t.Helper()
	profile := strings.Repeat("a", 32)
	output := strings.Repeat("b", 32)
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO settings(key,value,updated_at_ns) VALUES ('valid','saved',1)", nil},
		{"INSERT INTO profiles(id,name,created_at_ns,updated_at_ns) VALUES (?,'Profile',1,1)", []any{profile}},
		{"INSERT INTO profile_lists(profile_id,list_id) VALUES (?,'named')", []any{profile}},
		{"INSERT INTO outputs(id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES (?,?,'target','format','renderer','1','revision',1)", []any{output, profile}},
	} {
		if _, err := db.ExecContext(t.Context(), statement.sql, statement.args...); err != nil {
			t.Fatalf("valid schema operation: %v", err)
		}
	}
	for _, test := range []struct {
		name string
		sql  string
		args []any
	}{
		{"CHECK constraint", "INSERT INTO settings(key,value,updated_at_ns) VALUES ('','value',1)", nil},
		{"STRICT type", "INSERT INTO settings(key,value,updated_at_ns) VALUES ('typed','value','invalid')", nil},
		{"foreign key", "INSERT INTO profile_lists(profile_id,list_id) VALUES (?,'other')", []any{strings.Repeat("c", 32)}},
		{"membership trigger", "INSERT INTO profile_exclusions(profile_id,list_id) VALUES (?,'named')", []any{profile}},
		{"identity trigger", "UPDATE profiles SET id=? WHERE id=?", []any{strings.Repeat("d", 32), profile}},
		{"unique secondary index", "INSERT INTO outputs(id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES (?,?,'target','format','renderer','1','revision',1)", []any{strings.Repeat("e", 32), profile}},
	} {
		if _, err := db.ExecContext(t.Context(), test.sql, test.args...); err == nil {
			t.Fatalf("schema accepted a violation of %s", test.name)
		}
	}
}

// This comparison includes column cids because the legacy import uses SELECT *.
// Physical storage, object creation order and ledger timestamps are excluded
// from equality; all stored SQL and logical schema attributes are retained.
func readBootstrapSchema(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	state := map[string]string{
		"version": canonicalBootstrapRows(t, db, "PRAGMA user_version"),
		"table modes": canonicalBootstrapRows(t, db,
			"SELECT name,type,ncol,wr,strict FROM pragma_table_list WHERE schema='main'"),
	}
	objects := bootstrapRows(t, db, "SELECT type,name,tbl_name,sql FROM main.sqlite_schema")
	for _, object := range objects {
		state[fmt.Sprintf("object/%s/%s", object[0], object[1])] = bootstrapJSON(t, object)
		if object[0] != "table" {
			continue
		}
		table := object[1].(string)
		state["columns/"+table] = canonicalBootstrapRows(t, db,
			`SELECT cid,name,type,"notnull",dflt_value,pk,hidden FROM pragma_table_xinfo(?)`, table)
		state["foreign keys/"+table] = canonicalBootstrapRows(t, db,
			`SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list(?)`, table)
		indexes := bootstrapRows(t, db, `SELECT name,"unique",origin,partial FROM pragma_index_list(?)`, table)
		for _, index := range indexes {
			name := index[0].(string)
			state["index/"+name] = bootstrapJSON(t, index)
			state["index columns/"+name] = canonicalBootstrapRows(t, db,
				`SELECT seqno,cid,name,"desc",coll,"key" FROM pragma_index_xinfo(?)`, name)
		}
		if table == "schema_migrations" {
			state["history"] = canonicalBootstrapRows(t, db, "SELECT version FROM schema_migrations")
		} else {
			state["data/"+table] = canonicalBootstrapRows(t, db, `SELECT * FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`)
		}
	}
	return state
}

func canonicalBootstrapRows(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	var rows []string
	for _, row := range bootstrapRows(t, db, query, args...) {
		rows = append(rows, bootstrapJSON(t, row))
	}
	slices.Sort(rows)
	return strings.Join(rows, "\n")
}

func bootstrapRows(t *testing.T, db *sql.DB, query string, args ...any) [][]any {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var result [][]any
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			t.Fatal(err)
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func bootstrapJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func bootstrapSchemaDifference(want, got map[string]string) string {
	keys := make([]string, 0, len(want)+len(got))
	for key := range want {
		keys = append(keys, key)
	}
	for key := range got {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range slices.Compact(keys) {
		wantValue, wantExists := want[key]
		gotValue, gotExists := got[key]
		if wantExists != gotExists || wantValue != gotValue {
			return fmt.Sprintf("schema parity differs at %s:\nwant (present=%t): %s\ngot (present=%t): %s", key, wantExists, wantValue, gotExists, gotValue)
		}
	}
	return ""
}

func newBootstrapTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(newDataRoot(t), DatabaseName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, path: path}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func assertBootstrapRemainsEmptyAfterReopen(t *testing.T, store *Store) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", store.path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	var version, objects int
	if err := db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM main.sqlite_schema").Scan(&objects); err != nil {
		t.Fatal(err)
	}
	if version != 0 || objects != 0 {
		t.Fatalf("failed checkpoint survived reopen: version=%d objects=%d", version, objects)
	}
}

const bootstrapBarrierFunction = "routevane_test_bootstrap_barrier"

type bootstrapTestBarrier struct {
	entered chan struct{}
	release chan struct{}
}

var bootstrapActiveBarrier atomic.Pointer[bootstrapTestBarrier]

var bootstrapBarrierRegistration = sync.OnceValue(func() error {
	return modern.RegisterScalarFunction(bootstrapBarrierFunction, 0, func(*modern.FunctionContext, []driver.Value) (driver.Value, error) {
		barrier := bootstrapActiveBarrier.Load()
		if barrier == nil {
			return nil, errors.New("bootstrap test barrier is not installed")
		}
		close(barrier.entered)
		<-barrier.release
		return int64(1), nil
	})
})
