// Package processlock owns the one advisory scheduler lock. One-shot commands
// rely on SQLite WAL and never acquire it.
package processlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
)

const FileName = "routevane.run.lock"

// retiredFileName is the lock a process started from the retired routing-agent
// executable still holds (ADR 0039). Refusing while it is held is what keeps an
// interrupted upgrade from putting two writers on one data directory.
const retiredFileName = "routing-agent.run.lock"

var ErrLocked = errors.New("scheduler is already running")

type Lock struct {
	file *os.File
}

type Status struct {
	Healthy bool   `json:"healthy"`
	Locked  bool   `json:"locked"`
	Code    string `json:"code"`
}

func Acquire(dataRoot string) (*Lock, error) {
	root, err := filesystem.ResolvePrivateDataRoot(dataRoot)
	if err != nil {
		return nil, err
	}
	if held, err := retiredLockHeld(root); err != nil {
		return nil, err
	} else if held {
		return nil, ErrLocked
	}
	return acquireNamed(root, FileName)
}

// acquireNamed owns the one file-locking sequence. Acquire supplies the current
// name; a test supplies the retired one to stand in for a process that has not
// been upgraded yet.
func acquireNamed(root, name string) (*Lock, error) {
	path := filepath.Join(root, name)
	if info, statErr := os.Lstat(path); statErr == nil {
		if !info.Mode().IsRegular() || filesystem.IsLinkOrReparse(info) {
			return nil, fmt.Errorf("scheduler lock path is unsafe")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open scheduler root: %w", err)
	}
	file, err := rootHandle.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		_ = rootHandle.Close()
		return nil, fmt.Errorf("open scheduler lock: %w", err)
	}
	if err := rootHandle.Close(); err != nil {
		return nil, closeFileWith(file, fmt.Errorf("close scheduler root: %w", err))
	}
	if err := filesystem.ProtectFile(path); err != nil {
		return nil, closeFileWith(file, err)
	}
	locked, err := tryLockFile(file)
	if err != nil {
		return nil, closeFileWith(file, fmt.Errorf("acquire scheduler lock: %w", err))
	}
	if !locked {
		return nil, closeFileWith(file, ErrLocked)
	}
	return &Lock{file: file}, nil
}

func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unlockFile(l.file)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return err
	}
	return closeErr
}

// Inspect never creates a lock file. If one exists, it briefly attempts the
// same nonblocking advisory lock and releases it immediately when available.
func Inspect(dataRoot string) Status {
	root, err := filesystem.ResolvePrivateDataRoot(dataRoot)
	if err != nil {
		return Status{Healthy: false, Code: "unsafe_data_root"}
	}
	if held, retiredErr := retiredLockHeld(root); retiredErr != nil {
		return Status{Healthy: false, Code: "lock_status_failed"}
	} else if held {
		return Status{Healthy: true, Locked: true, Code: "locked"}
	}
	path := filepath.Join(root, FileName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Status{Healthy: true, Locked: false, Code: "unlocked"}
	}
	if err != nil || !info.Mode().IsRegular() || filesystem.IsLinkOrReparse(info) {
		return Status{Healthy: false, Code: "unsafe_lock_file"}
	}
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return Status{Healthy: false, Code: "lock_status_failed"}
	}
	file, err := rootHandle.OpenFile(FileName, os.O_RDWR, 0)
	if err != nil {
		_ = rootHandle.Close()
		return Status{Healthy: false, Code: "lock_status_failed"}
	}
	if err := rootHandle.Close(); err != nil {
		_ = file.Close()
		return Status{Healthy: false, Code: "lock_status_failed"}
	}
	defer file.Close()
	locked, err := tryLockFile(file)
	if err != nil {
		return Status{Healthy: false, Code: "lock_status_failed"}
	}
	if !locked {
		return Status{Healthy: true, Locked: true, Code: "locked"}
	}
	if err := unlockFile(file); err != nil {
		return Status{Healthy: false, Code: "lock_status_failed"}
	}
	return Status{Healthy: true, Locked: false, Code: "unlocked"}
}

// retiredLockHeld reports whether a process started under the retired
// executable name still holds its own lock. Only the lock decides; the file
// itself outlives its holder and an upgraded data directory keeps carrying the
// empty one. Removing it is not this function's call, because a file it does
// not own may be held by a process it cannot see.
func retiredLockHeld(root string) (bool, error) {
	path := filepath.Join(root, retiredFileName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || filesystem.IsLinkOrReparse(info) {
		return false, fmt.Errorf("retired scheduler lock path is unsafe")
	}
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return false, fmt.Errorf("open scheduler root: %w", err)
	}
	file, err := rootHandle.OpenFile(retiredFileName, os.O_RDWR, 0)
	if err != nil {
		_ = rootHandle.Close()
		return false, fmt.Errorf("open retired scheduler lock: %w", err)
	}
	if err := rootHandle.Close(); err != nil {
		return false, closeFileWith(file, fmt.Errorf("close scheduler root: %w", err))
	}
	locked, err := tryLockFile(file)
	if err != nil {
		return false, closeFileWith(file, fmt.Errorf("inspect retired scheduler lock: %w", err))
	}
	if !locked {
		return true, file.Close()
	}
	if err := unlockFile(file); err != nil {
		return false, closeFileWith(file, fmt.Errorf("release retired scheduler lock: %w", err))
	}
	return false, file.Close()
}

func closeFileWith(file *os.File, cause error) error {
	if closeErr := file.Close(); closeErr != nil {
		return errors.Join(cause, closeErr)
	}
	return cause
}
