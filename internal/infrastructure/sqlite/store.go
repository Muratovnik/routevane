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
	DatabaseName     = "routevane.db"
	operationTimeout = 5 * time.Second
	// Bringing a database to the current schema is startup work rather than an
	// interactive query. One migration rewrites a whole schema and commits it
	// with `synchronous = FULL`, and the machine doing it may be slow, busy,
	// or running everything under a race detector, where a single migration
	// has been measured past the budget a query gets. A step of schema work is
	// given an order more time before it is called stuck.
	schemaStepTimeout = time.Minute
	maxMetadataBytes  = 4096
)

var (
	ErrIncompatibleSchema = errors.New("incompatible SQLite schema")
	ErrInvalidCycle       = errors.New("invalid source cycle")
	// ErrFormatNotFound wraps the application's not-found sentinel so a caller
	// can distinguish "never observed yet" from a storage failure without
	// depending on this package.
	ErrFormatNotFound = fmt.Errorf("%w: effective format", application.ErrNotFound)
)

type Store struct {
	db                      *sql.DB
	path                    string
	publicationPreflight    func() error
	configTransferPreflight func() error
}

type FormatRecord = application.FormatRecord
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
	if err := renameRetiredDatabase(ctx, root); err != nil {
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

// initialize brings a database to the current schema. Every part of that is a
// separate database operation: a ping, two inspections, the connection
// pragmas, one transaction for each pending migration and a closing
// verification, and every migration commits with `synchronous = FULL`. Giving
// the whole sequence the budget of a single operation made the time a first
// run is allowed shrink with each migration the schema gains, so a slow or
// busy disk reported an unavailable database instead of finishing an
// initialization that was still making progress. Each step carries the budget
// on its own; a caller that bounds the open keeps its own limit over all of
// them.
func (s *Store) initialize(ctx context.Context, migrationSet []migration) error {
	if err := schemaStep(ctx, s.db.PingContext); err != nil {
		return fmt.Errorf("ping SQLite: %w", err)
	}
	var version int
	if err := schemaStep(ctx, func(ctx context.Context) error {
		return s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	}); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > CurrentSchemaVersion || version < 0 {
		return fmt.Errorf("%w: database version %d", ErrIncompatibleSchema, version)
	}
	if version == 0 {
		var count int
		if err := schemaStep(ctx, func(ctx context.Context) error {
			return s.db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count)
		}); err != nil {
			return fmt.Errorf("inspect empty schema: %w", err)
		}
		if count != 0 {
			return fmt.Errorf("%w: unversioned database is not empty", ErrIncompatibleSchema)
		}
	}
	if err := schemaStep(ctx, func(ctx context.Context) error {
		return configureConnection(ctx, s.db, true)
	}); err != nil {
		return err
	}
	for _, item := range migrationSet {
		if item.version <= version {
			continue
		}
		if item.version != version+1 || item.version > CurrentSchemaVersion {
			return fmt.Errorf("%w: migration sequence", ErrIncompatibleSchema)
		}
		if err := schemaStep(ctx, func(ctx context.Context) error {
			return s.applyMigration(ctx, item)
		}); err != nil {
			return err
		}
		version = item.version
	}
	if version != CurrentSchemaVersion {
		return fmt.Errorf("%w: database version %d", ErrIncompatibleSchema, version)
	}
	if err := schemaStep(ctx, func(ctx context.Context) error {
		return verifySchema(ctx, s.db)
	}); err != nil {
		return err
	}
	return nil
}

// applyMigration either commits one migration or leaves the database exactly
// as it was. The transaction is the unit of work one bound covers.
func (s *Store) applyMigration(ctx context.Context, item migration) error {
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
		// A rename migration depends on SQLite following a renamed table or
		// column into every foreign key, trigger and index that names it,
		// which it stops doing in legacy mode. Refusing the connection here
		// keeps a half-renamed schema from ever being committed.
		"PRAGMA legacy_alter_table = OFF",
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
		{"PRAGMA legacy_alter_table", 0},
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

// countIn answers one question about the schema. A query that fails is a
// failure to read the database and never an answer about its shape: reported
// as an incompatible schema, a timeout on a busy machine told an operator that
// the database this product had just written itself was wrong, and refused to
// open a database that was in fact sound.
func countIn(ctx context.Context, q queryer, query string, args ...any) (int, error) {
	var count int
	if err := q.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("inspect schema: %w", err)
	}
	return count, nil
}

func verifySchema(ctx context.Context, q queryer) error {
	version, err := countIn(ctx, q, "PRAGMA user_version")
	if err != nil {
		return err
	}
	if version != CurrentSchemaVersion {
		return fmt.Errorf("%w: schema version", ErrIncompatibleSchema)
	}
	for _, table := range requiredTables {
		count, err := countIn(ctx, q, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table)
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("%w: required table %s", ErrIncompatibleSchema, table)
		}
		count, err = countIn(ctx, q, "SELECT count(*) FROM pragma_table_info(?)", table)
		if err != nil {
			return err
		}
		if count != len(requiredColumns[table]) {
			return fmt.Errorf("%w: table shape %s", ErrIncompatibleSchema, table)
		}
		for _, column := range requiredColumns[table] {
			count, err := countIn(ctx, q, "SELECT count(*) FROM pragma_table_info(?) WHERE name=?", table, column)
			if err != nil {
				return err
			}
			if count != 1 {
				return fmt.Errorf("%w: required column %s.%s", ErrIncompatibleSchema, table, column)
			}
		}
	}
	for _, trigger := range requiredTriggers {
		count, err := countIn(ctx, q, "SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name=?", trigger)
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("%w: required trigger %s", ErrIncompatibleSchema, trigger)
		}
	}
	// The v4 rename relies on SQLite rewriting the child foreign keys, which it
	// only does while foreign_keys is on. A connection that lost the pragma
	// would migrate silently and leave these pointing at a table that no longer
	// exists, so the target is verified rather than assumed.
	for child, parents := range requiredForeignKeys {
		for _, parent := range parents {
			count, err := countIn(ctx, q, "SELECT count(*) FROM pragma_foreign_key_list(?) WHERE \"table\"=?", child, parent)
			if err != nil {
				return err
			}
			if count < 1 {
				return fmt.Errorf("%w: %s must reference %s", ErrIncompatibleSchema, child, parent)
			}
		}
	}
	applied, err := countIn(ctx, q, "SELECT count(*) FROM schema_migrations WHERE version=?", CurrentSchemaVersion)
	if err != nil {
		return err
	}
	if applied != 1 {
		return fmt.Errorf("%w: migration metadata", ErrIncompatibleSchema)
	}
	applied, err = countIn(ctx, q, "SELECT count(*) FROM schema_migrations")
	if err != nil {
		return err
	}
	if applied != CurrentSchemaVersion {
		return fmt.Errorf("%w: migration history", ErrIncompatibleSchema)
	}
	return nil
}

// schemaStep gives one unit of schema work its own bound, so neither the
// length of the sequence nor the budget a query gets can cut off a database
// that is still making progress, while a step that cannot finish still ends.
func schemaStep(ctx context.Context, run func(context.Context) error) error {
	step, cancel := boundedBy(ctx, schemaStepTimeout)
	defer cancel()
	return run(step)
}

func bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return boundedBy(ctx, operationTimeout)
}

// boundedBy keeps a caller's own deadline when it has one: a caller that
// bounds the whole call decides for everything inside it.
func boundedBy(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, limit)
}

func unixNanos(value int64) time.Time {
	return time.Unix(0, value).UTC()
}
