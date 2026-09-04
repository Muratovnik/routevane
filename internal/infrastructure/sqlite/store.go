// Package sqlite owns the durable observation history. Policy decisions and
// artifacts are intentionally absent from this schema.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	_ "modernc.org/sqlite"
)

const (
	DatabaseName     = "routing-agent.db"
	operationTimeout = 5 * time.Second
	maxMetadataBytes = 4096
)

var (
	ErrIncompatibleSchema = errors.New("incompatible SQLite schema")
	ErrInvalidCycle       = errors.New("invalid source cycle")
	// ErrProfileNotFound wraps the application's not-found sentinel so a caller
	// can distinguish "never observed yet" from a storage failure without
	// depending on this package.
	ErrProfileNotFound = fmt.Errorf("%w: effective profile", application.ErrNotFound)
)

type Store struct {
	db                      *sql.DB
	path                    string
	publicationPreflight    func() error
	configTransferPreflight func() error
}

type ProfileRecord = application.ProfileRecord
type SuccessCycle = application.SuccessCycle
type FailureCycle = application.FailureCycle
type PlanningSnapshot = application.PlanningSnapshot
type SourceRunState = application.SourceRunState

func Open(ctx context.Context, dataRoot string) (*Store, error) {
	return open(ctx, dataRoot, true)
}

// OpenExisting refuses to create an empty database for read-oriented commands
// such as build. Compatible existing databases still run owned migrations.
func OpenExisting(ctx context.Context, dataRoot string) (*Store, error) {
	return open(ctx, dataRoot, false)
}

func open(ctx context.Context, dataRoot string, allowCreate bool) (*Store, error) {
	root, err := filesystem.ResolvePrivateDataRoot(dataRoot)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, DatabaseName)
	if info, statErr := os.Lstat(path); statErr == nil {
		if !info.Mode().IsRegular() || filesystem.IsLinkOrReparse(info) {
			return nil, fmt.Errorf("database path is unsafe")
		}
	} else if errors.Is(statErr, os.ErrNotExist) && !allowCreate {
		return nil, fmt.Errorf("database does not exist")
	} else if errors.Is(statErr, os.ErrNotExist) {
		rootHandle, openErr := os.OpenRoot(root)
		if openErr != nil {
			return nil, fmt.Errorf("open data root: %w", openErr)
		}
		file, createErr := rootHandle.OpenFile(DatabaseName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil {
			_ = rootHandle.Close()
			return nil, fmt.Errorf("create private database file: %w", createErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			_ = rootHandle.Close()
			return nil, fmt.Errorf("close new database file: %w", closeErr)
		}
		if closeErr := rootHandle.Close(); closeErr != nil {
			return nil, fmt.Errorf("close data root: %w", closeErr)
		}
		if protectErr := filesystem.ProtectFile(path); protectErr != nil {
			return nil, fmt.Errorf("protect new database file: %w", protectErr)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect database: %w", statErr)
	}
	if err := validateSQLiteFiles(path); err != nil {
		return nil, err
	}
	if _, err := migrateLegacyV3(ctx, root, path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, path: path}
	if err := store.initialize(ctx, migrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.protectFiles(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) initialize(ctx context.Context, migrationSet []migration) error {
	ctx, cancel := bounded(ctx)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping SQLite: %w", err)
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > CurrentSchemaVersion || version < 0 {
		return fmt.Errorf("%w: database version %d", ErrIncompatibleSchema, version)
	}
	if version == 0 {
		var count int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count); err != nil {
			return fmt.Errorf("inspect empty schema: %w", err)
		}
		if count != 0 {
			return fmt.Errorf("%w: unversioned database is not empty", ErrIncompatibleSchema)
		}
	}
	if err := configureConnection(ctx, s.db, true); err != nil {
		return err
	}
	for _, item := range migrationSet {
		if item.version <= version {
			continue
		}
		if item.version != version+1 || item.version > CurrentSchemaVersion {
			return fmt.Errorf("%w: migration sequence", ErrIncompatibleSchema)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", item.version, err)
		}
		failed := func() error {
			if _, err := tx.ExecContext(ctx, item.sql); err != nil {
				return fmt.Errorf("execute migration %d: %w", item.version, err)
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at_ns) VALUES(?, ?)", item.version, time.Now().UTC().UnixNano()); err != nil {
				return fmt.Errorf("record migration %d: %w", item.version, err)
			}
			if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(item.version)); err != nil {
				return fmt.Errorf("set schema version %d: %w", item.version, err)
			}
			return nil
		}()
		if failed != nil {
			_ = tx.Rollback()
			return failed
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", item.version, err)
		}
		version = item.version
	}
	if version != CurrentSchemaVersion {
		return fmt.Errorf("%w: database version %d", ErrIncompatibleSchema, version)
	}
	if err := verifySchema(ctx, s.db); err != nil {
		return err
	}
	return nil
}

func configureConnection(ctx context.Context, db *sql.DB, setWAL bool) error {
	if setWAL {
		var mode string
		if err := db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode); err != nil || strings.ToLower(mode) != "wal" {
			return fmt.Errorf("configure SQLite WAL")
		}
	}
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = FULL",
		"PRAGMA trusted_schema = OFF",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure SQLite connection: %w", err)
		}
	}
	return verifyPragmas(ctx, db)
}

