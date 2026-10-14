package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
)

func TestDeviceForeignKeysSurviveCanceledMutation(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		name := "ordinary connection"
		if interrupt {
			name = "replacement after canceled mutation"
		}
		t.Run(name, func(t *testing.T) {
			store, err := Open(t.Context(), newDataRoot(t))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			device, output := createBoundDevice(t, store)
			if interrupt {
				cancelDeviceMutation(t, store, device)
			}
			if err := verifyPragmas(t.Context(), store.db); err != nil {
				t.Errorf("connection safety after mutation: %v", err)
			}
			if err := store.UpdateOutputDevice(t.Context(), output.ID, "44444444444444444444444444444444"); err == nil {
				t.Error("accepted an output binding to a nonexistent device")
			}
			if err := store.DeleteDevice(t.Context(), device.ID); err != nil {
				t.Fatal(err)
			}
			after, err := store.Output(t.Context(), output.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.DeviceID != "" {
				t.Errorf("deleted device left dangling output binding %q", after.DeviceID)
			}
		})
	}
}

func createBoundDevice(t *testing.T, store *Store) (application.Device, application.Output) {
	t.Helper()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	profile := application.Profile{ID: "11111111111111111111111111111111", Name: "Profile", Lists: []string{"youtube"}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateProfile(t.Context(), profile); err != nil {
		t.Fatal(err)
	}
	device := application.Device{ID: "22222222222222222222222222222222", TargetID: "keenetic", Name: "Before", Address: "http://192.0.2.1", Account: "admin", Interface: "Wireguard0", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateDevice(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	output := application.Output{ID: "33333333333333333333333333333333", ProfileID: profile.ID, TargetID: "keenetic", FormatKey: "keenetic-rci-v1", RendererID: "keenetic-routes-bat", RendererVersion: "1", TargetRevision: "revision", CreatedAt: now}
	if err := store.CreateOutput(t.Context(), application.NewOutput{Output: output}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateOutputDevice(t.Context(), output.ID, device.ID); err != nil {
		t.Fatal(err)
	}
	return device, output
}

// Interrupt a real write at the SQL boundary while owning the driver connection
// through Raw. All driver calls stay inside the callback and run sequentially.
// The finite recursive expression keeps SQLite busy until the context expires;
// IsValid then proves the native interruption happened before pool return.
func cancelDeviceMutation(t *testing.T, store *Store, device application.Device) {
	t.Helper()
	conn, err := store.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = conn.Raw(func(raw any) error {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		_, writeErr := raw.(driver.ExecerContext).ExecContext(ctx, `
UPDATE devices SET name = (
  WITH RECURSIVE work(value) AS (
    VALUES(1) UNION ALL SELECT value+1 FROM work WHERE value<100000000
  )
  SELECT CAST(sum(value) AS TEXT) FROM work
) WHERE id=?`, []driver.NamedValue{{Ordinal: 1, Value: device.ID}})
		if !errors.Is(writeErr, context.DeadlineExceeded) {
			t.Errorf("interrupted SQL mutation: %v", writeErr)
		}
		if raw.(driver.Validator).IsValid() {
			t.Error("test did not reach actual SQLite interruption")
		}
		return nil
	})
	// Closing returns the invalid connection to database/sql, whose ordinary
	// Validator path discards it. Later public operations must open a safe one.
	closeErr := conn.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("interrupt device mutation: %v / %v", err, closeErr)
	}
}

func TestSQLiteFileURIPreservesAbsolutePaths(t *testing.T) {
	for _, path := range []string{
		"/private/a #?&% space/routevane.db",
		"C:/private/a #?&% space/routevane.db",
		"//server/share/a #?&% space/routevane.db",
	} {
		t.Run(path, func(t *testing.T) {
			uri, err := url.Parse(sqliteDSN(filepath.FromSlash(path)))
			if err != nil {
				t.Fatal(err)
			}
			want := path
			if path[0] != '/' {
				want = "/" + path
			}
			if uri.Scheme != "file" || uri.Host != "" || uri.Fragment != "" || uri.Path != want {
				t.Fatalf("filename became URI configuration: %#v", uri)
			}
			if len(uri.Query()) != 1 || len(uri.Query()["_pragma"]) != len(connectionPragmas) {
				t.Fatalf("unexpected URI parameters: %v", uri.Query())
			}
		})
	}
}

func TestOpenPreservesLiteralPrivatePath(t *testing.T) {
	name := "data # & % space"
	if runtime.GOOS != "windows" {
		name += " ?mode=ro&_pragma=foreign_keys(0)"
	}
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	device, _ := createBoundDevice(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if inspection := InspectExisting(t.Context(), root); !inspection.Healthy {
		t.Fatalf("inspect literal filename: %+v", inspection)
	}
	if version, err := ReadUserVersion(t.Context(), root); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("read literal filename: version=%d err=%v", version, err)
	}
	// The supported retired-name transition must address the same literal root.
	if err := os.Rename(filepath.Join(root, DatabaseName), filepath.Join(root, retiredDatabaseName)); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenExisting(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, err := reopened.Device(t.Context(), device.ID); err != nil || got.Name != device.Name {
		t.Fatalf("reopened another database: got=%+v err=%v", got, err)
	}
	if err := verifyPragmas(t.Context(), reopened.db); err != nil {
		t.Fatal(err)
	}
}

func TestOpenPreservesDatabaseFileBoundaryWithLiteralPath(t *testing.T) {
	for _, kind := range []string{"directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "private # & % space"))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, DatabaseName)
			if kind == "directory" {
				err = os.Mkdir(path, 0o700)
			} else {
				outside := filepath.Join(t.TempDir(), "outside.db")
				if err := os.WriteFile(outside, []byte("unrelated file"), 0o600); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(outside, path)
				if err != nil && runtime.GOOS == "windows" {
					t.Skipf("symlink privilege unavailable: %v", err)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if store, err := OpenExisting(t.Context(), root); err == nil {
				_ = store.Close()
				t.Fatal("opened an unsafe database path")
			}
		})
	}
}
