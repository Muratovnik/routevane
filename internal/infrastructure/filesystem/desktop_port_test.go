package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopPortPersistsWithoutReplacement(t *testing.T) {
	root := t.TempDir()
	if port, err := DesktopPort(root); port != 0 || err != nil {
		t.Fatalf("first launch: %d %v", port, err)
	}
	if err := SaveDesktopPort(root, 43210); err != nil {
		t.Fatal(err)
	}
	if err := SaveDesktopPort(root, 43211); err == nil {
		t.Fatal("replaced a published subscription port")
	}
	if port, err := DesktopPort(root); port != 43210 || err != nil {
		t.Fatalf("reopen: %d %v", port, err)
	}
}

func TestDesktopPortRejectsCorruptState(t *testing.T) {
	for _, value := range []string{"", "0\n", "65536\n", "43210", "port\n", "1234567\n"} {
		t.Run(value, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "desktop.port"), []byte(value), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := DesktopPort(root); err == nil {
				t.Fatal("corrupt state silently accepted")
			}
		})
	}
}
