//go:build !windows

package filesystem

import (
	"os"

	"golang.org/x/sys/unix"
)

// O_NONBLOCK makes a FIFO swapped in after the path inspection open without a
// writer. ReadBoundedFile then rejects that handle before reading any bytes.
// The flag does not change regular-file reads, and os.Root still owns traversal.
func openInputFile(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
