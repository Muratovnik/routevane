package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSessionEvidenceStaysPrivateAndDoesNotOverwrite(t *testing.T) {
	root, err := EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	path, err := WriteSessionEvidence(t.Context(), root, "example", when, map[string]string{"host": "first.example"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteSessionEvidence(t.Context(), root, "example", when, map[string]string{"host": "replacement.example"}); err == nil {
		t.Fatal("evidence for the same instant overwrote an existing document")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]string
	if err := json.Unmarshal(payload, &stored); err != nil || stored["host"] != "first.example" {
		t.Fatalf("stored evidence=%q, error=%v", payload, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("temporary or replacement files remain: %v, error=%v", entries, err)
	}
	if runtime.GOOS != "windows" {
		for _, name := range []string{filepath.Dir(filepath.Dir(path)), filepath.Dir(path), path} {
			info, err := os.Stat(name)
			if err != nil || info.Mode().Perm()&0o077 != 0 {
				t.Fatalf("evidence path is not private: %s, error=%v", name, err)
			}
		}
	}
}

func TestSessionEvidenceRefusesNestedDirectoryRedirects(t *testing.T) {
	for _, relative := range []string{"discovery", filepath.Join("discovery", "example")} {
		t.Run(relative, func(t *testing.T) {
			base := t.TempDir()
			root, err := EnsureDataRoot(filepath.Join(base, "data"))
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(base, "outside")
			if err := os.Mkdir(outside, 0o700); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, relative)
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := createDirectoryRedirect(outside, link); err != nil {
				t.Skipf("directory redirects unavailable: %v", err)
			}
			_, err = WriteSessionEvidence(t.Context(), root, "example", time.Now(), map[string]string{"host": "example.com"})
			if !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("nested directory redirect error=%v, want unsafe path", err)
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("write escaped the data root: %v, error=%v", entries, err)
			}
		})
	}
}

type cancelEvidenceEncoding struct{ cancel context.CancelFunc }

func (e cancelEvidenceEncoding) MarshalJSON() ([]byte, error) {
	e.cancel()
	return []byte(`{"host":"example.com"}`), nil
}

func TestSessionEvidenceDoesNotCommitAfterCancellation(t *testing.T) {
	root, err := EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err = WriteSessionEvidence(ctx, root, "example", time.Now(), cancelEvidenceEncoding{cancel})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled evidence error=%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "discovery", "example"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("canceled write left evidence: %v", entries)
	}
}

func TestReadBoundedFileReadsOnlyAConfinedRegularFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "capture.har")
	if err := os.WriteFile(path, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadBoundedFile(path, 7)
	if err != nil || string(got) != "archive" {
		t.Fatalf("exact-bound read=%q, error=%v", got, err)
	}
	if _, err := ReadBoundedFile(path, 6); err == nil {
		t.Fatal("oversized input was accepted")
	}
	if _, err := ReadBoundedFile(directory, 7); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("directory input error=%v", err)
	}
	outside := filepath.Join(t.TempDir(), "capture.har")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "linked.har")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("file symlinks unavailable: %v", err)
	}
	if _, err := ReadBoundedFile(link, 7); err == nil {
		t.Fatal("input symlink escaped its owning directory")
	}
}
