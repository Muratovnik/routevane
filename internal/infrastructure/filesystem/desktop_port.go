package filesystem

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// DesktopPort is transport state owned under the serving data lock. Zero means
// first launch; unreadable state must not silently invalidate subscriptions.
func DesktopPort(dataRoot string) (int, error) {
	payload, err := ReadBoundedFile(filepath.Join(dataRoot, "desktop.port"), 6)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if len(payload) < 2 || payload[len(payload)-1] != '\n' {
		return 0, fmt.Errorf("invalid desktop port")
	}
	port, err := strconv.Atoi(string(payload[:len(payload)-1]))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid desktop port")
	}
	return port, nil
}

// SaveDesktopPort commits the first bound port before any URL can be published.
// The caller owns the data lock. An existing file is never replaced.
func SaveDesktopPort(dataRoot string, port int) error {
	return writeDesktopPort(dataRoot, port, false)
}

// ReplaceDesktopPort requires explicit recovery and the serving data lock.
// Check the expected endpoint so recovery cannot overwrite different state.
func ReplaceDesktopPort(dataRoot string, previous, port int) error {
	current, err := DesktopPort(dataRoot)
	if err != nil {
		return err
	}
	if previous == 0 || current != previous {
		return fmt.Errorf("desktop port changed")
	}
	return writeDesktopPort(dataRoot, port, true)
}

func writeDesktopPort(dataRoot string, port int, replace bool) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid desktop port")
	}
	removeStaleDesktopPortFiles(dataRoot)
	file, err := os.CreateTemp(dataRoot, desktopPortTemp)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	if _, err := fmt.Fprintf(file, "%d\n", port); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	destination := filepath.Join(dataRoot, "desktop.port")
	if replace {
		err = os.Rename(file.Name(), destination)
	} else {
		err = os.Link(file.Name(), destination)
	}
	if err != nil {
		return err
	}
	// Make the new entry durable; the platform helper skips Windows, which
	// offers no directory flush through os.File.
	return syncDirectory(dataRoot)
}

const desktopPortTemp = ".desktop-port-*.tmp"

// removeStaleDesktopPortFiles clears temporary files a crashed writer left
// before its deferred removal. The caller's data lock rules out a live writer.
// Names are matched inside dataRoot rather than globbed through it, so a data
// directory containing pattern characters cannot reach other directories.
// Cleanup is best effort: a leftover file is clutter, not port state.
func removeStaleDesktopPortFiles(dataRoot string) {
	entries, err := os.ReadDir(dataRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if matched, _ := filepath.Match(desktopPortTemp, entry.Name()); matched && entry.Type().IsRegular() {
			_ = os.Remove(filepath.Join(dataRoot, entry.Name()))
		}
	}
}
