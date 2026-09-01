package discovery

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// lockFile opens path exclusively, denying every other handle read, write,
// and delete sharing. This mirrors the file lock a just-closed browser
// process (or an antivirus scan reacting to it) can briefly hold on its own
// profile files on Windows — the exact condition that used to make
// `os.RemoveAll(profileDir)` fail silently. The caller must close the
// returned handle.
func lockFile(t *testing.T, path string) syscall.Handle {
	t.Helper()
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(pathPtr,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0)
	if err != nil {
		t.Fatalf("lock %s: %v", path, err)
	}
	return handle
}

func TestRemoveProfileDirReportsAPersistentWindowsFileLock(t *testing.T) {
	parent := t.TempDir()
	profile := filepath.Join(parent, "profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(profile, "lock.tmp")
	if err := os.WriteFile(locked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	handle := lockFile(t, locked)
	defer func() { _ = syscall.CloseHandle(handle) }()

	err := removeProfileDir(profile)
	if err == nil {
		t.Fatal("removeProfileDir succeeded although a file inside the profile stayed locked for the whole retry window; the failure must be observable, not swallowed")
	}
	if _, statErr := os.Stat(profile); statErr != nil {
		t.Fatalf("a profile that failed to clean up must still be on disk for later diagnosis: %v", statErr)
	}
}

func TestRemoveProfileDirRecoversFromATransientWindowsFileLock(t *testing.T) {
	parent := t.TempDir()
	profile := filepath.Join(parent, "profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(profile, "lock.tmp")
	if err := os.WriteFile(locked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	handle := lockFile(t, locked)

	// The lock clears well inside the retry window, mirroring a browser
	// process finishing its own teardown a few milliseconds after Chrome
	// exits. removeProfileDir's bounded retry is what turns this transient
	// failure into a success instead of a reported error.
	released := make(chan struct{})
	go func() {
		time.Sleep(profileCleanupBackoff / 2)
		_ = syscall.CloseHandle(handle)
		close(released)
	}()
	t.Cleanup(func() { <-released })

	if err := removeProfileDir(profile); err != nil {
		t.Fatalf("removeProfileDir did not recover once the lock was released: %v", err)
	}
	if _, statErr := os.Stat(profile); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("profile survived removal after the lock was released: %v", statErr)
	}
}
