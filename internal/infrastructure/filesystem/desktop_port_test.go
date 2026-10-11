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

func TestDesktopPortRecoveryChecksPreviousEndpoint(t *testing.T) {
	root := t.TempDir()
	if err := SaveDesktopPort(root, 43210); err != nil {
		t.Fatal(err)
	}
	for _, previous := range []int{0, 43209} {
		if err := ReplaceDesktopPort(root, previous, 43211); err == nil {
			t.Fatal("recovery accepted different saved state")
		}
	}
	if err := ReplaceDesktopPort(root, 43210, 65536); err == nil {
		t.Fatal("recovery accepted invalid new port")
	}
	if port, err := DesktopPort(root); port != 43210 || err != nil {
		t.Fatalf("failed recovery changed endpoint: %d %v", port, err)
	}
	if err := ReplaceDesktopPort(root, 43210, 43211); err != nil {
		t.Fatal(err)
	}
	if port, err := DesktopPort(root); port != 43211 || err != nil {
		t.Fatalf("recovery was not persisted: %d %v", port, err)
	}
	leftovers, err := filepath.Glob(filepath.Join(root, ".desktop-port-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary port state retained: %v %v", leftovers, err)
	}
}
