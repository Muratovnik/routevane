package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	modern "modernc.org/sqlite"
)

func TestMigrationsAreRepeatableAndPragmasAreVerified(t *testing.T) {
	root := newDataRoot(t)
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyPragmas(context.Background(), store.db); err != nil {
		t.Fatal(err)
	}
	var journal string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || strings.ToLower(journal) != "wal" {
		t.Fatalf("journal=%q err=%v", journal, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(context.Background(), root)
	if err != nil {
		t.Fatalf("second migration open: %v", err)
	}
	store.Close()
	if version, err := ReadUserVersion(context.Background(), root); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("version=%d err=%v", version, err)
	}
	inspection := InspectExisting(context.Background(), root)
	if !inspection.Healthy {
		t.Fatalf("inspection=%#v", inspection)
	}
}

func TestMigrationFailureRollsBackAndNewerSchemaFailsClosed(t *testing.T) {
	t.Run("rollback", func(t *testing.T) {
		root := newDataRoot(t)
		path := filepath.Join(root, DatabaseName)
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		store := &Store{db: db, path: path}
		err = store.initialize(context.Background(), []migration{{version: 1, sql: "CREATE TABLE partial(value TEXT) STRICT; INVALID SQL"}})
		if err == nil {
			t.Fatal("failing migration succeeded")
		}
		var version, partial int
		if scanErr := db.QueryRow("PRAGMA user_version").Scan(&version); scanErr != nil {
			t.Fatal(scanErr)
		}
		if scanErr := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='partial'").Scan(&partial); scanErr != nil {
			t.Fatal(scanErr)
		}
		if version != 0 || partial != 0 {
			t.Fatalf("failed migration leaked state: version=%d partial=%d", version, partial)
		}
		db.Close()
	})
	t.Run("newer", func(t *testing.T) {
		root := newDataRoot(t)
		path := filepath.Join(root, DatabaseName)
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		newer := CurrentSchemaVersion + 1
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version=%d", newer)); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if _, err := Open(context.Background(), root); !errors.Is(err, ErrIncompatibleSchema) {
			t.Fatalf("Open newer schema error=%v", err)
		}
		if version, err := ReadUserVersion(context.Background(), root); err != nil || version != newer {
			t.Fatalf("newer database was changed: version=%d err=%v", version, err)
		}
	})
}

// A schema question that cannot be asked has no answer. Every check read the
// database and treated a failed read as a bad shape, so a release build whose
// deadline ran out while it was verifying an imported database reported a
// missing trigger: the database this product had just written itself was
// declared incompatible, and opening it refused.
func TestAFailedSchemaReadIsNotAVerdictOnTheSchema(t *testing.T) {
	root := newDataRoot(t)
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	err = verifySchema(stopped, store.db)
	if err == nil {
		t.Fatal("a schema read that could not run reported a sound schema")
	}
	if errors.Is(err, ErrIncompatibleSchema) {
		t.Fatalf("a failed read was reported as a verdict on the schema: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want it to carry %v", err, context.Canceled)
	}
	// The same database answers for itself when the question can be asked.
	if err := verifySchema(context.Background(), store.db); err != nil {
		t.Fatalf("a sound schema was refused: %v", err)
	}
}

