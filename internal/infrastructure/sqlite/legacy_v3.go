package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
)

type legacyProfile struct {
	id, targetID, formatKey, rendererID, rendererVersion, targetRevision string
	lists                                                                []string
	createdAt                                                            int64
	latestArtifactID, previousArtifactID                                 string
}

// migrateLegacyV3 recognizes the pre-profile, pre-release schema by structure,
// snapshots it with SQLite itself, imports it into a fresh current database,
// and only then swaps files. An unknown version-three database remains
// untouched and is refused by the normal compatibility check.
func migrateLegacyV3(ctx context.Context, root, path string) (bool, error) {
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		return false, fmt.Errorf("inspect legacy SQLite: %w", err)
	}
	legacy.SetMaxOpenConns(1)
	legacy.SetMaxIdleConns(1)
	isLegacy, err := legacyV3Fingerprint(ctx, legacy)
	if err != nil {
		_ = legacy.Close()
		return false, err
	}
	if !isLegacy {
		_ = legacy.Close()
		return false, nil
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	if _, err := legacy.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		_ = legacy.Close()
		return false, fmt.Errorf("prepare legacy SQLite: %w", err)
	}
	var busy, logFrames, checkpointed int
	if err := legacy.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil || busy != 0 {
		_ = legacy.Close()
		return false, fmt.Errorf("checkpoint legacy SQLite")
	}
	var quick string
	if err := legacy.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&quick); err != nil || quick != "ok" {
		_ = legacy.Close()
		return false, fmt.Errorf("legacy SQLite integrity check failed")
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	backupName := "routevane.legacy-v3-" + stamp + ".db"
	backupPath := filepath.Join(root, backupName)
	if _, err := legacy.ExecContext(ctx, "VACUUM INTO ?", filepath.ToSlash(backupPath)); err != nil {
		_ = legacy.Close()
		return false, fmt.Errorf("backup legacy SQLite: %w", err)
	}
	if err := legacy.Close(); err != nil {
		return false, fmt.Errorf("close legacy SQLite: %w", err)
	}
	if err := filesystem.ProtectFile(backupPath); err != nil {
		return false, fmt.Errorf("protect legacy backup: %w", err)
	}

	tempName := DatabaseName + ".importing-" + stamp
	tempPath := filepath.Join(root, tempName)
	if err := createImportedDatabase(ctx, tempPath, backupPath); err != nil {
		return false, err
	}
	if err := filesystem.ProtectFile(tempPath); err != nil {
		return false, fmt.Errorf("protect imported SQLite: %w", err)
	}
	rawPath := backupPath + ".source"
	type movedFile struct{ source, destination string }
	moved := make([]movedFile, 0, 3)
	move := func(source, destination string) error {
		if err := os.Rename(source, destination); err != nil {
			return err
		}
		moved = append(moved, movedFile{source: source, destination: destination})
		return nil
	}
	rollbackMoves := func() {
		for index := len(moved) - 1; index >= 0; index-- {
			_ = os.Rename(moved[index].destination, moved[index].source)
		}
	}
	if err := move(path, rawPath); err != nil {
		return false, fmt.Errorf("preserve legacy source database: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := move(path+suffix, rawPath+suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollbackMoves()
			return false, fmt.Errorf("preserve legacy SQLite sidecar: %w", err)
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		rollbackMoves()
		return false, fmt.Errorf("activate imported SQLite: %w", err)
	}
	if err := filesystem.ProtectFile(path); err != nil {
		return false, fmt.Errorf("protect active imported SQLite: %w", err)
	}
	return true, nil
}

func legacyV3Fingerprint(ctx context.Context, db *sql.DB) (bool, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return false, fmt.Errorf("read legacy schema version: %w", err)
	}
	if version != 3 {
		return false, nil
	}
	for table, column := range map[string]string{
		"profiles": "services_json", "subscriptions": "profile_id",
		"plan_snapshots": "profile_id", "artifact_builds": "profile_id",
	} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info(?) WHERE name=?", table, column).Scan(&count); err != nil {
			return false, fmt.Errorf("inspect legacy schema: %w", err)
		}
		if count != 1 {
			return false, nil
		}
	}
	// A version-three database from the released lineage carries the baseline
	// table version one created, which was named lists then and is named
	// profiles now (ADR 0039). Its presence is what separates that database
	// from the pre-release schema this importer accepts, so the check names
	// the historical table and not the current one.
	var baseline int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='lists'").Scan(&baseline); err != nil {
		return false, fmt.Errorf("inspect legacy baseline: %w", err)
	}
	return baseline == 0, nil
}

