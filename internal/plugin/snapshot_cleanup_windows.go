package plugin

import (
	"context"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// Windows can briefly deny deletion after an executable has exited. Go's own
// cmd/internal/robustio handles the same condition; it is internal to Go and
// cannot be imported. Retry only the two relevant Windows errors, within the
// caller's cleanup budget. A persistent lock remains an observable failure.
func removeSnapshot(ctx context.Context, directory string) error {
	return removeSnapshotFiles(ctx, directory, os.RemoveAll)
}

func removeSnapshotFiles(ctx context.Context, directory string, remove func(string) error) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := remove(directory)
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-ticker.C:
		}
	}
}
