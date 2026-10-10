package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/infrastructure/processlock"
)

type lockedDeviceDialer struct{ calls atomic.Int64 }

func (d *lockedDeviceDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	d.calls.Add(1)
	return nil, errors.New("unexpected device request while data root is locked")
}

func TestOneShotWritersRespectTheDataRootLock(t *testing.T) {
	catalog := filepath.Join("..", "..", "catalog")
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := processlock.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	dialer := &lockedDeviceDialer{}
	deps := runtimeDeps{DeviceDialer: dialer}
	for _, args := range [][]string{
		{"refresh", "--list", "youtube"},
		{"build", "--target", "raw-json", "--list", "youtube"},
		{"deploy", "--artifact", "existing-artifact", "--target", "keenetic", "--device", "http://192.168.1.1", "--confirm"},
		{"discover", "--url", "example.com", "--list-id", "blocked-draft", "--confirm"},
		{"learn", "--url", "example.com", "--har", filepath.Join(root, "not-read.har"), "--list-id", "blocked-draft", "--confirm"},
	} {
		t.Run(args[0], func(t *testing.T) {
			command := append(append([]string(nil), args...), "--catalog-dir", catalog, "--data-dir", root)
			var stdout, stderr bytes.Buffer
			if code := runWithDeps(&stdout, &stderr, command, deps); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"error_code":"data_lock_unavailable"`) {
				t.Fatalf("locked command code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
		})
	}
	if got := dialer.calls.Load(); got != 0 {
		t.Fatalf("locked commands contacted device %d times", got)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != processlock.FileName {
		t.Fatalf("locked commands changed data directory: %v", entries)
	}
}

func TestUnconfirmedCommandsRemainAvailableWhileDataRootIsLocked(t *testing.T) {
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := processlock.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	for _, args := range [][]string{
		{"deploy", "--artifact", "existing-artifact", "--target", "keenetic", "--device", "http://192.168.1.1"},
		{"discover", "--url", "example.com"},
		{"learn", "--url", "example.com", "--har", filepath.Join(root, "not-read.har")},
	} {
		t.Run(args[0], func(t *testing.T) {
			command := append(append([]string(nil), args...), "--catalog-dir", filepath.Join("..", "..", "catalog"), "--data-dir", root)
			result := runCommand(t, runtimeDeps{}, command, 0)
			if !strings.Contains(result.stderr, `"error_code":"confirmation_required"`) || !strings.Contains(result.stdout, "--confirm") {
				t.Fatalf("unconfirmed command did not show review: %#v", result)
			}
		})
	}
	var stdout, stderr bytes.Buffer
	if code := runWithDeps(&stdout, &stderr, []string{"preview", "--list", "example", "--target", "raw-json"}, runtimeDeps{Resolver: &commandFakeResolver{}}); code != 0 || stdout.Len() == 0 {
		t.Fatalf("preview code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
