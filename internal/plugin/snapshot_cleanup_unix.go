//go:build linux || darwin

package plugin

import (
	"context"
	"os"
)

func removeSnapshot(_ context.Context, directory string) error {
	return os.RemoveAll(directory)
}
