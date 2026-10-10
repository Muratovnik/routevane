package singboxlocal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
)

func TestProbeRefusesARelativeRuleSetWhoseRuntimeDirectoryIsUnknown(t *testing.T) {
	configurationDirectory := t.TempDir()
	runtimeDirectory := t.TempDir()
	t.Chdir(runtimeDirectory)
	path := filepath.Join(configurationDirectory, "config.json")
	config := map[string]any{"route": map[string]any{"rule_set": []map[string]string{{"tag": "routevane", "type": "local", "format": "source", "path": "rules.json"}}}}
	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDirectory, "rules.json"), []byte(previousPayload), 0o600); err != nil {
		t.Fatal(err)
	}
	device, err := New().Probe(context.Background(), application.Connection{URL: fileURL(path)})
	if !errors.Is(err, ErrConfigUnusable) || device.Interface != "" || device.FormatKey != "" {
		t.Fatalf("an unknown sing-box working directory was guessed: destination=%q err=%v", device.Interface, err)
	}
	if _, err := os.Stat(filepath.Join(configurationDirectory, "rules.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the configuration directory acquired a guessed rule-set file: %v", err)
	}
}

func TestProbeRefusesAVersionDependentRuleSetPathTemplate(t *testing.T) {
	base := newFixture(t, "{tag}.json")
	device, err := New().Probe(context.Background(), base.connection)
	if !errors.Is(err, ErrConfigUnusable) || device.Interface != "" || device.FormatKey != "" {
		t.Fatalf("a version-dependent path template was treated as a literal file: destination=%q err=%v", device.Interface, err)
	}
}
