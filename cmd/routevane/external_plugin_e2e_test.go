package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

// installPlugin builds one plugin and installs it the way an operator would: an
// executable beside a manifest whose checksum matches it.
func installPlugin(t *testing.T, root, name, packagePath string, manifest map[string]any) {
	t.Helper()
	directory := filepath.Join(root, name)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := "plugin"
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	build := exec.Command("go", "build", "-o", filepath.Join(directory, executable), packagePath)
	build.Dir = filepath.Join("..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, output)
	}
	payload, err := os.ReadFile(filepath.Join(directory, executable))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	manifest["executable"] = executable
	manifest["sha256"] = hex.EncodeToString(digest[:])
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exampleFile(t *testing.T, example, name string) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("..", "..", "examples", "plugins", example, name))
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func exampleManifest(t *testing.T, example string) map[string]any {
	t.Helper()
	var manifest map[string]any
	if err := json.Unmarshal(exampleFile(t, example, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestPublishesThroughAnExternalRendererAndWorksWithoutOne(t *testing.T) {
	catalogDir := filepath.Join(t.TempDir(), "catalog")
	dataDir := filepath.Join(t.TempDir(), "data")
	outputDir := filepath.Join(dataDir, "out")
	pluginsDir := filepath.Join(t.TempDir(), "plugins")

	// A catalog with one built-in target and one target served only by a plugin.
	if err := os.MkdirAll(filepath.Join(catalogDir, "builtin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(catalogDir, "targets"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"youtube.yaml"} {
		payload, err := os.ReadFile(filepath.Join("..", "..", "testdata", "expiry", "catalog", "builtin", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(catalogDir, "builtin", name), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keeneticTarget, err := os.ReadFile(filepath.Join("..", "..", "catalog", "targets", "keenetic.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalogDir, "targets", "keenetic.yaml"), keeneticTarget, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalogDir, "targets", "examplecsv.yaml"), exampleFile(t, "csv-renderer", "target.yaml"), 0o600); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}})
	deps := runtimeDeps{Resolver: resolver, Now: func() time.Time { return now }, Context: context.Background()}

	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	if code := runWithDeps(stdout, stderr, []string{"refresh", "--list", "youtube", "--catalog-dir", catalogDir, "--data-dir", dataDir}, deps); code != 0 {
		t.Fatalf("refresh failed: %s %s", stdout.String(), stderr.String())
	}

	// With no plugin directory the built-in target still builds, and the
	// plugin-only target is simply not selectable.
	builtIn := buildYoutubeArtifact(t, deps, catalogDir, dataDir, outputDir, "keenetic")
	if !strings.Contains(builtIn, "route ADD 192.0.2.10") {
		t.Fatalf("built-in artifact = %s", builtIn)
	}
	stdout, stderr = &syncBuffer{}, &syncBuffer{}
	if code := runWithDeps(stdout, stderr, []string{"build", "--target", "examplecsv", "--list", "youtube", "--catalog-dir", catalogDir, "--data-dir", dataDir, "--output", outputDir}, deps); code == 0 {
		t.Fatalf("a plugin-only target was selectable with no plugin installed: %s", stdout.String())
	}

	// Installing the plugin makes its target selectable, and the artifact goes
	// through the same publication path as a built-in one.
	installPlugin(t, pluginsDir, "example-csv-renderer", "./examples/plugins/csv-renderer", exampleManifest(t, "csv-renderer"))
	deps.PluginsDir = pluginsDir
	external := buildYoutubeArtifact(t, deps, catalogDir, dataDir, outputDir, "examplecsv")
	if !strings.HasPrefix(external, "kind,value,service,component\n") {
		t.Fatalf("external artifact = %s", external)
	}
	// The plugin's target is domain-capable, so the same policy that applies to
	// the built-in sing-box format applies here: the domain covers the component
	// and the observed address is not needed. A plugin gets the plan the planner
	// decided, never a different one.
	if !strings.Contains(external, "domain_suffix,youtube.com,youtube,playback") {
		t.Fatalf("external artifact = %s", external)
	}
	if strings.Contains(external, "192.0.2.10") {
		t.Fatalf("a domain-capable plugin target must not need the observed address: %s", external)
	}
	// The built-in target keeps working with the plugin installed.
	stillBuiltIn := buildYoutubeArtifact(t, deps, catalogDir, dataDir, outputDir, "keenetic")
	if stillBuiltIn != builtIn {
		t.Fatalf("installing a plugin changed a built-in artifact:\n%s\n%s", builtIn, stillBuiltIn)
	}

	// A replaced executable is refused rather than run.
	executable := "plugin"
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	if err := os.WriteFile(filepath.Join(pluginsDir, "example-csv-renderer", executable), []byte("replaced"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr = &syncBuffer{}, &syncBuffer{}
	if code := runWithDeps(stdout, stderr, []string{"build", "--target", "examplecsv", "--list", "youtube", "--catalog-dir", catalogDir, "--data-dir", dataDir, "--output", outputDir}, deps); code == 0 {
		t.Fatalf("a replaced plugin executable was accepted: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "checksum") {
		t.Fatalf("the refusal must name the checksum: %s", stderr.String())
	}
}

func TestPluginsAreDiscoveredThroughOneEnvironmentVariable(t *testing.T) {
	pluginsDir := filepath.Join(t.TempDir(), "plugins")
	installPlugin(t, pluginsDir, "example-static-source", "./examples/plugins/static-source", exampleManifest(t, "static-source"))
	t.Setenv(PluginsDirVariable, pluginsDir)
	deps := normalizeDeps(runtimeDeps{Now: func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) }, Context: context.Background()})
	if deps.PluginsDir != pluginsDir {
		t.Fatalf("plugins dir = %q", deps.PluginsDir)
	}
	resolved, err := resolveAdapters(context.Background(), deps, newLogger(&syncBuffer{}))
	if err != nil {
		t.Fatal(err)
	}
	defer resolved.Close()
	// The plugin's source type joins the built-in ones without replacing any.
	for _, expected := range []string{"dns", "http", "example-static"} {
		if _, present := resolved.sources[domain.SourceType(expected)]; !present {
			t.Fatalf("source type %q is missing: %#v", expected, resolved.sources)
		}
	}
	// A source plugin adds no format: every built-in renderer is still present
	// and nothing else is.
	builtin := builtinRenderers()
	if len(resolved.renderers) != len(builtin) {
		t.Fatalf("renderers = %#v", resolved.renderers)
	}
	for id := range builtin {
		if _, present := resolved.renderers[id]; !present {
			t.Fatalf("built-in renderer %q is missing: %#v", id, resolved.renderers)
		}
	}
}

func TestAnInstalledExternalSourceParticipatesInRefresh(t *testing.T) {
	catalogDir := filepath.Join(t.TempDir(), "catalog")
	dataDir := filepath.Join(t.TempDir(), "data")
	pluginsDir := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(filepath.Join(catalogDir, "builtin"), 0o700); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(catalogDir, "builtin", "plugin-list.yaml")
	if err := os.WriteFile(catalogPath, exampleFile(t, "static-source", "list.yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	installPlugin(t, pluginsDir, "example-static-source", "./examples/plugins/static-source", exampleManifest(t, "static-source"))
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	deps := runtimeDeps{PluginsDir: pluginsDir, Now: func() time.Time { return now }, Context: context.Background()}
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	args := []string{"refresh", "--list", "plugin-list", "--catalog-dir", catalogDir, "--data-dir", dataDir}
	if code := runWithDeps(stdout, stderr, args, deps); code != 0 {
		t.Fatalf("external source refresh failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var summary application.RefreshSummary
	if err := json.Unmarshal([]byte(stdout.String()), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.SourceRuns != 2 || summary.SuccessfulRuns != 2 || summary.FailedRuns != 0 || summary.Sightings != 3 {
		t.Fatalf("refresh summary = %#v", summary)
	}

	// The catalog is loadable without local plugin state, but an actual refresh
	// refuses a missing adapter instead of silently omitting the declared source.
	missingDataDir := filepath.Join(t.TempDir(), "missing-data")
	stdout, stderr = &syncBuffer{}, &syncBuffer{}
	missingDeps := runtimeDeps{Now: func() time.Time { return now }, Context: context.Background()}
	missingArgs := []string{"refresh", "--list", "plugin-list", "--catalog-dir", catalogDir, "--data-dir", missingDataDir}
	if code := runWithDeps(stdout, stderr, missingArgs, missingDeps); code == 0 || !strings.Contains(stderr.String(), "not installed") {
		t.Fatalf("missing plugin was not refused: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	// The catalog revision is the operator-reviewed contract. An installed
	// process reporting another implementation revision is also a refusal.
	mismatched := strings.Replace(string(exampleFile(t, "static-source", "list.yaml")), "example-static-v1", "example-static-v2", 1)
	if err := os.WriteFile(catalogPath, []byte(mismatched), 0o600); err != nil {
		t.Fatal(err)
	}
	mismatchDataDir := filepath.Join(t.TempDir(), "mismatch-data")
	stdout, stderr = &syncBuffer{}, &syncBuffer{}
	mismatchArgs := []string{"refresh", "--list", "plugin-list", "--catalog-dir", catalogDir, "--data-dir", mismatchDataDir}
	if code := runWithDeps(stdout, stderr, mismatchArgs, deps); code == 0 || !strings.Contains(stderr.String(), "does not match") {
		t.Fatalf("mismatched plugin revision was not refused: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func buildYoutubeArtifact(t *testing.T, deps runtimeDeps, catalogDir, dataDir, outputDir, target string) string {
	t.Helper()
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	args := []string{"build", "--target", target, "--list", "youtube", "--catalog-dir", catalogDir, "--data-dir", dataDir, "--output", outputDir}
	if code := runWithDeps(stdout, stderr, args, deps); code != 0 {
		t.Fatalf("build %s failed: code=%d stdout=%s stderr=%s", target, code, stdout.String(), stderr.String())
	}
	payload, err := os.ReadFile(strings.TrimSpace(stdout.String()))
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
