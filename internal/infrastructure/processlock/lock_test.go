package processlock

import (
	"errors"
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
