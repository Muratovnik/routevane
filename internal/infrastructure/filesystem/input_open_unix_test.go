//go:build !windows

package filesystem

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadBoundedFileRefusesFIFOWithoutWaiting(t *testing.T) {
	for _, mode := range []string{"read", "open"} {
		t.Run(mode, func(t *testing.T) {
			fifo := filepath.Join(t.TempDir(), "capture.har")
			if err := unix.Mkfifo(fifo, 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			// A separate process keeps a regressed blocking open from leaking a
			// goroutine or hanging the suite. No writer ever opens this FIFO.
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReadBoundedFIFOProcess$")
			command.Env = append(os.Environ(), "ROUTEVANE_TEST_FIFO="+fifo, "ROUTEVANE_TEST_FIFO_MODE="+mode)
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatal("input boundary waited for a FIFO writer")
			}
			if err != nil {
				t.Fatalf("FIFO process: %v\n%s", err, output)
			}
		})
	}
}

func TestReadBoundedFIFOProcess(t *testing.T) {
	path := os.Getenv("ROUTEVANE_TEST_FIFO")
	if path == "" {
		t.Skip("subprocess-only FIFO check")
	}
	if os.Getenv("ROUTEVANE_TEST_FIFO_MODE") == "read" {
		if _, err := ReadBoundedFile(path, 1024); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("FIFO error=%v, want unsafe path", err)
		}
		return
	}
	// Exercise the open primitive separately: it must stay nonblocking if a
	// regular path is swapped for a FIFO after ReadBoundedFile's first Stat.
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := openInputFile(root, filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("opened handle is not the FIFO: %v", err)
	}
}
