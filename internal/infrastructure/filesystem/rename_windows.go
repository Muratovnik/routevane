//go:build windows

package filesystem

import "golang.org/x/sys/windows"

func renameNoReplace(oldPath, newPath string) error {
	from, err := windows.UTF16PtrFromString(oldPath)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(newPath)
	if err != nil {
		return err
	}
	// MoveFile (unlike Go's os.Rename/MoveFileEx with REPLACE_EXISTING) fails
	// if the destination exists, preserving immutable artifact identities.
	return windows.MoveFile(from, to)
}

func syncDirectory(string) error {
	// Windows does not expose directory FlushFileBuffers through os.File. The
	// file itself is flushed before the atomic MoveFile operation.
	return nil
}