func verifyPragmas(ctx context.Context, q queryer) error {
	checks := []struct {
		query string
		want  int
	}{
		{"PRAGMA foreign_keys", 1},
		{"PRAGMA busy_timeout", 5000},
		{"PRAGMA synchronous", 2},
		{"PRAGMA trusted_schema", 0},
	}
	for _, check := range checks {
		var got int
		if err := q.QueryRowContext(ctx, check.query).Scan(&got); err != nil || got != check.want {
			return fmt.Errorf("SQLite pragma mismatch")
		}
	}
	return nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func verifySchema(ctx context.Context, q queryer) error {
	var version int
	if err := q.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != CurrentSchemaVersion {
		return fmt.Errorf("%w: schema version", ErrIncompatibleSchema)
	}
	for _, table := range requiredTables {
		var count int
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil || count != 1 {
			return fmt.Errorf("%w: required table %s", ErrIncompatibleSchema, table)
		}
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info(?)", table).Scan(&count); err != nil || count != len(requiredColumns[table]) {
			return fmt.Errorf("%w: table shape %s", ErrIncompatibleSchema, table)
		}
		for _, column := range requiredColumns[table] {
			if err := q.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info(?) WHERE name=?", table, column).Scan(&count); err != nil || count != 1 {
				return fmt.Errorf("%w: required column %s.%s", ErrIncompatibleSchema, table, column)
			}
		}
	}
	for _, trigger := range requiredTriggers {
		var count int
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name=?", trigger).Scan(&count); err != nil || count != 1 {
			return fmt.Errorf("%w: required trigger %s", ErrIncompatibleSchema, trigger)
		}
	}
	// The v4 rename relies on SQLite rewriting the child foreign keys, which it
	// only does while foreign_keys is on. A connection that lost the pragma
	// would migrate silently and leave these pointing at a table that no longer
	// exists, so the target is verified rather than assumed.
	for child, parents := range requiredForeignKeys {
		for _, parent := range parents {
			var count int
			if err := q.QueryRowContext(ctx, "SELECT count(*) FROM pragma_foreign_key_list(?) WHERE \"table\"=?", child, parent).Scan(&count); err != nil || count < 1 {
				return fmt.Errorf("%w: %s must reference %s", ErrIncompatibleSchema, child, parent)
			}
		}
	}
	var applied int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version=?", CurrentSchemaVersion).Scan(&applied); err != nil || applied != 1 {
		return fmt.Errorf("%w: migration metadata", ErrIncompatibleSchema)
	}
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&applied); err != nil || applied != CurrentSchemaVersion {
		return fmt.Errorf("%w: migration history", ErrIncompatibleSchema)
	}
	return nil
}

func bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, operationTimeout)
}

func unixNanos(value int64) time.Time {
	return time.Unix(0, value).UTC()
}
