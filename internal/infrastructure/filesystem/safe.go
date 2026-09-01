// Package filesystem owns the local data-root and artifact file boundary.
package filesystem

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var ErrUnsafePath = errors.New("unsafe filesystem path")

// ResolveRoot resolves and validates a root without creating it.
func ResolveRoot(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: empty root", ErrUnsafePath)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%w: resolve root", ErrUnsafePath)
	}
	abs = filepath.Clean(abs)
	if isVolumeRoot(abs) {
		return "", fmt.Errorf("%w: volume root is not allowed", ErrUnsafePath)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("inspect root: %w", err)
	}
	if !info.IsDir() || IsLinkOrReparse(info) {
		return "", fmt.Errorf("%w: root is not a plain directory", ErrUnsafePath)
	}
	if err := checkNoLinksInPath(abs); err != nil {
		return "", err
	}
	return abs, nil
}

func ResolvePrivateDataRoot(path string) (string, error) {
	root, err := ResolveRoot(path)
	if err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(root)
		if err != nil || info.Mode().Perm()&0o077 != 0 {
			return "", fmt.Errorf("%w: data root permissions are not private", ErrUnsafePath)
		}
	}
	return root, nil
}

// EnsureDataRoot creates only the requested data root and then validates its
// resolved identity. POSIX mode hardening is best-effort on Windows because
// chmod is not an ACL boundary there.
func EnsureDataRoot(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: empty root", ErrUnsafePath)
	}
	abs, err := filepath.Abs(path)
	if err != nil || isVolumeRoot(filepath.Clean(abs)) {
		return "", fmt.Errorf("%w: invalid data root", ErrUnsafePath)
	}
	if info, statErr := os.Lstat(abs); statErr == nil {
		if !info.IsDir() || IsLinkOrReparse(info) {
			return "", fmt.Errorf("%w: data root is not a plain directory", ErrUnsafePath)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("inspect data root: %w", statErr)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return "", fmt.Errorf("create data root: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := chmodPrivateDir(abs); err != nil {
			return "", fmt.Errorf("protect data root: %w", err)
		}
	}
	return ResolvePrivateDataRoot(abs)
}

func EnsurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || IsLinkOrReparse(info) {
		return fmt.Errorf("%w: artifact directory is not plain", ErrUnsafePath)
	}
	if runtime.GOOS != "windows" {
		return chmodPrivateDir(path)
	}
	return nil
}

func chmodPrivateDir(path string) error {
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	return parent.Chmod(filepath.Base(path), 0o700)
}

func ProtectFile(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return os.Chmod(path, 0o600)
}

func CheckOptionalPlainDir(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || IsLinkOrReparse(info) {
		return fmt.Errorf("%w: directory is not plain", ErrUnsafePath)
	}
	return nil
}

// InspectArtifactTree is read-only and bounded. It rejects links, reparse
// points, devices, sockets, and unexpected non-regular entries anywhere below
// the optional artifacts directory.
func InspectArtifactTree(dataRoot string) error {
	root, err := ResolvePrivateDataRoot(dataRoot)
	if err != nil {
		return err
	}
	artifacts := filepath.Join(root, "artifacts")
	if err := CheckOptionalPlainDir(artifacts); err != nil {
		return err
	}
	if _, err := os.Lstat(artifacts); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	visited := 0
	return filepath.Walk(artifacts, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		visited++
		if visited > 10000 {
			return fmt.Errorf("%w: artifact tree entry bound exceeded", ErrUnsafePath)
		}
		if IsLinkOrReparse(info) || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("%w: unsafe artifact entry", ErrUnsafePath)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("%w: artifact entry permissions are not private", ErrUnsafePath)
		}
		return nil
	})
}

func isVolumeRoot(path string) bool {
	volume := filepath.VolumeName(path)
	remainder := strings.TrimPrefix(filepath.Clean(path), volume)
	return remainder == string(filepath.Separator) || remainder == ""
}

func checkNoLinksInPath(path string) error {
	volume := filepath.VolumeName(path)
	base := volume + string(filepath.Separator)
	remainder := strings.TrimPrefix(filepath.Clean(path), base)
	current := base
	for _, part := range strings.Split(remainder, string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect path component: %w", err)
		}
		if IsLinkOrReparse(info) {
			return fmt.Errorf("%w: root traverses a link", ErrUnsafePath)
		}
	}
	return nil
}
