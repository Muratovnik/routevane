package processlock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
)

func TestLockContentionAndRelease(t *testing.T) {
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	if status := Inspect(root); !status.Healthy || !status.Locked {
		t.Fatalf("locked status=%#v", status)
	}
	if _, err := Acquire(root); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Acquire error=%v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if status := Inspect(root); !status.Healthy || status.Locked {
		t.Fatalf("released status=%#v", status)
	}
	second, err := Acquire(root)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	second.Close()
}

// A process started from the retired executable name holds its own lock file.
// Starting the renamed executable against that data directory must refuse
// rather than put a second writer beside it (ADR 0039).
func TestRetiredLockIsHonoredAndReleased(t *testing.T) {
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	retired, err := acquireNamed(root, retiredFileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(root); !errors.Is(err, ErrLocked) {
		t.Fatalf("acquire beside a held retired lock: error=%v, want ErrLocked", err)
	}
	if status := Inspect(root); !status.Healthy || !status.Locked {
		t.Fatalf("status beside a held retired lock=%#v", status)
	}
	if err := retired.Close(); err != nil {
		t.Fatal(err)
	}
	// The file survives its holder. Only the lock it carried decides.
	if status := Inspect(root); !status.Healthy || status.Locked {
		t.Fatalf("status after the retired holder exited=%#v", status)
	}
	lock, err := Acquire(root)
	if err != nil {
		t.Fatalf("acquire after the retired holder exited: %v", err)
	}
	lock.Close()
}

func TestAcquireNeverCreatesTheRetiredLockFile(t *testing.T) {
	root, err := filesystem.EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := os.Lstat(filepath.Join(root, retiredFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired lock file was created: %v", err)
	}
}
