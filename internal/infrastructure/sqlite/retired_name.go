package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// retiredDatabaseName is the filename written while the executable was called
// routing-agent (ADR 0039). It is storage, not a compatibility surface: the
// rename below is the only place the old name is still recognized.
const retiredDatabaseName = "routing-agent.db"

// renameRetiredDatabase gives an existing database the product name. It
// checkpoints first, so the write-ahead log can never be separated from the
// file it belongs to, and it refuses to act while another process still holds
// the database. A directory that already has the current name is left alone,
// which also makes a second call a no-op.
func renameRetiredDatabase(ctx context.Context, root string) error {
	current := filepath.Join(root, DatabaseName)
	retired := filepath.Join(root, retiredDatabaseName)
	if _, err := os.Lstat(current); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect database: %w", err)
	}
	if _, err := os.Lstat(retired); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect retired database: %w", err)
	}
	if err := validateSQLiteFiles(retired); err != nil {
		return err
	}
	// A write-ahead log or shared-memory file whose database is gone describes
	// something this rename is not carrying. Adopting a database into that name
	// would let SQLite recover an unrelated log into it, so the operator decides
	// what those files are instead of the migration guessing.
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(current + suffix); err == nil {
			return fmt.Errorf("cannot adopt %s: %s%s belongs to a database that is not here", retiredDatabaseName, DatabaseName, suffix)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect database sidecar: %w", err)
		}
	}
	if err := checkpointRetiredDatabase(ctx, retired); err != nil {
		return err
	}
	if err := os.Rename(retired, current); err != nil {
		return fmt.Errorf("rename retired database: %w", err)
	}
	// A clean close normally removes both sidecars. Move any that survive so a
	// later recovery reads them beside the file they describe.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Rename(retired+suffix, current+suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("rename retired database sidecar: %w", err)
		}
	}
	return nil
}

// checkpointRetiredDatabase folds the write-ahead log back into the database
// and closes it. A busy database means another writer is still running, and
// renaming underneath it would split one database across two names.
func checkpointRetiredDatabase(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open retired database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := bounded(ctx)
	defer cancel()
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return fmt.Errorf("prepare retired database: %w", err)
	}
	var busy, logFrames, checkpointed int
	row := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	if err := row.Scan(&busy, &logFrames, &checkpointed); err != nil {
		_ = db.Close()
		return fmt.Errorf("checkpoint retired database: %w", err)
	}
	if busy != 0 {
		_ = db.Close()
		return fmt.Errorf("retired database is still in use")
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("close retired database: %w", err)
	}
	return nil
}
