package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/deployers/singboxlocal"
	"github.com/Muratovnik/routevane/internal/renderers/singbox"
)

// TestAppliesAPublishedRuleSetToALocalSingBox is the visible outcome of the
// local sing-box slice: a published artifact reaches the file the operator's own
// configuration declares and is verified there, and every refusal happens before
// that file is touched.
//
// Rollback is exercised where it can be made to fail deterministically: the
// deployer's own tests restore both a previous rule set and an absent one, and
// the application lifecycle test covers the ordering.
func TestAppliesAPublishedRuleSetToALocalSingBox(t *testing.T) {
	catalog := filepath.Join("..", "..", "testdata", "expiry", "catalog")
	data := filepath.Join(t.TempDir(), "data")
	singBoxDir := t.TempDir()
	configPath := filepath.Join(singBoxDir, "config.json")
	ruleSetPath := filepath.Join(singBoxDir, "routevane.json")
	writeLocalSingBoxConfig(t, configPath, "routevane.json")

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}})

	origin, cancel, done, stderr := startServeServerWithHostResolver(t, catalog, data, resolver, func() time.Time { return now })
	defer func() {
		cancel()
		if code := <-done; code != 0 {
			t.Fatalf("server code=%d stderr=%s", code, stderr.String())
		}
	}()

	profileID, outputID, _ := createProfileOutput(t, origin, "YouTube", "singbox", "youtube")
	build := refreshAndBuild(t, origin, profileID, outputID)
	if build.Artifact.ID == "" {
		t.Fatalf("build = %#v", build)
	}

	deps := runtimeDeps{Resolver: resolver, Now: func() time.Time { return now }, Context: context.Background()}
	baseArgs := []string{
		"deploy", "--artifact", build.Artifact.ID, "--target", "singbox",
		"--device", localFileURL(configPath), "--catalog-dir", catalog, "--data-dir", data,
	}

	// A local deployment takes no account, no interface, and no credential.
	preview := deployViaCLI(t, deps, baseArgs, "")
	if preview.Confirmed || preview.Hint == "" {
		t.Fatalf("preview = %#v", preview)
	}
	if _, err := os.Stat(ruleSetPath); !os.IsNotExist(err) {
		t.Fatalf("an unconfirmed deployment wrote the rule set: %v", err)
	}

	applied := deployViaCLI(t, deps, append(append([]string(nil), baseArgs...), "--confirm"), "")
	if !applied.Applied || applied.RolledBack {
		t.Fatalf("result = %#v", applied)
	}
	if applied.Device.Vendor != singboxlocal.Vendor || applied.Device.FormatKey != singbox.Version {
		t.Fatalf("device = %#v", applied.Device)
	}
	if applied.Device.Interface != ruleSetPath {
		t.Fatalf("device rule set = %q, want %q", applied.Device.Interface, ruleSetPath)
	}
	if !applied.Backup.Verified() {
		t.Fatalf("backup = %#v", applied.Backup)
	}
	steps := make([]string, 0, len(applied.Events))
	for _, event := range applied.Events {
		steps = append(steps, event.Step+":"+event.Outcome)
	}
	if strings.Join(steps, ",") != "probe:success,backup:success,deploy:success,verify:success" {
		t.Fatalf("lifecycle = %v", steps)
	}

	// The file on disk is the published artifact and satisfies its own validator.
	written, err := os.ReadFile(ruleSetPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := singbox.Validate(written); err != nil {
		t.Fatalf("the deployed rule set is not valid: %v\n%s", err, written)
	}
	if !strings.Contains(string(written), "youtube.com") {
		t.Fatalf("deployed rule set = %s", written)
	}
	artifact := httpGet(t, origin+"/v1/artifacts/"+build.Artifact.ID, nil)
	if artifact.status != 200 || string(written) != string(artifact.body) {
		t.Fatalf("the deployed file is not the published artifact:\n%s\n%s", written, artifact.body)
	}

	// A credential is refused rather than ignored: a local deployment has
	// nowhere to send one.
	stdout, deployStderr := &syncBuffer{}, &syncBuffer{}
	withCredential := append(append([]string(nil), baseArgs...), "--confirm", "--user", "admin")
	if code := runWithDeps(stdout, deployStderr, withCredential, deps); code == 0 {
		t.Fatalf("a local deployment accepted an account: %s", stdout.String())
	}
	if !strings.Contains(deployStderr.String(), "connection_invalid") {
		t.Fatalf("stderr = %s", deployStderr.String())
	}

	// A configuration whose rule set this product cannot write is refused before
	// anything is touched.
	foreignDir := t.TempDir()
	foreignConfig := filepath.Join(foreignDir, "config.json")
	writeBinarySingBoxConfig(t, foreignConfig)
	stdout, deployStderr = &syncBuffer{}, &syncBuffer{}
	foreignArgs := []string{
		"deploy", "--artifact", build.Artifact.ID, "--target", "singbox",
		"--device", localFileURL(foreignConfig), "--catalog-dir", catalog, "--data-dir", data, "--confirm",
	}
	if code := runWithDeps(stdout, deployStderr, foreignArgs, deps); code == 0 {
		t.Fatalf("a configuration with no writable local rule set was accepted: %s", stdout.String())
	}
	entries, err := os.ReadDir(foreignDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("a refused deployment wrote into the configuration directory: %#v", entries)
	}

	// A destination that cannot be backed up stops the lifecycle at the backup
	// step: without a backup there is nothing to roll back to, so nothing is
	// written. Here the declared rule set is a directory.
	blockedDir := t.TempDir()
	blockedConfig := filepath.Join(blockedDir, "config.json")
	writeLocalSingBoxConfig(t, blockedConfig, "blocked")
	if err := os.Mkdir(filepath.Join(blockedDir, "blocked"), 0o700); err != nil {
		t.Fatal(err)
	}
	stdout, deployStderr = &syncBuffer{}, &syncBuffer{}
	blockedArgs := []string{
		"deploy", "--artifact", build.Artifact.ID, "--target", "singbox",
		"--device", localFileURL(blockedConfig), "--catalog-dir", catalog, "--data-dir", data, "--confirm",
	}
	if code := runWithDeps(stdout, deployStderr, blockedArgs, deps); code == 0 {
		t.Fatalf("a failed deployment reported success: %s", stdout.String())
	}
	var blocked deployReport
	if err := json.Unmarshal([]byte(stdout.String()), &blocked); err != nil {
		t.Fatalf("stdout=%s err=%v", stdout.String(), err)
	}
	if blocked.Applied || blocked.RolledBack {
		t.Fatalf("report = %#v", blocked)
	}
	blockedSteps := make([]string, 0, len(blocked.Events))
	for _, event := range blocked.Events {
		blockedSteps = append(blockedSteps, event.Step+":"+event.Outcome)
	}
	if strings.Join(blockedSteps, ",") != "probe:success,backup:failed" {
		t.Fatalf("lifecycle = %v", blockedSteps)
	}
	// The directory that stood in the way is untouched: a refused deployment
	// changes nothing.
	info, err := os.Stat(filepath.Join(blockedDir, "blocked"))
	if err != nil || !info.IsDir() {
		t.Fatalf("stat = %#v err = %v", info, err)
	}
	if entries, readErr := os.ReadDir(filepath.Join(blockedDir, "blocked")); readErr != nil || len(entries) != 0 {
		t.Fatalf("entries = %#v err = %v", entries, readErr)
	}
}

func writeLocalSingBoxConfig(t *testing.T, path, ruleSetPath string) {
	t.Helper()
	config := map[string]any{
		"log": map[string]any{"level": "warn"},
		"route": map[string]any{
			"rule_set": []map[string]any{
				{"tag": "routevane", "type": "local", "format": "source", "path": ruleSetPath},
			},
		},
	}
	writeJSONFile(t, path, config)
}

func writeBinarySingBoxConfig(t *testing.T, path string) {
	t.Helper()
	config := map[string]any{
		"route": map[string]any{
			"rule_set": []map[string]any{
				{"tag": "compiled", "type": "local", "format": "binary", "path": "compiled.srs"},
			},
		},
	}
	writeJSONFile(t, path, config)
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func localFileURL(path string) string {
	normalized := strings.ReplaceAll(path, "\\", "/")
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return singboxlocal.Scheme + normalized
}
