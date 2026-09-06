package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
)

func TestOpenBacksUpAndImportsLegacyV3(t *testing.T) {
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, DatabaseName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC).UnixNano()
	profileID := strings.Repeat("1", 32)
	snapshotID := strings.Repeat("2", 32)
	artifactID := strings.Repeat("3", 32)
	tokenID := strings.Repeat("4", 32)
	hash := strings.Repeat("a", 64)
	legacySchema := `
PRAGMA foreign_keys=ON;
CREATE TABLE resources(id INTEGER PRIMARY KEY,kind TEXT NOT NULL,normalized_value TEXT NOT NULL,ip_version INTEGER,created_at_ns INTEGER NOT NULL);
CREATE TABLE sightings(id INTEGER PRIMARY KEY,service_id TEXT NOT NULL,component_id TEXT NOT NULL,resource_id INTEGER NOT NULL REFERENCES resources(id),source_id TEXT NOT NULL,source_class TEXT NOT NULL,source_revision TEXT NOT NULL,first_seen_ns INTEGER NOT NULL,last_seen_ns INTEGER NOT NULL,valid_until_ns INTEGER NOT NULL,ttl_seconds INTEGER,observation_count INTEGER NOT NULL,metadata_json TEXT NOT NULL,invalid INTEGER NOT NULL);
CREATE TABLE relations(id INTEGER PRIMARY KEY,source_resource_id INTEGER NOT NULL REFERENCES resources(id),relation_type TEXT NOT NULL,target_resource_id INTEGER NOT NULL REFERENCES resources(id),service_id TEXT NOT NULL,component_id TEXT NOT NULL,first_seen_ns INTEGER NOT NULL,last_seen_ns INTEGER NOT NULL,valid_until_ns INTEGER NOT NULL,source_id TEXT NOT NULL,source_revision TEXT NOT NULL,invalid INTEGER NOT NULL);
CREATE TABLE source_runs(id INTEGER PRIMARY KEY,service_id TEXT NOT NULL,source_id TEXT NOT NULL,source_revision TEXT NOT NULL,started_at_ns INTEGER NOT NULL,completed_at_ns INTEGER NOT NULL,status TEXT NOT NULL,sighting_count INTEGER NOT NULL,relation_count INTEGER NOT NULL,error_code TEXT);
CREATE TABLE effective_profiles(profile_key TEXT NOT NULL,service_id TEXT NOT NULL,target_id TEXT NOT NULL,renderer_id TEXT NOT NULL,catalog_revision TEXT NOT NULL,config_json TEXT NOT NULL,updated_at_ns INTEGER NOT NULL,PRIMARY KEY(profile_key,service_id));
CREATE TABLE profiles(id TEXT PRIMARY KEY,target_id TEXT NOT NULL,profile_key TEXT NOT NULL,renderer_id TEXT NOT NULL,renderer_version TEXT NOT NULL,target_revision TEXT NOT NULL,services_json TEXT NOT NULL,created_at_ns INTEGER NOT NULL,latest_artifact_id TEXT,previous_artifact_id TEXT);
CREATE TABLE plan_snapshots(id TEXT PRIMARY KEY,profile_id TEXT NOT NULL REFERENCES profiles(id),routing_plan_hash TEXT NOT NULL,routing_plan_json BLOB NOT NULL,policy_version TEXT NOT NULL,catalog_revision TEXT NOT NULL,observation_cutoff_ns INTEGER NOT NULL,created_at_ns INTEGER NOT NULL,status TEXT NOT NULL);
CREATE TABLE artifact_builds(id TEXT PRIMARY KEY,profile_id TEXT NOT NULL REFERENCES profiles(id),plan_snapshot_id TEXT NOT NULL REFERENCES plan_snapshots(id),renderer_id TEXT NOT NULL,renderer_version TEXT NOT NULL,artifact_hash TEXT NOT NULL,artifact_path TEXT NOT NULL,size_bytes INTEGER NOT NULL,content_type TEXT NOT NULL,content_created_at_ns INTEGER NOT NULL,validation_status TEXT NOT NULL,status TEXT NOT NULL);
CREATE TABLE subscriptions(profile_id TEXT PRIMARY KEY REFERENCES profiles(id),token_id TEXT NOT NULL,token_hash BLOB NOT NULL,created_at_ns INTEGER NOT NULL);
PRAGMA user_version=3;`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO resources VALUES(1,'ip','192.0.2.1',4,?)`, []any{now}},
		{`INSERT INTO sightings VALUES(1,'discord','web',1,'dns','observed','v1',?,?,?,?,1,'',0)`, []any{now, now, now + int64(time.Hour), 3600}},
		{`INSERT INTO source_runs VALUES(1,'discord','dns','v1',?,?,'success',1,0,NULL)`, []any{now, now}},
		{`INSERT INTO effective_profiles VALUES('raw-v1','discord','raw-json','raw-json',?,'{}',?)`, []any{hash, now}},
		{`INSERT INTO profiles VALUES(?,?,?,?,?,?,?,?,?,NULL)`, []any{profileID, "keenetic", "keenetic-bat-ipv4-v1", "keenetic-route-bat", "keenetic-bat-ipv4-v1", hash, `["discord"]`, now, artifactID}},
		{`INSERT INTO plan_snapshots VALUES(?,?,?,?,?,?,?,?,?)`, []any{snapshotID, profileID, hash, []byte(`{}`), "auto-v1", hash, now, now, "valid"}},
		{`INSERT INTO artifact_builds VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, []any{artifactID, profileID, snapshotID, "keenetic-route-bat", "keenetic-bat-ipv4-v1", hash, "artifacts/published/keenetic-route-bat/" + hash + ".bat", 1, "application/x-bat", now, "valid", "published"}},
		{`INSERT INTO subscriptions VALUES(?,?,zeroblob(32),?)`, []any{profileID, tokenID, now}},
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	lists, err := store.Lists(context.Background())
	if err != nil || len(lists) != 1 || lists[0].ID != profileID || len(lists[0].Services) != 1 || lists[0].Services[0] != "discord" {
		t.Fatalf("imported lists=%#v err=%v", lists, err)
	}
	output, err := store.OutputBySubscription(context.Background(), tokenID, [32]byte{})
	if err != nil || output.ID != profileID || output.LatestArtifactID != artifactID {
		t.Fatalf("imported output=%#v err=%v", output, err)
	}
	attempt, err := store.LatestOutputAttempt(context.Background(), output.ID)
	if err != nil || attempt.Status != "success" || attempt.ArtifactID != artifactID {
		t.Fatalf("imported attempt=%#v err=%v", attempt, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(root, "routevane.legacy-v3-*.db"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("legacy backups=%v err=%v", backups, err)
	}
	sources, err := filepath.Glob(filepath.Join(root, "routevane.legacy-v3-*.db.source"))
	if err != nil || len(sources) != 1 {
		t.Fatalf("legacy sources=%v err=%v", sources, err)
	}
	if version, err := ReadUserVersion(context.Background(), root); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("version=%d err=%v", version, err)
	}
	store, err = Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	after, _ := filepath.Glob(filepath.Join(root, "routevane.legacy-v3-*.db"))
	if len(after) != len(backups) {
		t.Fatalf("reopen created another backup: %v", after)
	}
}
