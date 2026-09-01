package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
)

type Check struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Code    string `json:"code"`
}

type Inspection struct {
	Healthy bool    `json:"healthy"`
	Checks  []Check `json:"checks"`
}

// InspectExisting opens only an existing database in mode=ro. It never
// creates, migrates, repairs, checkpoints, or changes journal mode.
func InspectExisting(ctx context.Context, dataRoot string) Inspection {
	report := Inspection{Healthy: true, Checks: make([]Check, 0, 4)}
	root, err := filesystem.ResolvePrivateDataRoot(dataRoot)
	if err != nil {
		return failedInspection("data_root", "unsafe_data_root")
	}
	path := filepath.Join(root, DatabaseName)
	info, err := os.Lstat(path)
	if err != nil || !isPrivateRegular(info) {
		return failedInspection("database", "database_missing_or_unsafe")
	}
	report.Checks = append(report.Checks, Check{Name: "database", Healthy: true, Code: "ok"})
	if !databaseHeaderUsesWAL(root, DatabaseName) {
		return appendFailure(report, "pragmas", "wal_not_enabled")
	}
	if walInfo, walErr := os.Lstat(path + "-wal"); walErr == nil && !isPrivateRegular(walInfo) {
		return appendFailure(report, "integrity", "wal_unsafe")
	} else if walErr == nil && walInfo.Size() > 0 {
		// immutable read-only mode is what keeps doctor from creating or
		// changing WAL sidecars. It intentionally cannot apply pending WAL
		// frames, so fail closed instead of inspecting a stale main file.
		return appendFailure(report, "integrity", "wal_pending")
	} else if walErr != nil && !os.IsNotExist(walErr) {
		return appendFailure(report, "integrity", "wal_status_failed")
	}
	if shmInfo, shmErr := os.Lstat(path + "-shm"); shmErr == nil && !isPrivateRegular(shmInfo) {
		return appendFailure(report, "integrity", "shm_unsafe")
	} else if shmErr != nil && !os.IsNotExist(shmErr) {
		return appendFailure(report, "integrity", "shm_status_failed")
	}

	uri := "file:" + filepath.ToSlash(path) + "?mode=ro&immutable=1"
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return appendFailure(report, "schema", "database_open_failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := bounded(ctx)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return appendFailure(report, "schema", "database_open_failed")
	}
	if err := configureConnection(ctx, db, false); err != nil {
		return appendFailure(report, "pragmas", "pragma_mismatch")
	}
	report.Checks = append(report.Checks, Check{Name: "pragmas", Healthy: true, Code: "ok"})
	if err := verifySchema(ctx, db); err != nil {
		return appendFailure(report, "schema", "schema_incompatible")
	}
	report.Checks = append(report.Checks, Check{Name: "schema", Healthy: true, Code: "ok"})
	var quick string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&quick); err != nil || quick != "ok" {
		return appendFailure(report, "integrity", "quick_check_failed")
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return appendFailure(report, "integrity", "foreign_key_check_failed")
	}
	hasViolation := rows.Next()
	closeErr := rows.Close()
	if hasViolation || closeErr != nil {
		return appendFailure(report, "integrity", "foreign_key_check_failed")
	}
	report.Checks = append(report.Checks, Check{Name: "integrity", Healthy: true, Code: "ok"})
	return report
}

func databaseHeaderUsesWAL(rootPath, name string) bool {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return false
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()
	header := make([]byte, 20)
	if _, err := io.ReadFull(file, header); err != nil {
		return false
	}
	return string(header[:16]) == "SQLite format 3\x00" && header[18] == 2 && header[19] == 2
}

func isPrivateRegular(info os.FileInfo) bool {
	if !info.Mode().IsRegular() || filesystem.IsLinkOrReparse(info) {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o077 == 0
}

func failedInspection(name, code string) Inspection {
	return Inspection{Healthy: false, Checks: []Check{{Name: name, Healthy: false, Code: code}}}
}

func appendFailure(report Inspection, name, code string) Inspection {
	report.Healthy = false
	report.Checks = append(report.Checks, Check{Name: name, Healthy: false, Code: code})
	return report
}

// ReadUserVersion is a narrow read-only helper used in migration integration
// tests without exposing the database handle.
func ReadUserVersion(ctx context.Context, dataRoot string) (int, error) {
	root, err := filesystem.ResolvePrivateDataRoot(dataRoot)
	if err != nil {
		return 0, err
	}
	path := filepath.Join(root, DatabaseName)
	if _, err := os.Stat(path); err != nil {
		return 0, err
	}
	uri := "file:" + filepath.ToSlash(path) + "?mode=ro&immutable=1"
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	ctx, cancel := bounded(ctx)
	defer cancel()
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}
