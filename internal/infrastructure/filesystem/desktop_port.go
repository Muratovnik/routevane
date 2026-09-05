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
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid desktop port")
	}
	file, err := os.CreateTemp(dataRoot, ".desktop-port-*.tmp")
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
	return os.Link(file.Name(), filepath.Join(dataRoot, "desktop.port"))
}