// A first run has to apply every migration before the product can be used.
// Neither the length of that sequence nor the size of one migration in it is
// what a query budget describes: a build measured one migration past five
// seconds while a race detector and four test packages shared the machine, and
// a first run there reported an unavailable database while it was still
// applying the schema. One step here outlasts a query budget and has to finish.
func TestSchemaWorkOutlastingAQueryBudgetStillFinishes(t *testing.T) {
	pause := operationTimeout + time.Second
	registerPause(t, pause)
	// The baseline migration carries the ledger every later one records itself
	// in; the pausing migration after it is the step that outlasts the budget.
	set := []migration{migrations[0], {version: 2, sql: "SELECT " + pauseFunction + "();"}}
	root := newDataRoot(t)
	path := filepath.Join(root, DatabaseName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	store := &Store{db: db, path: path}

	started := time.Now()
	err = store.initialize(context.Background(), set)
	elapsed := time.Since(started)

	if elapsed <= operationTimeout {
		t.Fatalf("the sequence took %s, which one budget of %s already covers; the run proves nothing", elapsed, operationTimeout)
	}
	// Reaching the end of a set that stops short of the current version is the
	// only refusal expected here: every migration in it was applied.
	if !errors.Is(err, ErrIncompatibleSchema) {
		t.Fatalf("initialize error = %v, want %v", err, ErrIncompatibleSchema)
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != len(set) {
		t.Fatalf("applied %d of %d migrations", version, len(set))
	}
}

// Bringing a database to the current schema is a sequence of operations, and
// the sequence grows every time the schema gains a migration. Measuring the
// whole sequence against the budget of one operation let a first run on a slow
// or busy disk fail as an unavailable database while it was still making
// progress, so each step is measured on its own. A caller that bounds the open
// still bounds everything inside it, and a step that cannot finish still ends.
func TestSchemaWorkIsBoundedOneStepAtATime(t *testing.T) {
	t.Run("each step starts its own budget", func(t *testing.T) {
		deadline := func() time.Time {
			var seen time.Time
			if err := schemaStep(context.Background(), func(ctx context.Context) error {
				value, ok := ctx.Deadline()
				if !ok {
					return errors.New("schema step ran unbounded")
				}
				seen = value
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			return seen
		}
		// Schema work is not measured against the budget a query gets.
		if schemaStepTimeout <= operationTimeout {
			t.Fatalf("schema step budget %s is no larger than a query's %s", schemaStepTimeout, operationTimeout)
		}
		before := time.Now()
		first := deadline()
		after := time.Now()
		// The step is bounded from the moment it starts, which is somewhere
		// between these two readings, so its budget is one step's worth
		// measured from inside that interval and never more.
		if !first.After(before) || first.After(after.Add(schemaStepTimeout)) {
			t.Fatalf("first step deadline = %s, want within (%s, %s]", first, before, after.Add(schemaStepTimeout))
		}
		time.Sleep(10 * time.Millisecond)
		if second := deadline(); !second.After(first) {
			t.Fatalf("the second step reused the first budget: first=%s second=%s", first, second)
		}
	})
	t.Run("a caller's own deadline still covers the sequence", func(t *testing.T) {
		caller, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		want, _ := caller.Deadline()
		if err := schemaStep(caller, func(ctx context.Context) error {
			got, ok := ctx.Deadline()
			if !ok || !got.Equal(want) {
				return fmt.Errorf("step deadline = %s (present=%t), want %s", got, ok, want)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a step that cannot finish ends", func(t *testing.T) {
		caller, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		err := schemaStep(caller, func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("blocked step error = %v, want %v", err, context.DeadlineExceeded)
		}
	})
}

// Version ten extends the exact ownership ledger without inventing provenance
// for version-nine claims. Empty legacy descriptions remain safe and a later
// publication can fill them from its immutable plan snapshot.
func TestVersionNineManagedRouteOwnershipMigratesWithEmptyDescriptions(t *testing.T) {
	const previousVersion = 9
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
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != previousVersion {
		t.Fatalf("version=%d err=%v", version, err)
	}
	populate := []string{
		`INSERT INTO settings(key,value,updated_at_ns) VALUES('refresh_interval','weekly',1)`,
		`INSERT INTO lists(id,name,created_at_ns,updated_at_ns) VALUES('11111111111111111111111111111111','Legacy list',1,1)`,
		`INSERT INTO outputs(id,list_id,target_id,profile_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES('33333333333333333333333333333333','11111111111111111111111111111111','keenetic','keenetic-bat-ipv4-v1','keenetic-route-bat','keenetic-bat-ipv4-v1','legacy',1)`,
		`INSERT INTO managed_route_scopes(endpoint,target_id,interface,created_at_ns,updated_at_ns) VALUES('http://192.168.1.1','keenetic','Wireguard0',1,1)`,
		`INSERT INTO managed_routes(scope_id,prefix,created_by_routevane) VALUES(1,'192.0.2.10/32',1)`,
		`INSERT INTO managed_route_claims(scope_id,output_id,prefix) VALUES(1,'33333333333333333333333333333333','192.0.2.10/32')`,
	}
	for _, statement := range populate {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	var descriptionColumns int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('managed_routes') WHERE name='description'`).Scan(&descriptionColumns); err != nil || descriptionColumns != 0 {
		t.Fatalf("version nine already had route descriptions: count=%d err=%v", descriptionColumns, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("migrating an existing database: %v", err)
	}
	defer store.Close()
	if err := verifySchema(context.Background(), store.db); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("version=%d err=%v", version, err)
	}
	var routeDescription, claimDescription, labelsJSON string
	if err := store.db.QueryRow(`SELECT description FROM managed_routes WHERE scope_id=1 AND prefix='192.0.2.10/32'`).Scan(&routeDescription); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT description,labels_json FROM managed_route_claims WHERE scope_id=1 AND output_id='33333333333333333333333333333333'`).Scan(&claimDescription, &labelsJSON); err != nil {
		t.Fatal(err)
	}
	if routeDescription != "" || claimDescription != "" || labelsJSON != "[]" {
		t.Fatalf("legacy ownership gained invented provenance: route=%q claim=%q labels=%q", routeDescription, claimDescription, labelsJSON)
	}
	loaded, err := store.ManagedRouteOwnership(context.Background(), application.ManagedRouteScope{Endpoint: "http://192.168.1.1", TargetID: "keenetic", Interface: "Wireguard0"})
	if err != nil || len(loaded.Routes) != 1 || len(loaded.Claims) != 1 || loaded.Routes[0].Description != "" || loaded.Claims[0].Description != "" || len(loaded.Claims[0].Labels) != 0 {
		t.Fatalf("migrated ownership=%#v err=%v", loaded, err)
	}
	if _, err := store.db.Exec(`UPDATE managed_routes SET description=? WHERE scope_id=1`, strings.Repeat("é", 49)); err == nil {
		t.Fatal("description over the UTF-8 byte bound was accepted")
	}
	if _, err := store.db.Exec(`UPDATE managed_route_claims SET labels_json='{}' WHERE scope_id=1`); err == nil {
		t.Fatal("non-array claim labels were accepted")
	}
	var value string
	if err := store.db.QueryRow(`SELECT value FROM settings WHERE key='refresh_interval'`).Scan(&value); err != nil || value != "weekly" {
		t.Fatalf("the migration lost stored state: value=%q err=%v", value, err)
	}
}

// Version thirteen renames the schema to the product's one vocabulary
// (ADR 0039). Nothing about it is a shape change, so the only thing worth
// proving is that a populated database arrives intact under the new names:
// every row still there, every foreign key still pointing at its parent, every
// recreated trigger and index still refusing what it refused before, and the
// one retired word that was a stored value rewritten rather than left behind.
func TestVersionTwelveVocabularyMigratesEveryStoredRowInPlace(t *testing.T) {
	const previousVersion = 12
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
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != previousVersion {
		t.Fatalf("version=%d err=%v", version, err)
	}
	profileID := strings.Repeat("1", 32)
	outputID := strings.Repeat("2", 32)
	snapshotID := strings.Repeat("3", 32)
	artifactID := strings.Repeat("4", 32)
	deviceID := strings.Repeat("5", 32)
	tokenID := strings.Repeat("6", 32)
	hash := strings.Repeat("a", 64)
	// Written with the words version twelve used. A test that populated the
	// new names would prove only that the new schema accepts them.
	populate := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO settings(key,value,updated_at_ns) VALUES('refresh_interval','weekly',1)`, nil},
		{`INSERT INTO lists(id,name,refresh_interval,archived_at_ns,created_at_ns,updated_at_ns) VALUES(?,'Legacy list','daily',7,1,1)`, []any{profileID}},
		{`INSERT INTO list_services(list_id,service_id) VALUES(?,'discord')`, []any{profileID}},
		{`INSERT INTO list_categories(list_id,category_id) VALUES(?,'video')`, []any{profileID}},
		{`INSERT INTO list_exclusions(list_id,service_id) VALUES(?,'telegram')`, []any{profileID}},
		{`INSERT INTO list_service_domains(list_id,service_id,domains_json) VALUES(?,'discord','["a.example"]')`, []any{profileID}},
		{`INSERT INTO list_service_priorities(list_id,service_id,position) VALUES(?,'discord',3)`, []any{profileID}},
		{`INSERT INTO library_service_priorities(service_id,position) VALUES('discord',5)`, nil},
		{`INSERT INTO custom_services(id,title,domains_json,created_at_ns,updated_at_ns) VALUES('custom-1234567890abcdef','Custom','["b.example"]',1,1)`, nil},
		{`INSERT INTO service_disabled_sources(service_id,source_id) VALUES('discord','dns')`, nil},
		{`INSERT INTO service_domain_verdicts(service_id,domain,verdict) VALUES('discord','c.example','exclude')`, nil},
		{`INSERT INTO custom_sources(id,service_id,url,format,created_at_ns,updated_at_ns) VALUES('feed-1234567890abcdef','discord','https://example.test/f','text',1,1)`, nil},
		{`INSERT INTO custom_categories(id,title,created_at_ns,updated_at_ns) VALUES('custom-fedcba0987654321','Mine',1,1)`, nil},
		{`INSERT INTO category_memberships(category_id,service_id,state,updated_at_ns) VALUES('video','discord','added',1)`, nil},
		{`INSERT INTO catalog_removals(kind,id,removed_at) VALUES('service','netflix','2026-01-01')`, nil},
		{`INSERT INTO catalog_removals(kind,id,removed_at) VALUES('category','music','2026-01-02')`, nil},
		{`INSERT INTO effective_profiles(profile_key,service_id,target_id,renderer_id,catalog_revision,config_json,updated_at_ns) VALUES('raw-v1','discord','raw-json','raw-json',?,'{}',1)`, []any{hash}},
		{`INSERT INTO resources(id,kind,normalized_value,ip_version,created_at_ns) VALUES(1,'ip','192.0.2.1',4,1)`, nil},
		{`INSERT INTO sightings(id,service_id,component_id,resource_id,source_id,source_class,source_revision,first_seen_ns,last_seen_ns,valid_until_ns,observation_count) VALUES(1,'discord','web',1,'dns','observed','v1',1,1,2,1)`, nil},
		{`INSERT INTO relations(id,source_resource_id,relation_type,target_resource_id,service_id,component_id,first_seen_ns,last_seen_ns,valid_until_ns,source_id,source_revision) VALUES(1,1,'cname_to',1,'discord','web',1,1,2,'dns','v1')`, nil},
		{`INSERT INTO source_runs(id,service_id,source_id,source_revision,started_at_ns,completed_at_ns,status) VALUES(1,'discord','dns','v1',1,1,'success')`, nil},
		{`INSERT INTO devices(id,target_id,name,address,account,created_at_ns,updated_at_ns) VALUES(?,'keenetic','Router','http://192.168.1.1','admin',1,1)`, []any{deviceID}},
		{`INSERT INTO outputs(id,list_id,target_id,profile_key,renderer_id,renderer_version,target_revision,created_at_ns,device_id) VALUES(?,?,'keenetic','keenetic-bat-ipv4-v1','keenetic-route-bat','keenetic-bat-ipv4-v1','rev',1,?)`, []any{outputID, profileID, deviceID}},
		{`INSERT INTO plan_snapshots(id,output_id,routing_plan_hash,routing_plan_json,policy_version,catalog_revision,observation_cutoff_ns,created_at_ns,status) VALUES(?,?,?,?,'auto-v1',?,1,1,'valid')`, []any{snapshotID, outputID, hash, []byte("{}"), hash}},
		{`INSERT INTO artifact_builds(id,output_id,plan_snapshot_id,renderer_id,renderer_version,artifact_hash,artifact_path,size_bytes,content_type,content_created_at_ns,validation_status,status) VALUES(?,?,?,'keenetic-route-bat','keenetic-bat-ipv4-v1',?,'artifacts/published/x.bat',2,'application/x-bat',1,'valid','published')`, []any{artifactID, outputID, snapshotID, hash}},
		{`UPDATE outputs SET latest_artifact_id=? WHERE id=?`, []any{artifactID, outputID}},
		{`INSERT INTO output_attempts(output_id,status,code,projected_rules,maximum_rules,artifact_id,completed_at_ns) VALUES(?,'success','',1,10,?,1)`, []any{outputID, artifactID}},
		{`INSERT INTO subscriptions(output_id,token_id,token_hash,created_at_ns) VALUES(?,?,zeroblob(32),1)`, []any{outputID, tokenID}},
	}
	for _, statement := range populate {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("%s: %v", statement.query, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("migrating an existing database: %v", err)
	}
	defer store.Close()
	if err := verifySchema(context.Background(), store.db); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("version=%d err=%v", version, err)
	}
	// One assertion per renamed name, reading what version twelve wrote
	// through the name version thirteen gives it.
	surviving := []struct {
		query string
		args  []any
		want  string
	}{
		{`SELECT name||'/'||refresh_interval||'/'||archived_at_ns FROM profiles WHERE id=?`, []any{profileID}, "Legacy list/daily/7"},
		{`SELECT list_id FROM profile_lists WHERE profile_id=?`, []any{profileID}, "discord"},
		{`SELECT category_id FROM profile_categories WHERE profile_id=?`, []any{profileID}, "video"},
		{`SELECT list_id FROM profile_exclusions WHERE profile_id=?`, []any{profileID}, "telegram"},
		{`SELECT domains_json FROM profile_list_domains WHERE profile_id=? AND list_id='discord'`, []any{profileID}, `["a.example"]`},
		{`SELECT position FROM profile_list_priorities WHERE profile_id=? AND list_id='discord'`, []any{profileID}, "3"},
		{`SELECT position FROM library_list_priorities WHERE list_id='discord'`, nil, "5"},
		{`SELECT title FROM custom_lists WHERE id='custom-1234567890abcdef'`, nil, "Custom"},
		{`SELECT source_id FROM list_disabled_sources WHERE list_id='discord'`, nil, "dns"},
		{`SELECT verdict FROM list_domain_verdicts WHERE list_id='discord' AND domain='c.example'`, nil, "exclude"},
		{`SELECT url FROM custom_sources WHERE list_id='discord'`, nil, "https://example.test/f"},
		{`SELECT state FROM category_memberships WHERE category_id='video' AND list_id='discord'`, nil, "added"},
		{`SELECT config_json FROM effective_formats WHERE format_key='raw-v1' AND list_id='discord'`, nil, "{}"},
		{`SELECT component_id FROM sightings WHERE list_id='discord'`, nil, "web"},
		{`SELECT relation_type FROM relations WHERE list_id='discord'`, nil, "cname_to"},
		{`SELECT status FROM source_runs WHERE list_id='discord'`, nil, "success"},
		{`SELECT profile_id||'/'||format_key FROM outputs WHERE id=?`, []any{outputID}, profileID + "/keenetic-bat-ipv4-v1"},
		// The one retired word that was stored as a value rather than a name.
		{`SELECT id FROM catalog_removals WHERE kind='list'`, nil, "netflix"},
		{`SELECT id FROM catalog_removals WHERE kind='category'`, nil, "music"},
	}
	for _, check := range surviving {
		var got string
		if err := store.db.QueryRow(check.query, check.args...).Scan(&got); err != nil || got != check.want {
			t.Fatalf("%s -> %q (want %q) err=%v", check.query, got, check.want, err)
		}
	}
	rows, err := store.db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	violations := 0
	for rows.Next() {
		violations++
	}
	if err := rows.Close(); err != nil || violations != 0 {
		t.Fatalf("foreign-key violations after the rename: %d err=%v", violations, err)
	}
	// The recreated objects have to carry their old refusals, not only their
	// new names. Every one of these passed through a DROP and a CREATE.
	refusals := []struct {
		name  string
		query string
		args  []any
	}{
		{"deleting a profile", `DELETE FROM profiles WHERE id=?`, []any{profileID}},
		{"changing a profile identity", `UPDATE profiles SET created_at_ns=9 WHERE id=?`, []any{profileID}},
		{"naming an excluded list", `INSERT INTO profile_lists(profile_id,list_id) VALUES(?,'telegram')`, []any{profileID}},
		{"excluding a named list", `INSERT INTO profile_exclusions(profile_id,list_id) VALUES(?,'discord')`, []any{profileID}},
		{"changing a custom list identity", `UPDATE custom_lists SET created_at_ns=9 WHERE id='custom-1234567890abcdef'`, nil},
		{"changing a custom source owner", `UPDATE custom_sources SET list_id='telegram' WHERE id='feed-1234567890abcdef'`, nil},
		{"changing an output format", `UPDATE outputs SET format_key='other' WHERE id=?`, []any{outputID}},
		{"a second output for one target", `INSERT INTO outputs(id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES(?,?,'keenetic','other','r','v','rev',1)`, []any{strings.Repeat("7", 32), profileID}},
		{"the retired removal kind", `INSERT INTO catalog_removals(kind,id,removed_at) VALUES('service','hulu','2026-01-03')`, nil},
	}
	for _, refusal := range refusals {
		if _, err := store.db.Exec(refusal.query, refusal.args...); err == nil {
			t.Fatalf("the migrated schema accepted %s", refusal.name)
		}
	}
	// The queries the store itself issues have to name the columns the
	// migration produced, which a schema check on its own cannot show.
	profiles, err := store.Profiles(context.Background())
	if err != nil || len(profiles) != 1 || profiles[0].ID != profileID || len(profiles[0].Lists) != 1 || profiles[0].Lists[0] != "discord" {
		t.Fatalf("migrated composition=%#v err=%v", profiles, err)
	}
	output, err := store.OutputBySubscription(context.Background(), tokenID, [32]byte{})
	if err != nil || output.ID != outputID || output.LatestArtifactID != artifactID {
		t.Fatalf("migrated output=%#v err=%v", output, err)
	}
}

func TestSourceCyclesPersistLifecycleRevisionAndRelations(t *testing.T) {
	root := newDataRoot(t)
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	list := "example"
	revision := "dns-revision-1"
	resource, _ := domain.NewAddrResourceFromString("::ffff:192.0.2.1")
	sourceDomain, _ := domain.NewDomainResource("www.example.com")
	targetDomain, _ := domain.NewDomainResource("edge.example.com")
	sighting := domain.Sighting{ListID: list, ComponentID: "web", Resource: resource, SourceID: "dns-main", SourceClass: domain.SourceObserved, SourceRevision: revision, FirstSeen: t0, LastSeen: t0, ValidUntil: t0.Add(2 * time.Hour), ObservationCount: 1, Validity: domain.ValidityValid}
	relation := domain.Relation{SourceResource: sourceDomain, RelationType: domain.RelationCNAMETo, TargetResource: targetDomain, ListID: list, ComponentID: "web", FirstSeen: t0, LastSeen: t0, ValidUntil: t0.Add(2 * time.Hour), SourceID: "dns-main", SourceRevision: revision, Validity: domain.ValidityValid}
	profileJSON, _ := json.Marshal(domain.RawJSONTargetDefinition())
	format := FormatRecord{FormatKey: "raw-v1", ListID: list, TargetID: "raw-json", RendererID: "raw-json", CatalogRevision: strings.Repeat("a", 64), ConfigJSON: profileJSON, UpdatedAt: t0}
	cycle := SuccessCycle{ListID: list, SourceID: "dns-main", SourceRevision: revision, StartedAt: t0, CompletedAt: t0, Sightings: []domain.Sighting{sighting, sighting}, Relations: []domain.Relation{relation, relation}, Format: &format}
	if err := store.ApplySuccess(context.Background(), cycle); err != nil {
		t.Fatal(err)
	}
	store.Close()

	store, err = Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	active := map[string]string{"dns-main": revision}
	snapshot, err := store.ReadPlanningSnapshot(context.Background(), list, active, "raw-v1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Sightings) != 1 || snapshot.Sightings[0].ObservationCount != 1 || snapshot.Sightings[0].TTLKnown || snapshot.Format.CatalogRevision != format.CatalogRevision || len(snapshot.Relations) != 1 {
		t.Fatalf("first snapshot=%#v", snapshot)
	}

	t1 := t0.Add(time.Hour)
	second := sighting
	second.FirstSeen = t0
	second.LastSeen = t1
	second.ValidUntil = t1.Add(2 * time.Hour)
	secondCycle := SuccessCycle{ListID: list, SourceID: "dns-main", SourceRevision: revision, StartedAt: t1, CompletedAt: t1, Sightings: []domain.Sighting{second, second}, Relations: []domain.Relation{{SourceResource: sourceDomain, RelationType: domain.RelationCNAMETo, TargetResource: targetDomain, ListID: list, ComponentID: "web", FirstSeen: t0, LastSeen: t1, ValidUntil: t1.Add(2 * time.Hour), SourceID: "dns-main", SourceRevision: revision}}}
	if err := store.ApplySuccess(context.Background(), secondCycle); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = store.ReadPlanningSnapshot(context.Background(), list, active, "raw-v1", t1)
	if got := snapshot.Sightings[0]; got.ObservationCount != 2 || !got.FirstSeen.Equal(t0) || !got.LastSeen.Equal(t1) || !got.ValidUntil.Equal(t1.Add(2*time.Hour)) {
		t.Fatalf("updated sighting=%#v", got)
	}

	outOfOrder := second
	outOfOrder.LastSeen = t0.Add(30 * time.Minute)
	outOfOrder.ValidUntil = t0.Add(time.Hour)
	if err := store.ApplySuccess(context.Background(), SuccessCycle{ListID: list, SourceID: "dns-main", SourceRevision: revision, StartedAt: t1.Add(time.Minute), CompletedAt: t1.Add(time.Minute), Sightings: []domain.Sighting{outOfOrder}}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = store.ReadPlanningSnapshot(context.Background(), list, active, "raw-v1", t1)
	if got := snapshot.Sightings[0]; !got.LastSeen.Equal(t1) || !got.ValidUntil.Equal(t1.Add(2*time.Hour)) || got.ObservationCount != 3 {
		t.Fatalf("out-of-order cycle moved timestamps: %#v", got)
	}

	if err := store.RecordFailure(context.Background(), FailureCycle{ListID: list, SourceID: "dns-main", SourceRevision: revision, StartedAt: t1.Add(2 * time.Minute), CompletedAt: t1.Add(2 * time.Minute), ErrorCode: "dns_observation_failed"}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = store.ReadPlanningSnapshot(context.Background(), list, active, "raw-v1", t1)
	if snapshot.Sightings[0].ObservationCount != 3 {
		t.Fatal("failed source run changed observation")
	}

	expiry := t1.Add(2 * time.Hour)
	stale, _ := store.ReadPlanningSnapshot(context.Background(), list, active, "raw-v1", expiry)
	if stale.Sightings[0].Validity != domain.ValidityStale || stale.Relations[0].Validity != domain.ValidityStale {
		t.Fatalf("expiry equality lifecycle=%#v %#v", stale.Sightings, stale.Relations)
	}
	archived, _ := store.ReadPlanningSnapshot(context.Background(), list, active, "raw-v1", expiry.Add(domain.StaleRetention))
	if archived.Sightings[0].Validity != domain.ValidityArchived || archived.Relations[0].Validity != domain.ValidityArchived {
		t.Fatalf("archive equality lifecycle=%#v %#v", archived.Sightings, archived.Relations)
	}

	revivalTime := expiry.Add(domain.StaleRetention)
	revived := second
	revived.FirstSeen = revivalTime
	revived.LastSeen = revivalTime
	revived.ValidUntil = revivalTime.Add(2 * time.Hour)
	if err := store.ApplySuccess(context.Background(), SuccessCycle{ListID: list, SourceID: "dns-main", SourceRevision: revision, StartedAt: revivalTime, CompletedAt: revivalTime, Sightings: []domain.Sighting{revived}}); err != nil {
		t.Fatal(err)
	}
	revivedSnapshot, _ := store.ReadPlanningSnapshot(context.Background(), list, active, "raw-v1", revivalTime)
	if got := revivedSnapshot.Sightings[0]; got.Validity != domain.ValidityValid || !got.FirstSeen.Equal(t0) || got.ObservationCount != 4 {
		t.Fatalf("revived=%#v", got)
	}

	revision2 := "dns-revision-2"
	other, _ := domain.NewAddrResourceFromString("192.0.2.2")
	newSighting := domain.Sighting{ListID: list, ComponentID: "web", Resource: other, SourceID: "dns-main", SourceClass: domain.SourceObserved, SourceRevision: revision2, FirstSeen: revivalTime, LastSeen: revivalTime, ValidUntil: revivalTime.Add(time.Hour), ObservationCount: 1}
	if err := store.ApplySuccess(context.Background(), SuccessCycle{ListID: list, SourceID: "dns-main", SourceRevision: revision2, StartedAt: revivalTime, CompletedAt: revivalTime, Sightings: []domain.Sighting{newSighting}}); err != nil {
		t.Fatal(err)
	}
	filtered, _ := store.ReadPlanningSnapshot(context.Background(), list, map[string]string{"dns-main": revision2}, "raw-v1", revivalTime)
	if len(filtered.Sightings) != 1 || filtered.Sightings[0].Resource.CanonicalValue() != "192.0.2.2" || len(filtered.Relations) != 0 {
		t.Fatalf("source revision filtering=%#v", filtered)
	}
	store.Close()
}

func TestDoctorInspectionDoesNotMutateDatabaseFiles(t *testing.T) {
	root := newDataRoot(t)
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	before := fileFingerprints(t, root)
	if report := InspectExisting(context.Background(), root); !report.Healthy {
		t.Fatalf("report=%#v", report)
	}
	after := fileFingerprints(t, root)
	if len(before) != len(after) {
		t.Fatalf("doctor changed files: %#v %#v", before, after)
	}
	for path, hash := range before {
		if after[path] != hash {
			t.Fatalf("doctor mutated %s", path)
		}
	}
}

// pauseFunction is a test-only SQLite function that spends a fixed amount of
// time inside one statement, so a migration sequence can be made longer than a
// single operation's budget without depending on how fast the host is.
const pauseFunction = "routevane_test_pause"

var pauseRegistration = sync.OnceValue(func() error {
	return modern.RegisterScalarFunction(pauseFunction, 0, func(*modern.FunctionContext, []driver.Value) (driver.Value, error) {
		time.Sleep(registeredPause.Load().(time.Duration))
		return int64(1), nil
	})
})

var registeredPause atomic.Value

func registerPause(t *testing.T, pause time.Duration) {
	t.Helper()
	registeredPause.Store(pause)
	if err := pauseRegistration(); err != nil {
		t.Fatal(err)
	}
}

func newDataRoot(t *testing.T) string {
	t.Helper()
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func fileFingerprints(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	result := map[string][32]byte{}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		payload, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		result[entry.Name()] = sha256.Sum256(payload)
	}
	return result
}
