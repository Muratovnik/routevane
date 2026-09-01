//go:build !windows

package filesystem

import "os"

func createDirectoryRedirect(target, link string) error {
	return os.Symlink(target, link)
}
