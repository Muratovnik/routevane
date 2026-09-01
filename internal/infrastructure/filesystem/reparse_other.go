//go:build !windows

package filesystem

import "os"

func IsLinkOrReparse(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}
