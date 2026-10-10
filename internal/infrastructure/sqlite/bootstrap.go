package sqlite

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"time"
)

// This checkpoint is fixed: later versions still run their ordinary migrations.
// schema_v15.sql preserves the schema produced by replaying migrations 1..15,
// including column order, constraints, indexes and triggers.
const bootstrapSchemaVersion = 15

//go:embed schema_v15.sql
var bootstrapSchemaSQL string

func (s *Store) initializeCurrent(ctx context.Context) error {
	return s.initializeWithBootstrap(ctx, migrations, bootstrapSchemaSQL)
}

// applyBootstrap atomically installs a fixed schema and its migration history.
// The full current-schema verification follows this transaction and any later
// migrations in initializeWithBootstrap, as it does for historical upgrades.
func (s *Store) applyBootstrap(ctx context.Context, schemaSQL string) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire bootstrap connection: %w", err)
	}
	// Cancellation may let Tx.Rollback return ErrTxDone while database/sql's
	// own rollback is still running. Closing the reserved connection waits for
	// that transaction to release it before initialization can return.
	defer func() { _ = conn.Close() }()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var version, objects int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read bootstrap schema version: %w", err)
	}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM main.sqlite_schema").Scan(&objects); err != nil {
		return fmt.Errorf("inspect bootstrap schema: %w", err)
	}
	if version != 0 || objects != 0 {
		return fmt.Errorf("bootstrap SQLite: database changed during initialization")
	}
	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("execute schema bootstrap: %w", err)
	}
	appliedAt := time.Now().UTC().UnixNano()
	for version := 1; version <= bootstrapSchemaVersion; version++ {
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at_ns) VALUES(?, ?)", version, appliedAt); err != nil {
			return fmt.Errorf("record bootstrap migration %d: %w", version, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(bootstrapSchemaVersion)); err != nil {
		return fmt.Errorf("set bootstrap schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema bootstrap: %w", err)
	}
	return nil
}
