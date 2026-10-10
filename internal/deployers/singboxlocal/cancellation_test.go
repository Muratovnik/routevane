package singboxlocal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

func TestCanceledLocalDeploymentDoesNotReplaceTheOperatorsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(path, []byte(previousPayload), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := New().Deploy(ctx, application.DeviceInfo{Interface: path}, application.Connection{}, artifact(ruleSetPayload))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("canceled deployment returned %v", err)
	}
	payload, err := os.ReadFile(path)
	if err != nil || string(payload) != previousPayload {
		t.Errorf("a canceled deployment replaced the operator's file: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("canceled deployment left a staging file: entries=%d err=%v", len(entries), err)
	}
}
