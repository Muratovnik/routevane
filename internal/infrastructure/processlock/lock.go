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

const FileName = "routing-agent.run.lock"

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
	path := filepath.Join(root, FileName)
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
	file, err := rootHandle.OpenFile(FileName, os.O_RDWR|os.O_CREATE, 0o600)
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

func closeFileWith(file *os.File, cause error) error {
	if closeErr := file.Close(); closeErr != nil {
		return errors.Join(cause, closeErr)
	}
	return cause
}