func createImportedDatabase(ctx context.Context, tempPath, backupPath string) error {
	db, err := sql.Open("sqlite", tempPath)
	if err != nil {
		return fmt.Errorf("create imported SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, path: tempPath}
	failed := func(cause error) error {
		_ = db.Close()
		return cause
	}
	if err := store.initialize(ctx, migrations); err != nil {
		return failed(fmt.Errorf("initialize imported SQLite: %w", err))
	}
	legacyURI := "file:" + filepath.ToSlash(backupPath) + "?mode=ro"
	if _, err := db.ExecContext(ctx, "ATTACH DATABASE ? AS legacy", legacyURI); err != nil {
		return failed(fmt.Errorf("attach legacy backup: %w", err))
	}
	profiles, err := readLegacyProfiles(ctx, db)
	if err != nil {
		return failed(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return failed(fmt.Errorf("begin legacy import: %w", err))
	}
	rollback := func(cause error) error { _ = tx.Rollback(); return failed(cause) }
	for _, statement := range []string{
		`INSERT INTO resources SELECT * FROM legacy.resources`,
		`INSERT INTO sightings SELECT * FROM legacy.sightings`,
		`INSERT INTO relations SELECT * FROM legacy.relations`,
		`INSERT INTO source_runs SELECT * FROM legacy.source_runs`,
		`INSERT INTO effective_formats SELECT * FROM legacy.effective_profiles`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return rollback(fmt.Errorf("import legacy observation state: %w", err))
		}
	}
	for index, profile := range profiles {
		name := legacyProfileName(index, profile)
		if _, err := tx.ExecContext(ctx, `INSERT INTO profiles(id,name,created_at_ns,updated_at_ns) VALUES(?,?,?,?)`, profile.id, name, profile.createdAt, profile.createdAt); err != nil {
			return rollback(fmt.Errorf("import legacy profile: %w", err))
		}
		for _, listID := range profile.lists {
			if _, err := tx.ExecContext(ctx, `INSERT INTO profile_lists(profile_id,list_id) VALUES(?,?)`, profile.id, listID); err != nil {
				return rollback(fmt.Errorf("import legacy profile list: %w", err))
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO outputs(id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns) VALUES(?,?,?,?,?,?,?,?)`, profile.id, profile.id, profile.targetID, profile.formatKey, profile.rendererID, profile.rendererVersion, profile.targetRevision, profile.createdAt); err != nil {
			return rollback(fmt.Errorf("import legacy output: %w", err))
		}
	}
	for _, statement := range []string{
		`INSERT INTO plan_snapshots(id,output_id,routing_plan_hash,routing_plan_json,policy_version,catalog_revision,observation_cutoff_ns,created_at_ns,status) SELECT id,profile_id,routing_plan_hash,routing_plan_json,policy_version,catalog_revision,observation_cutoff_ns,created_at_ns,status FROM legacy.plan_snapshots`,
		`INSERT INTO artifact_builds(id,output_id,plan_snapshot_id,renderer_id,renderer_version,artifact_hash,artifact_path,size_bytes,content_type,content_created_at_ns,validation_status,status) SELECT id,profile_id,plan_snapshot_id,renderer_id,renderer_version,artifact_hash,artifact_path,size_bytes,content_type,content_created_at_ns,validation_status,status FROM legacy.artifact_builds`,
		`INSERT INTO subscriptions(output_id,token_id,token_hash,created_at_ns) SELECT profile_id,token_id,token_hash,created_at_ns FROM legacy.subscriptions`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return rollback(fmt.Errorf("import legacy publication history: %w", err))
		}
	}
	for _, profile := range profiles {
		var latest, previous any
		if profile.latestArtifactID != "" {
			latest = profile.latestArtifactID
		}
		if profile.previousArtifactID != "" {
			previous = profile.previousArtifactID
		}
		if _, err := tx.ExecContext(ctx, `UPDATE outputs SET latest_artifact_id=?,previous_artifact_id=? WHERE id=?`, latest, previous, profile.id); err != nil {
			return rollback(fmt.Errorf("restore legacy publication pointers: %w", err))
		}
		if profile.latestArtifactID != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO output_attempts(output_id,status,code,projected_rules,maximum_rules,artifact_id,completed_at_ns) SELECT ?, 'success','',0,0,id,content_created_at_ns FROM artifact_builds WHERE id=?`, profile.id, profile.latestArtifactID); err != nil {
				return rollback(fmt.Errorf("record imported publication attempt: %w", err))
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return failed(fmt.Errorf("commit legacy import: %w", err))
	}
	if err := verifySchema(ctx, db); err != nil {
		return failed(fmt.Errorf("verify imported SQLite: %w", err))
	}
	var violations int
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return failed(fmt.Errorf("check imported foreign keys: %w", err))
	}
	for rows.Next() {
		violations++
	}
	if err := rows.Close(); err != nil || violations != 0 {
		return failed(fmt.Errorf("imported SQLite has foreign-key violations"))
	}
	if _, err := db.ExecContext(ctx, "DETACH DATABASE legacy"); err != nil {
		return failed(fmt.Errorf("detach legacy backup: %w", err))
	}
	var busy, logFrames, checkpointed int
	if err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil || busy != 0 {
		return failed(fmt.Errorf("checkpoint imported SQLite"))
	}
	var journal string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode = DELETE").Scan(&journal); err != nil || strings.ToLower(journal) != "delete" {
		return failed(fmt.Errorf("finalize imported SQLite"))
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("close imported SQLite: %w", err)
	}
	return nil
}

func readLegacyProfiles(ctx context.Context, db *sql.DB) ([]legacyProfile, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,target_id,profile_key,renderer_id,renderer_version,target_revision,services_json,created_at_ns,coalesce(latest_artifact_id,''),coalesce(previous_artifact_id,'') FROM legacy.profiles ORDER BY created_at_ns,id`)
	if err != nil {
		return nil, fmt.Errorf("read legacy profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	profiles := make([]legacyProfile, 0)
	for rows.Next() {
		var profile legacyProfile
		var listsJSON string
		if err := rows.Scan(&profile.id, &profile.targetID, &profile.formatKey, &profile.rendererID, &profile.rendererVersion, &profile.targetRevision, &listsJSON, &profile.createdAt, &profile.latestArtifactID, &profile.previousArtifactID); err != nil {
			return nil, fmt.Errorf("scan legacy profile: %w", err)
		}
		if !validID(profile.id) || profile.createdAt <= 0 || json.Unmarshal([]byte(listsJSON), &profile.lists) != nil || len(profile.lists) == 0 || len(profile.lists) > 128 {
			return nil, fmt.Errorf("invalid legacy profile")
		}
		profile.lists = domain.StableStrings(profile.lists)
		for _, listID := range profile.lists {
			if domain.ValidateSlug(listID) != nil {
				return nil, fmt.Errorf("invalid legacy profile service")
			}
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read legacy profiles: %w", err)
	}
	return profiles, nil
}

func legacyProfileName(index int, profile legacyProfile) string {
	lists := append([]string(nil), profile.lists...)
	slices.Sort(lists)
	name := fmt.Sprintf("Imported %d · %s · %s", index+1, strings.Join(lists, ", "), profile.targetID)
	if len(name) <= 120 {
		return name
	}
	return name[:120]
}
