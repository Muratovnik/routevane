package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArtifactWriterIsAtomicIdempotentAndNoReplace(t *testing.T) {
	root, err := EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	hash := strings.Repeat("a", 64)
	payload := []byte("{\"ok\":true}\n")
	writer := ArtifactWriter{}
	path, reused, err := writer.Put(context.Background(), root, "raw-json", "example", cutoff, hash, payload)
	if err != nil || reused || !filepath.IsAbs(path) {
		t.Fatalf("first Put path=%q reused=%v err=%v", path, reused, err)
	}
	again, reused, err := writer.Put(context.Background(), root, "raw-json", "example", cutoff, hash, payload)
	if err != nil || !reused || again != path {
		t.Fatalf("second Put path=%q reused=%v err=%v", again, reused, err)
	}

	failed := ArtifactWriter{BeforeRename: func(string) error { return errors.New("injected") }}
	if _, _, err := failed.Put(context.Background(), root, "raw-json", "example", cutoff.Add(time.Second), hash, payload); err == nil {
		t.Fatal("injected pre-rename failure succeeded")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(payload) {
		t.Fatalf("prior artifact changed: %q err=%v", got, err)
	}
	temps, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("temporary files remain: %v err=%v", temps, err)
	}

	if err := os.WriteFile(path, []byte(strings.Repeat("x", len(payload))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := writer.Put(context.Background(), root, "raw-json", "example", cutoff, hash, payload); !errors.Is(err, ErrArtifactCollision) {
		t.Fatalf("collision error=%v", err)
	}
}
