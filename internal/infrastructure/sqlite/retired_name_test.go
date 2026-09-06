package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
)

// seedRetiredDatabase writes a real current-schema database, stores one value
// through the ordinary API, and then gives the file the retired name. Building
// it this way keeps the fixture honest: it is a database this build wrote, not
// a hand-assembled file that only resembles one.
func seedRetiredDatabase(t *testing.T, key, value string) string {
	t.Helper()
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutSetting(context.Background(), key, value, time.Unix(0, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, DatabaseName), filepath.Join(root, retiredDatabaseName)); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		err := os.Rename(filepath.Join(root, DatabaseName+suffix), filepath.Join(root, retiredDatabaseName+suffix))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
	return root
}

func TestOpenAdoptsARetiredlyNamedDatabase(t *testing.T) {
	root := seedRetiredDatabase(t, "refresh_interval", "24h")

	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	value, err := store.Setting(context.Background(), "refresh_interval")
	if err != nil || value != "24h" {
		t.Fatalf("setting=%q err=%v, want the value written before the rename", value, err)
	}
	if _, err := os.Lstat(filepath.Join(root, DatabaseName)); err != nil {
		t.Fatalf("database was not adopted under the product name: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, retiredDatabaseName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired database still present: %v", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		path := filepath.Join(root, retiredDatabaseName+suffix)
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("retired sidecar %s still present: %v", suffix, err)
		}
	}
}

// A directory that already holds the current name is not a database to adopt.
// Renaming into it would destroy the newer file, so the retired one is left
// exactly where it is and the current database opens untouched.
func TestOpenKeepsTheCurrentDatabaseWhenBothNamesExist(t *testing.T) {
	root := seedRetiredDatabase(t, "refresh_interval", "24h")

	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutSetting(context.Background(), "refresh_interval", "6h", time.Unix(0, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// Put a retired name back beside the adopted database.
	stale := filepath.Join(root, retiredDatabaseName)
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err = Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	value, err := store.Setting(context.Background(), "refresh_interval")
	if err != nil || value != "6h" {
		t.Fatalf("setting=%q err=%v, want the current database untouched", value, err)
	}
	contents, err := os.ReadFile(stale)
	if err != nil || string(contents) != "stale" {
		t.Fatalf("retired file contents=%q err=%v, want it left alone", contents, err)
	}
}

func TestOpenCreatesAFreshDatabaseWhenNoRetiredNameExists(t *testing.T) {
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := os.Lstat(filepath.Join(root, DatabaseName)); err != nil {
		t.Fatalf("fresh database missing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, retiredDatabaseName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired name must never be created: %v", err)
	}
}

// A sidecar whose database is gone is not part of the database being adopted.
// Recovering it into the adopted file would silently mix two databases, so the
// rename refuses and says which file is in the way.
func TestOpenRefusesToAdoptIntoAnOrphanedSidecar(t *testing.T) {
	root := seedRetiredDatabase(t, "refresh_interval", "24h")
	orphan := filepath.Join(root, DatabaseName+"-wal")
	if err := os.WriteFile(orphan, []byte("orphaned log"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Open(context.Background(), root)
	if err == nil {
		t.Fatal("Open accepted a database beside an orphaned write-ahead log")
	}
	if !strings.Contains(err.Error(), DatabaseName+"-wal") {
		t.Fatalf("error=%v, want it to name the file in the way", err)
	}
	if _, err := os.Lstat(filepath.Join(root, retiredDatabaseName)); err != nil {
		t.Fatalf("refusing must leave the retired database untouched: %v", err)
	}
}
