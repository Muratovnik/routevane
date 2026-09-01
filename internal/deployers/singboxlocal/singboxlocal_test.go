package singboxlocal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/renderers/singbox"
)

const ruleSetPayload = `{
  "version": 3,
  "rules": [
    {
      "domain_suffix": [
        "example.com"
      ]
    }
  ]
}
`

const previousPayload = `{
  "version": 3,
  "rules": [
    {
      "domain_suffix": [
        "previous.example.com"
      ]
    }
  ]
}
`

type fixture struct {
	configPath  string
	ruleSetPath string
	connection  application.Connection
}

func newFixture(t *testing.T, ruleSetName string) fixture {
	t.Helper()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	config := map[string]any{
		"log": map[string]any{"level": "info"},
		"route": map[string]any{
			"rule_set": []map[string]any{
				{"tag": "remote-thing", "type": "remote", "format": "binary", "url": "https://example.com/x.srs"},
				{"tag": "routevane", "type": "local", "format": "source", "path": ruleSetName},
			},
		},
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return fixture{
		configPath:  configPath,
		ruleSetPath: filepath.Join(directory, ruleSetName),
		connection:  application.Connection{URL: fileURL(configPath)},
	}
}

func fileURL(path string) string {
	normalized := strings.ReplaceAll(path, "\\", "/")
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return Scheme + normalized
}

func artifact(payload string) application.DeployArtifact {
	return application.DeployArtifact{
		ArtifactID: "artifact", RendererID: DeployerID, ArtifactHash: strings.Repeat("a", 64),
		ContentType: "application/json", Payload: []byte(payload),
	}
}

func TestValidateConnectionAcceptsALocalPathAndRefusesACredential(t *testing.T) {
	deployer := New()
	base := newFixture(t, "routevane.json")
	if err := deployer.ValidateConnection(base.connection); err != nil {
		t.Fatalf("a local configuration path was refused: %v", err)
	}
	cases := map[string]application.Connection{
		"no url":            {},
		"http url":          {URL: "http://192.168.1.1"},
		"relative path":     {URL: Scheme + "config.json"},
		"non canonical":     {URL: Scheme + "/tmp/../tmp/config.json"},
		"with authority":    {URL: Scheme + "server/share/config.json"},
		"with userinfo":     {URL: "file://user@/tmp/config.json"},
		"with query":        {URL: base.connection.URL + "?secret=value"},
		"with fragment":     {URL: base.connection.URL + "#secret"},
		"with account":      {URL: base.connection.URL, Username: "admin"},
		"with password":     {URL: base.connection.URL, Password: "secret"},
		"with an interface": {URL: base.connection.URL, Interface: "Wireguard0"},
	}
	for name, connection := range cases {
		t.Run(name, func(t *testing.T) {
			if err := deployer.ValidateConnection(connection); err == nil {
				t.Fatalf("connection accepted: %#v", connection)
			}
		})
	}
}

func TestProbeReportsTheDeclaredLocalRuleSet(t *testing.T) {
	deployer := New()
	base := newFixture(t, "routevane.json")
	device, err := deployer.Probe(context.Background(), base.connection)
	if err != nil {
		t.Fatal(err)
	}
	if device.Vendor != Vendor || device.ProfileKey != singbox.Version {
		t.Fatalf("device = %#v", device)
	}
	// A relative rule-set path is resolved against the configuration, and the
	// remote rule set in the same configuration is ignored.
	if device.Interface != base.ruleSetPath {
		t.Fatalf("rule set path = %q, want %q", device.Interface, base.ruleSetPath)
	}
	if device.FirmwareVersion != "routevane" {
		t.Fatalf("device = %#v", device)
	}
}

func TestProbeRefusesAConfigurationThisDeployerCannotWrite(t *testing.T) {
	deployer := New()
	cases := map[string]map[string]any{
		"no rule set":     {"route": map[string]any{}},
		"remote only":     {"route": map[string]any{"rule_set": []map[string]any{{"tag": "x", "type": "remote", "format": "binary", "url": "https://example.com/x"}}}},
		"binary local":    {"route": map[string]any{"rule_set": []map[string]any{{"tag": "x", "type": "local", "format": "binary", "path": "x.srs"}}}},
		"no path":         {"route": map[string]any{"rule_set": []map[string]any{{"tag": "x", "type": "local", "format": "source"}}}},
		"two local sets":  {"route": map[string]any{"rule_set": []map[string]any{{"tag": "a", "type": "local", "format": "source", "path": "a.json"}, {"tag": "b", "type": "local", "format": "source", "path": "b.json"}}}},
		"not a route key": {"outbounds": []map[string]any{{"type": "direct"}}},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "config.json")
			encoded, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			device, err := deployer.Probe(context.Background(), application.Connection{URL: fileURL(path)})
			if err == nil {
				t.Fatalf("configuration accepted: %#v", device)
			}
			// The refusal must be visible as an empty profile key, which is what
			// stops the lifecycle before a file is touched.
			if device.ProfileKey != "" {
				t.Fatalf("device = %#v", device)
			}
		})
	}
	// A missing or unreadable configuration is refused too.
	if _, err := deployer.Probe(context.Background(), application.Connection{URL: fileURL(filepath.Join(t.TempDir(), "absent.json"))}); !errors.Is(err, ErrConfigUnusable) {
		t.Fatalf("err = %v", err)
	}
	invalid := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(invalid, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := deployer.Probe(context.Background(), application.Connection{URL: fileURL(invalid)}); !errors.Is(err, ErrConfigUnusable) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeployWritesVerifiesAndRollsBackToTheAbsentFile(t *testing.T) {
	deployer := New()
	base := newFixture(t, "routevane.json")
	device, err := deployer.Probe(context.Background(), base.connection)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := deployer.Backup(context.Background(), device, base.connection)
	if err != nil {
		t.Fatal(err)
	}
	if len(backup.Payload) == 0 {
		t.Fatal("a first deployment must still produce a usable backup")
	}
	if err := deployer.Deploy(context.Background(), device, base.connection, artifact(ruleSetPayload)); err != nil {
		t.Fatal(err)
	}
	if err := deployer.Verify(context.Background(), device, base.connection, artifact(ruleSetPayload)); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(base.ruleSetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != ruleSetPayload {
		t.Fatalf("file = %q", written)
	}
	// Rolling back a first deployment removes the file, because that is what was
	// there before.
	if err := deployer.Rollback(context.Background(), device, base.connection, backup); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base.ruleSetPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the rule set survived a rollback: %v", err)
	}
	// No temporary file is left behind by either write.
	entries, err := os.ReadDir(filepath.Dir(base.ruleSetPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "routevane-") {
			t.Fatalf("a temporary file was left behind: %s", entry.Name())
		}
	}
}

func TestRollbackRestoresThePreviousRuleSetExactly(t *testing.T) {
	deployer := New()
	base := newFixture(t, "routevane.json")
	if err := os.WriteFile(base.ruleSetPath, []byte(previousPayload), 0o600); err != nil {
		t.Fatal(err)
	}
	device, err := deployer.Probe(context.Background(), base.connection)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := deployer.Backup(context.Background(), device, base.connection)
	if err != nil {
		t.Fatal(err)
	}
	if err := deployer.Deploy(context.Background(), device, base.connection, artifact(ruleSetPayload)); err != nil {
		t.Fatal(err)
	}
	if err := deployer.Rollback(context.Background(), device, base.connection, backup); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(base.ruleSetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != previousPayload {
		t.Fatalf("restored = %q", restored)
	}
	// A backup whose bytes were altered is refused rather than written.
	var envelope backupEnvelope
	if err := json.Unmarshal(backup.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Payload = []byte("tampered")
	tampered, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := deployer.Rollback(context.Background(), device, base.connection, application.BackupPayload{Payload: tampered}); !errors.Is(err, ErrBackupUnusable) {
		t.Fatalf("err = %v", err)
	}
	// A backup describing another file is refused too.
	envelope.Path = filepath.Join(filepath.Dir(base.ruleSetPath), "other.json")
	foreign, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := deployer.Rollback(context.Background(), device, base.connection, application.BackupPayload{Payload: foreign}); !errors.Is(err, ErrBackupUnusable) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeployRefusesBytesTheFormatDoesNotAccept(t *testing.T) {
	deployer := New()
	base := newFixture(t, "routevane.json")
	device, err := deployer.Probe(context.Background(), base.connection)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]application.DeployArtifact{
		"foreign renderer":  {ArtifactID: "a", RendererID: "keenetic-route-bat", ArtifactHash: strings.Repeat("a", 64), Payload: []byte(ruleSetPayload)},
		"not a rule set":    artifact("route ADD 192.0.2.1 MASK 255.255.255.255 0.0.0.0\r\n"),
		"non canonical":     artifact(`{"version":3,"rules":[{"domain_suffix":["example.com"]}]}` + "\n"),
		"unsupported field": artifact(strings.Replace(ruleSetPayload, `"domain_suffix"`, `"process_name"`, 1)),
	}
	for name, hostile := range cases {
		t.Run(name, func(t *testing.T) {
			if err := deployer.Deploy(context.Background(), device, base.connection, hostile); !errors.Is(err, ErrArtifactMismatch) {
				t.Fatalf("err = %v", err)
			}
			if _, statErr := os.Stat(base.ruleSetPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("a refused artifact was written: %v", statErr)
			}
		})
	}
}

func TestVerifyFailsWhenTheFileOnDiskIsNotTheArtifact(t *testing.T) {
	deployer := New()
	base := newFixture(t, "routevane.json")
	device, err := deployer.Probe(context.Background(), base.connection)
	if err != nil {
		t.Fatal(err)
	}
	if err := deployer.Verify(context.Background(), device, base.connection, artifact(ruleSetPayload)); !errors.Is(err, ErrVerifyMismatch) {
		t.Fatalf("a missing file passed verification: %v", err)
	}
	if err := os.WriteFile(base.ruleSetPath, []byte(previousPayload), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := deployer.Verify(context.Background(), device, base.connection, artifact(ruleSetPayload)); !errors.Is(err, ErrVerifyMismatch) {
		t.Fatalf("a different rule set passed verification: %v", err)
	}
}

// TestTheDeployerReachesNothing is a structural assertion: a local deployment
// must not acquire a network, a process, or a credential.
func TestTheDeployerReachesNothing(t *testing.T) {
	source, err := os.ReadFile("singboxlocal.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, forbidden := range []string{"net/http", "net.Dial", "os/exec", "exec.Command", "singbox.Render(", "planner.", "domain.RoutingPlan"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("the local deployer referenced %q", forbidden)
		}
	}
}
