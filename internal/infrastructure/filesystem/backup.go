package filesystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

// MaxBackupBytes bounds one stored device backup.
const MaxBackupBytes = 16 << 20

// BackupStore stores device backups under the data root.
//
// A backup is what makes a rollback possible, so it is written, synced, and read
// back before the deployment that depends on it is allowed to start.
type BackupStore struct {
	DataRoot string
}

func (s BackupStore) PutBackup(ctx context.Context, deployerID string, takenAt time.Time, payload []byte) (application.BackupRef, error) {
	if ctx == nil || ctx.Err() != nil {
		return application.BackupRef{}, fmt.Errorf("invalid backup context")
	}
	if err := validateSegment(deployerID); err != nil {
		return application.BackupRef{}, err
	}
	if len(payload) == 0 || len(payload) > MaxBackupBytes {
		return application.BackupRef{}, fmt.Errorf("device backup size %d is outside the supported bound", len(payload))
	}
	rootPath, err := ResolvePrivateDataRoot(s.DataRoot)
	if err != nil {
		return application.BackupRef{}, err
	}
	digest := sha256.Sum256(payload)
	hash := hex.EncodeToString(digest[:])
	directory := filepath.Join(rootPath, "backups", deployerID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return application.BackupRef{}, fmt.Errorf("create backup directory: %w", err)
	}
	name := takenAt.UTC().Format("20060102T150405.000000000Z") + "_" + hash + ".conf"
	target := filepath.Join(directory, name)
	temporary, err := os.CreateTemp(directory, ".backup.*.tmp")
	if err != nil {
		return application.BackupRef{}, fmt.Errorf("create backup temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if _, err := temporary.Write(payload); err != nil {
		return application.BackupRef{}, fmt.Errorf("write backup: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return application.BackupRef{}, fmt.Errorf("sync backup: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return application.BackupRef{}, fmt.Errorf("close backup: %w", err)
	}
	if err := os.Link(temporaryName, target); err != nil {
		if !os.IsExist(err) {
			return application.BackupRef{}, fmt.Errorf("commit backup: %w", err)
		}
		// The same bytes at the same instant are already stored; that is the
		// same backup, not a conflict.
	}
	committed = true
	if err := os.Remove(temporaryName); err != nil {
		return application.BackupRef{}, fmt.Errorf("remove backup temporary file: %w", err)
	}
	// The stored copy is read back before it is reported as usable: a rollback
	// that discovers a truncated backup has nothing left to restore. The read is
	// scoped to the backup directory through os.Root, so it cannot follow a link
	// out of the data root.
	stored, err := ReadBoundedFile(target, MaxBackupBytes)
	if err != nil {
		return application.BackupRef{}, fmt.Errorf("verify backup: %w", err)
	}
	verified := sha256.Sum256(stored)
	if hex.EncodeToString(verified[:]) != hash {
		return application.BackupRef{}, fmt.Errorf("stored backup does not match what was written")
	}
	relative, err := filepath.Rel(rootPath, target)
	if err != nil {
		return application.BackupRef{}, fmt.Errorf("resolve backup path: %w", err)
	}
	return application.BackupRef{
		ID:        hash,
		Path:      filepath.ToSlash(relative),
		Hash:      hash,
		SizeBytes: int64(len(stored)),
		CreatedAt: takenAt.UTC(),
	}, nil
}
