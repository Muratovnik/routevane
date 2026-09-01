package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
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

// An installation that predates scheduled device bindings migrates one step to
// them and keeps everything it held, including the previous catalog overlay.
// The runner is strictly +1, so this is the only path a stored database can
// take; a schema that arrived any other way is refused by verifySchema rather
// than served.
func TestAnExistingDatabaseMigratesOneStepToScheduledDeviceBindings(t *testing.T) {
	root := newDataRoot(t)
	path := filepath.Join(root, DatabaseName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	previous := &Store{db: db, path: path}
	// The previous schema is the one this change is the successor of. Running
	// it alone leaves the database one version short, which the runner reports
	// rather than serving.
	if err := previous.initialize(context.Background(), migrations[:CurrentSchemaVersion-1]); err == nil {
		t.Fatal("a database short of the current version was accepted")
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != CurrentSchemaVersion-1 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	// The database is populated the way a real installation is: a preference,
	// an operator-created list with its own tuning, and a membership overlay
	// over a shipped category. All of it must survive the step.
	populate := []string{
		`INSERT INTO settings(key,value,updated_at_ns) VALUES('refresh_interval','weekly',1)`,
		`INSERT INTO custom_services(id,title,domains_json,created_at_ns,updated_at_ns) VALUES('custom-1234567890abcdef','Мои сайты','["a.example"]',1,1)`,
		`INSERT INTO service_domain_verdicts(service_id,domain,verdict) VALUES('custom-1234567890abcdef','b.example','include')`,
		`INSERT INTO category_memberships(category_id,service_id,state,updated_at_ns) VALUES('video','custom-1234567890abcdef','added',1)`,
		`INSERT INTO lists(id,name,created_at_ns,updated_at_ns) VALUES('11111111111111111111111111111111','Legacy list',1,1)`,
		`INSERT INTO devices(id,target_id,name,address,account,auto_deliver,created_at_ns,updated_at_ns) VALUES('22222222222222222222222222222222','keenetic','Legacy router','http://192.168.1.1','admin',0,1,1)`,
		`INSERT INTO outputs(id,list_id,target_id,profile_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES('33333333333333333333333333333333','11111111111111111111111111111111','keenetic','keenetic-bat-ipv4-v1','keenetic-route-bat','keenetic-bat-ipv4-v1','legacy',1)`,
	}
	for _, statement := range populate {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	for table, column := range map[string]string{"devices": "interface", "outputs": "device_id"} {
		var present int
		if err := db.QueryRow("SELECT count(*) FROM pragma_table_info(?) WHERE name=?", table, column).Scan(&present); err != nil || present != 0 {
			t.Fatalf("%s.%s existed before its migration: present=%d err=%v", table, column, present, err)
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
	for table, column := range map[string]string{"devices": "interface", "outputs": "device_id"} {
		var present int
		if err := store.db.QueryRow("SELECT count(*) FROM pragma_table_info(?) WHERE name=?", table, column).Scan(&present); err != nil || present != 1 {
			t.Fatalf("%s.%s missing after migration: present=%d err=%v", table, column, present, err)
		}
	}
	var interfaceName string
	if err := store.db.QueryRow(`SELECT interface FROM devices WHERE id='22222222222222222222222222222222'`).Scan(&interfaceName); err != nil || interfaceName != "" {
		t.Fatalf("legacy device interface=%q err=%v", interfaceName, err)
	}
	var binding sql.NullString
	if err := store.db.QueryRow(`SELECT device_id FROM outputs WHERE id='33333333333333333333333333333333'`).Scan(&binding); err != nil || binding.Valid {
		t.Fatalf("legacy output binding=%#v err=%v", binding, err)
	}
	if _, err := store.db.Exec(`UPDATE outputs SET device_id='22222222222222222222222222222222' WHERE id='33333333333333333333333333333333'`); err != nil {
		t.Fatalf("bind migrated output: %v", err)
	}
	if _, err := store.db.Exec(`DELETE FROM devices WHERE id='22222222222222222222222222222222'`); err != nil {
		t.Fatalf("delete migrated device: %v", err)
	}
	if err := store.db.QueryRow(`SELECT device_id FROM outputs WHERE id='33333333333333333333333333333333'`).Scan(&binding); err != nil || binding.Valid {
		t.Fatalf("ON DELETE SET NULL binding=%#v err=%v", binding, err)
	}
	var value string
	if err := store.db.QueryRow(`SELECT value FROM settings WHERE key='refresh_interval'`).Scan(&value); err != nil || value != "weekly" {
		t.Fatalf("the migration lost stored state: value=%q err=%v", value, err)
	}
	services, err := store.CustomServices(context.Background())
	if err != nil || len(services) != 1 || services[0].ID != "custom-1234567890abcdef" {
		t.Fatalf("the migration lost the operator's lists: %#v err=%v", services, err)
	}
	overlay, err := store.CategoryOverlay(context.Background())
	if err != nil || len(overlay.Memberships) != 1 || len(overlay.Removals) != 0 {
		t.Fatalf("the migration lost the overlay: %#v err=%v", overlay, err)
	}
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	// Behavior from the preceding migration remains intact after the new step.
	if err := store.RemoveFromLibrary(context.Background(), application.LibraryRemoval{
		Kind: application.RemovalCategory, ID: "video", Services: []string{"custom-1234567890abcdef"}, RemovedAt: now,
	}); err != nil {
		t.Fatalf("the migrated database refused a removal: %v", err)
	}
	overlay, err = store.CategoryOverlay(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []application.CatalogRemoval{{Kind: application.RemovalCategory, ID: "video", RemovedAt: now}}
	if !reflect.DeepEqual(overlay.Removals, want) || len(overlay.Memberships) != 0 {
		t.Fatalf("overlay after removal = %#v", overlay)
	}
	if services, err := store.CustomServices(context.Background()); err != nil || len(services) != 0 {
		t.Fatalf("the deleted list survived: %#v err=%v", services, err)
	}
	var verdicts int
	if err := store.db.QueryRow(`SELECT count(*) FROM service_domain_verdicts WHERE service_id='custom-1234567890abcdef'`).Scan(&verdicts); err != nil || verdicts != 0 {
		t.Fatalf("the deleted list kept its verdicts: count=%d err=%v", verdicts, err)
	}
}

func TestSourceCyclesPersistLifecycleRevisionAndRelations(t *testing.T) {
	root := newDataRoot(t)
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	service := "example"
	revision := "dns-revision-1"
	resource, _ := domain.NewAddrResourceFromString("::ffff:192.0.2.1")
	sourceDomain, _ := domain.NewDomainResource("www.example.com")
	targetDomain, _ := domain.NewDomainResource("edge.example.com")
	sighting := domain.Sighting{ServiceID: service, ComponentID: "web", Resource: resource, SourceID: "dns-main", SourceClass: domain.SourceObserved, SourceRevision: revision, FirstSeen: t0, LastSeen: t0, ValidUntil: t0.Add(2 * time.Hour), ObservationCount: 1, Validity: domain.ValidityValid}
	relation := domain.Relation{SourceResource: sourceDomain, RelationType: domain.RelationCNAMETo, TargetResource: targetDomain, ServiceID: service, ComponentID: "web", FirstSeen: t0, LastSeen: t0, ValidUntil: t0.Add(2 * time.Hour), SourceID: "dns-main", SourceRevision: revision, Validity: domain.ValidityValid}
	profileJSON, _ := json.Marshal(domain.RawJSONTargetProfile())
	profile := ProfileRecord{ProfileKey: "raw-v1", ServiceID: service, TargetID: "raw-json", RendererID: "raw-json", CatalogRevision: strings.Repeat("a", 64), ConfigJSON: profileJSON, UpdatedAt: t0}
	cycle := SuccessCycle{ServiceID: service, SourceID: "dns-main", SourceRevision: revision, StartedAt: t0, CompletedAt: t0, Sightings: []domain.Sighting{sighting, sighting}, Relations: []domain.Relation{relation, relation}, Profile: &profile}
	if err := store.ApplySuccess(context.Background(), cycle); err != nil {
		t.Fatal(err)
	}
	store.Close()

	store, err = Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	active := map[string]string{"dns-main": revision}
	snapshot, err := store.ReadPlanningSnapshot(context.Background(), service, active, "raw-v1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Sightings) != 1 || snapshot.Sightings[0].ObservationCount != 1 || snapshot.Sightings[0].TTLKnown || snapshot.Profile.CatalogRevision != profile.CatalogRevision || len(snapshot.Relations) != 1 {
		t.Fatalf("first snapshot=%#v", snapshot)
	}

	t1 := t0.Add(time.Hour)
	second := sighting
	second.FirstSeen = t0
	second.LastSeen = t1
	second.ValidUntil = t1.Add(2 * time.Hour)
	secondCycle := SuccessCycle{ServiceID: service, SourceID: "dns-main", SourceRevision: revision, StartedAt: t1, CompletedAt: t1, Sightings: []domain.Sighting{second, second}, Relations: []domain.Relation{{SourceResource: sourceDomain, RelationType: domain.RelationCNAMETo, TargetResource: targetDomain, ServiceID: service, ComponentID: "web", FirstSeen: t0, LastSeen: t1, ValidUntil: t1.Add(2 * time.Hour), SourceID: "dns-main", SourceRevision: revision}}}
	if err := store.ApplySuccess(context.Background(), secondCycle); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = store.ReadPlanningSnapshot(context.Background(), service, active, "raw-v1", t1)
	if got := snapshot.Sightings[0]; got.ObservationCount != 2 || !got.FirstSeen.Equal(t0) || !got.LastSeen.Equal(t1) || !got.ValidUntil.Equal(t1.Add(2*time.Hour)) {
		t.Fatalf("updated sighting=%#v", got)
	}

	outOfOrder := second
	outOfOrder.LastSeen = t0.Add(30 * time.Minute)
	outOfOrder.ValidUntil = t0.Add(time.Hour)
	if err := store.ApplySuccess(context.Background(), SuccessCycle{ServiceID: service, SourceID: "dns-main", SourceRevision: revision, StartedAt: t1.Add(time.Minute), CompletedAt: t1.Add(time.Minute), Sightings: []domain.Sighting{outOfOrder}}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = store.ReadPlanningSnapshot(context.Background(), service, active, "raw-v1", t1)
	if got := snapshot.Sightings[0]; !got.LastSeen.Equal(t1) || !got.ValidUntil.Equal(t1.Add(2*time.Hour)) || got.ObservationCount != 3 {
		t.Fatalf("out-of-order cycle moved timestamps: %#v", got)
	}

	if err := store.RecordFailure(context.Background(), FailureCycle{ServiceID: service, SourceID: "dns-main", SourceRevision: revision, StartedAt: t1.Add(2 * time.Minute), CompletedAt: t1.Add(2 * time.Minute), ErrorCode: "dns_observation_failed"}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = store.ReadPlanningSnapshot(context.Background(), service, active, "raw-v1", t1)
	if snapshot.Sightings[0].ObservationCount != 3 {
		t.Fatal("failed source run changed observation")
	}

	expiry := t1.Add(2 * time.Hour)
	stale, _ := store.ReadPlanningSnapshot(context.Background(), service, active, "raw-v1", expiry)
	if stale.Sightings[0].Validity != domain.ValidityStale || stale.Relations[0].Validity != domain.ValidityStale {
		t.Fatalf("expiry equality lifecycle=%#v %#v", stale.Sightings, stale.Relations)
	}
	archived, _ := store.ReadPlanningSnapshot(context.Background(), service, active, "raw-v1", expiry.Add(domain.StaleRetention))
	if archived.Sightings[0].Validity != domain.ValidityArchived || archived.Relations[0].Validity != domain.ValidityArchived {
		t.Fatalf("archive equality lifecycle=%#v %#v", archived.Sightings, archived.Relations)
	}

	revivalTime := expiry.Add(domain.StaleRetention)
	revived := second
	revived.FirstSeen = revivalTime
	revived.LastSeen = revivalTime
	revived.ValidUntil = revivalTime.Add(2 * time.Hour)
	if err := store.ApplySuccess(context.Background(), SuccessCycle{ServiceID: service, SourceID: "dns-main", SourceRevision: revision, StartedAt: revivalTime, CompletedAt: revivalTime, Sightings: []domain.Sighting{revived}}); err != nil {
		t.Fatal(err)
	}
	revivedSnapshot, _ := store.ReadPlanningSnapshot(context.Background(), service, active, "raw-v1", revivalTime)
	if got := revivedSnapshot.Sightings[0]; got.Validity != domain.ValidityValid || !got.FirstSeen.Equal(t0) || got.ObservationCount != 4 {
		t.Fatalf("revived=%#v", got)
	}

	revision2 := "dns-revision-2"
	other, _ := domain.NewAddrResourceFromString("192.0.2.2")
	newSighting := domain.Sighting{ServiceID: service, ComponentID: "web", Resource: other, SourceID: "dns-main", SourceClass: domain.SourceObserved, SourceRevision: revision2, FirstSeen: revivalTime, LastSeen: revivalTime, ValidUntil: revivalTime.Add(time.Hour), ObservationCount: 1}
	if err := store.ApplySuccess(context.Background(), SuccessCycle{ServiceID: service, SourceID: "dns-main", SourceRevision: revision2, StartedAt: revivalTime, CompletedAt: revivalTime, Sightings: []domain.Sighting{newSighting}}); err != nil {
		t.Fatal(err)
	}
	filtered, _ := store.ReadPlanningSnapshot(context.Background(), service, map[string]string{"dns-main": revision2}, "raw-v1", revivalTime)
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
