package filesystem

import "os"

// Windows named pipes do not live in ordinary filesystem directories, and
// os.Root rejects reserved device names. Both the path and the opened handle
// are required to be regular files by ReadBoundedFile.
func openInputFile(root *os.Root, name string) (*os.File, error) {
	return root.Open(name)
}
