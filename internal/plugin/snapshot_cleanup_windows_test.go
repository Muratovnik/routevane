package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestSnapshotCleanupRecoversOnlyAfterItsFileLockIsReleased(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(map[bool]string{false: "released", true: "persistent"}[persistent], func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "snapshot")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "plugin.exe")
			if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			name, err := windows.UTF16PtrFromString(path)
			if err != nil {
				t.Fatal(err)
			}
			handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if handle != 0 {
					_ = windows.CloseHandle(handle)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			attempts := 0
			err = removeSnapshotFiles(ctx, directory, func(path string) error {
				attempts++
				removeErr := os.RemoveAll(path)
				if attempts == 1 && !persistent {
					if closeErr := windows.CloseHandle(handle); closeErr != nil {
						t.Fatal(closeErr)
					}
					handle = 0
				}
				return removeErr
			})
			if persistent {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("persistent lock = %v", err)
				}
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("locked file disappeared: %v", err)
				}
			} else {
				if err != nil || attempts < 2 {
					t.Fatalf("released lock = %v, attempts=%d", err, attempts)
				}
				if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("snapshot survived: %v", err)
				}
			}
		})
	}
}
