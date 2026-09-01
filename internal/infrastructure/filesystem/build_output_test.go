package filesystem

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestBuildOutputWriterSafeCommitReuseAndCollision(t *testing.T) {
	root, err := EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("route ADD 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n")
	hash := strings.Repeat("a", 64)
	writer := BuildOutputWriter{}
	path, reused, err := writer.Put(context.Background(), root, "", keeneticDescriptor(), hash, payload)
	if err != nil || reused || !filepath.IsAbs(path) {
		t.Fatalf("first output path=%q reused=%v err=%v", path, reused, err)
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}_[a-f0-9]{64}\.bat$`).MatchString(filepath.Base(path)) {
		t.Fatalf("output basename is not controlled by hashes: %q", filepath.Base(path))
	}
	again, reused, err := writer.Put(context.Background(), root, filepath.Dir(path), keeneticDescriptor(), hash, payload)
	if err != nil || !reused || again != path {
		t.Fatalf("identical reuse path=%q reused=%v err=%v", again, reused, err)
	}
	if err := os.WriteFile(path, bytesOf('x', len(payload)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := writer.Put(context.Background(), root, filepath.Dir(path), keeneticDescriptor(), hash, payload); !errors.Is(err, ErrArtifactCollision) {
		t.Fatalf("corrupt collision error=%v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := bytesOf('x', len(payload)); !bytes.Equal(got, want) {
		t.Fatalf("collision overwrote existing bytes: got %q want %q", got, want)
	}
}

func TestBuildOutputWriterRejectsTraversalRootAndLinks(t *testing.T) {
	root, err := EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("route ADD 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n")
	hash := strings.Repeat("b", 64)
	writer := BuildOutputWriter{}
	for name, output := range map[string]string{
		"root":      root,
		"traversal": filepath.Join(root, "..", "outside"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := writer.Put(context.Background(), root, output, keeneticDescriptor(), hash, payload); !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("unsafe output error=%v", err)
			}
		})
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked")
	if err := createDirectoryRedirect(outside, linked); err != nil {
		t.Skipf("symlink/reparse creation unavailable: %v", err)
	}
	if _, _, err := writer.Put(context.Background(), root, linked, keeneticDescriptor(), hash, payload); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("linked output error=%v", err)
	}
}

func TestBuildOutputWriterCancellationAndFailureLeaveNoFinalOrTemp(t *testing.T) {
	payload := []byte("route ADD 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n")
	hash := strings.Repeat("c", 64)
	for _, test := range []struct {
		name   string
		ctx    func() context.Context
		writer func() BuildOutputWriter
	}{
		{
			name: "already canceled",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			writer: func() BuildOutputWriter { return BuildOutputWriter{} },
		},
		{
			name: "injected failure",
			ctx:  context.Background,
			writer: func() BuildOutputWriter {
				return BuildOutputWriter{BeforeRename: func(string) error { return errors.New("injected") }}
			},
		},
		{
			name: "canceled hook error",
			ctx:  context.Background,
			writer: func() BuildOutputWriter {
				return BuildOutputWriter{BeforeRename: func(string) error { return context.Canceled }}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := test.writer().Put(test.ctx(), root, "", keeneticDescriptor(), hash, payload); err == nil {
				t.Fatal("failed/canceled output committed")
			}
			artifacts := filepath.Join(root, "artifacts")
			entries, readErr := os.ReadDir(artifacts)
			if errors.Is(readErr, os.ErrNotExist) {
				return
			}
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("failed output left entries: %v", entries)
			}
		})
	}
	t.Run("context canceled after temp sync", func(t *testing.T) {
		root, err := EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		writer := BuildOutputWriter{BeforeRename: func(string) error {
			cancel()
			return nil
		}}
		if _, _, err := writer.Put(ctx, root, "", keeneticDescriptor(), hash, payload); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error=%v", err)
		}
		entries, err := os.ReadDir(filepath.Join(root, "artifacts"))
		if err != nil || len(entries) != 0 {
			t.Fatalf("canceled output left entries=%v err=%v", entries, err)
		}
	})
}

func TestBuildOutputWriterRejectsOutputDirectorySwapBeforeCommit(t *testing.T) {
	root, err := EnsureDataRoot(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "exports")
	moved := filepath.Join(root, "exports-moved")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	errSwapUnavailable := errors.New("output directory swap unavailable")
	hookCalled := false
	writer := BuildOutputWriter{BeforeRename: func(string) error {
		hookCalled = true
		if err := os.Rename(output, moved); err != nil {
			return fmt.Errorf("%w: rename output directory: %v", errSwapUnavailable, err)
		}
		if err := createDirectoryRedirect(outside, output); err != nil {
			_ = os.Rename(moved, output)
			return fmt.Errorf("%w: create directory link: %v", errSwapUnavailable, err)
		}
		return nil
	}}
	payload := []byte("route ADD 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n")
	_, _, err = writer.Put(context.Background(), root, output, keeneticDescriptor(), strings.Repeat("d", 64), payload)
	if errors.Is(err, errSwapUnavailable) {
		t.Skipf("platform cannot perform the reparse/symlink swap: %v", err)
	}
	if err == nil || !hookCalled {
		t.Fatalf("output directory swap was not rejected: hook=%v err=%v", hookCalled, err)
	}
	outsideEntries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(outsideEntries) != 0 {
		t.Fatalf("output directory swap touched outside files: %v", outsideEntries)
	}
	movedEntries, err := os.ReadDir(moved)
	if err != nil {
		t.Fatal(err)
	}
	if len(movedEntries) != 0 {
		t.Fatalf("failed swapped output left a final or temporary file: %v", movedEntries)
	}
}

func bytesOf(value byte, count int) []byte {
	result := make([]byte, count)
	for i := range result {
		result[i] = value
	}
	return result
}
