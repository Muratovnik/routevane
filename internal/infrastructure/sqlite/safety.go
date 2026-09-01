package sqlite

import (
	"errors"
	"fmt"
	"os"

	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
)

func (s *Store) protectFiles() error {
	if err := validateSQLiteFiles(s.path); err != nil {
		return err
	}
	for _, path := range []string{s.path, s.path + "-wal", s.path + "-shm"} {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := filesystem.ProtectFile(path); err != nil {
			return fmt.Errorf("protect SQLite file: %w", err)
		}
	}
	return nil
}

func validateSQLiteFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect SQLite file: %w", err)
		}
		if !info.Mode().IsRegular() || filesystem.IsLinkOrReparse(info) {
			return fmt.Errorf("SQLite file path is unsafe")
		}
	}
	return nil
}
