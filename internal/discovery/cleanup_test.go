package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests exercise removeProfileDir directly, without a browser: it is
// the piece that used to be a bare `_ = os.RemoveAll(profileDir)` in both
// LoadPage and RunScenario, and now is the single place a cleanup failure
// becomes observable instead of silently dropped.

func TestRemoveProfileDirRemovesAnOrdinaryProfile(t *testing.T) {
	parent := t.TempDir()
	profile := filepath.Join(parent, "profile")
	if err := os.MkdirAll(filepath.Join(profile, "Default"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "Default", "Cookies"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeProfileDir(profile); err != nil {
		t.Fatalf("removeProfileDir = %v, want nil", err)
	}
	if _, statErr := os.Stat(profile); !os.IsNotExist(statErr) {
		t.Fatalf("profile survived removal: %v", statErr)
	}
}

func TestRemoveProfileDirAcceptsAnAlreadyMissingProfile(t *testing.T) {
	parent := t.TempDir()
	profile := filepath.Join(parent, "never-created")
	if err := removeProfileDir(profile); err != nil {
		t.Fatalf("removeProfileDir on a directory that never existed = %v, want nil", err)
	}
}
