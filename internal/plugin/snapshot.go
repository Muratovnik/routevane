package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// stageVerifiedExecutable copies the installed executable through a handle
// rooted in the installation directory, verifies the bytes while copying, and
// returns an operator-private snapshot. Start executes this snapshot instead
// of reopening the plugin-owned path after Load has checked it.
func stageVerifiedExecutable(installed Installed) (path string, directory string, err error) {
	root, err := os.OpenRoot(installed.Directory)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrPluginRoot, err)
	}
	defer func() { _ = root.Close() }()

	source, err := root.Open(installed.Manifest.Executable)
	if err != nil {
		return "", "", fmt.Errorf("%w: executable %q is missing", ErrManifestInvalid, installed.Manifest.Executable)
	}
	defer func() { _ = source.Close() }()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("%w: executable is not a regular file", ErrManifestInvalid)
	}
	if info.Size() <= 0 || info.Size() > MaxExecutableBytes {
		return "", "", fmt.Errorf("%w: executable size %d is outside the bound", ErrManifestInvalid, info.Size())
	}

	directory, err = os.MkdirTemp("", "routevane-plugin-"+installed.Manifest.Name+"-")
	if err != nil {
		return "", "", fmt.Errorf("%w: create execution snapshot: %v", ErrUnavailable, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(directory)
		}
	}()

	path = filepath.Join(directory, installed.Manifest.Executable)
	snapshotRoot, err := os.OpenRoot(directory)
	if err != nil {
		return "", "", fmt.Errorf("%w: open execution snapshot: %v", ErrUnavailable, err)
	}
	defer func() { _ = snapshotRoot.Close() }()
	destination, err := snapshotRoot.OpenFile(installed.Manifest.Executable, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("%w: create execution snapshot: %v", ErrUnavailable, err)
	}
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(destination, digest), io.LimitReader(source, MaxExecutableBytes+1))
	syncErr := destination.Sync()
	closeErr := destination.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		return "", "", fmt.Errorf("%w: write execution snapshot", ErrUnavailable)
	}
	if written != info.Size() || written > MaxExecutableBytes {
		return "", "", fmt.Errorf("%w: executable changed while it was copied", ErrChecksumMismatch)
	}
	if hex.EncodeToString(digest.Sum(nil)) != installed.Manifest.SHA256 {
		return "", "", fmt.Errorf("%w: %s", ErrChecksumMismatch, installed.Manifest.Name)
	}
	// #nosec G302 -- the owner execute bit is required for a private executable;
	// the containing directory and file grant no group or other access.
	if err := snapshotRoot.Chmod(installed.Manifest.Executable, 0o700); err != nil {
		return "", "", fmt.Errorf("%w: prepare execution snapshot: %v", ErrUnavailable, err)
	}
	committed = true
	return path, directory, nil
}
