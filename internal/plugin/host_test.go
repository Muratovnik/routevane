package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
	wire "github.com/Muratovnik/routevane/sdk/routevaneplugin"
)

type observedWriteCloser struct {
	io.WriteCloser
	started chan struct{}
	once    sync.Once
}

func (w *observedWriteCloser) Write(payload []byte) (int, error) {
	if len(payload) > wire.MaxFrameBytes/2 {
		w.once.Do(func() { close(w.started) })
	}
	return w.WriteCloser.Write(payload)
}

func TestMain(m *testing.M) {
	if handled, exitCode := RunProcessRunner(os.Args[1:]); handled {
		os.Exit(exitCode)
	}
	os.Exit(m.Run())
}

// buildPlugin compiles one example plugin and installs it beside a manifest whose
// checksum matches the file that was just built. Installing the way an operator
// would is the point: the host is exercised through discovery, not through a
// hand-made struct.
func buildPlugin(t *testing.T, packagePath string, manifest wire.Manifest) Installed {
	t.Helper()
	directory := t.TempDir()
	name := "plugin"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable := filepath.Join(directory, name)
	build := exec.Command("go", "build", "-o", executable, packagePath)
	build.Dir = repositoryRoot(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, output)
	}
	payload, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	manifest.Executable = name
	manifest.SHA256 = hex.EncodeToString(digest[:])
	writeManifest(t, directory, manifest)
	installed, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	return installed
}

func writeManifest(t *testing.T, directory string, manifest wire.Manifest) {
	t.Helper()
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func rendererManifest() wire.Manifest {
	return wire.Manifest{
		Name: "example-csv-renderer", Version: "1.0.0", ProtocolVersion: wire.ProtocolVersion,
		Kind: wire.KindRenderer, Permissions: []wire.Permission{wire.PermissionRenderPlan},
		Renderer: &wire.RendererManifest{
			ID: "example-csv", FormatVersion: "example-csv-v1", ContentType: "text/csv", FileExtension: "csv",
			SupportedRuleKinds: []string{"domain_exact", "domain_suffix", "ipv4", "ipv6", "prefix4", "prefix6"},
		},
	}
}

func sourceManifest() wire.Manifest {
	return wire.Manifest{
		Name: "example-static-source", Version: "1.0.0", ProtocolVersion: wire.ProtocolVersion,
		Kind: wire.KindSource, Permissions: []wire.Permission{wire.PermissionObserveNames},
		Source: &wire.SourceManifest{Type: "example-static", Revision: "example-static-v1"},
	}
}

func testPlan(t *testing.T) domain.RoutingPlan {
	t.Helper()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	rule, err := domain.NewAddrRule(mustAddr(t, "192.0.2.10"), "example", "core", domain.SourceObserved, []string{"fresh_dns_observation"}, []string{"dns"})
	if err != nil {
		t.Fatal(err)
	}
	suffix, err := domain.NewDomainRule(domain.RuleDomainSuffix, "example.com", "example", "core", domain.SourceManual, []string{"manual_rule"}, []string{"manual"})
	if err != nil {
		t.Fatal(err)
	}
	return domain.RoutingPlan{
		InterfaceVersion: domain.RoutingPlanInterfaceVersion, TargetID: "example", FormatKey: "example-csv-v1",
		Lists: []string{"example"}, Rules: []domain.RouteRule{rule, suffix},
		Coverage:      []domain.Coverage{{ListID: "example", ComponentID: "core", Complete: true, RuleCount: 2}},
		PolicyVersion: "auto-v1", CatalogRevision: strings.Repeat("a", 64), ObservationCutoff: now,
		SemanticHash: strings.Repeat("b", 64),
	}
}

func mustAddr(t *testing.T, value string) netip.Addr {
	t.Helper()
	resource, err := domain.NewAddrResourceFromString(value)
	if err != nil {
		t.Fatal(err)
	}
	return resource.Addr
}

func TestAnExternalRendererIsIndistinguishableFromABuiltInOne(t *testing.T) {
	installed := buildPlugin(t, "./examples/plugins/csv-renderer", rendererManifest())
	client, err := Start(context.Background(), installed, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if client.ProtocolVersion() != wire.ProtocolVersion {
		t.Fatalf("negotiated version = %d", client.ProtocolVersion())
	}

	renderer, err := NewRenderer(client)
	if err != nil {
		t.Fatal(err)
	}
	// It satisfies the same seam the built-in renderers do.
	var _ application.Renderer = renderer
	descriptor := renderer.Descriptor()
	if !descriptor.IsValid() || descriptor.ID != "example-csv" || descriptor.ContentType != "text/csv" || descriptor.FileExtension != "csv" {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if len(renderer.SupportedRuleKinds()) != 6 {
		t.Fatalf("kinds = %#v", renderer.SupportedRuleKinds())
	}

	plan := testPlan(t)
	count, err := renderer.ProjectedRuleCount(plan)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d", count)
	}
	artifact, err := renderer.Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	text := string(artifact)
	if !strings.HasPrefix(text, "kind,value,service,component\n") {
		t.Fatalf("artifact = %q", text)
	}
	if !strings.Contains(text, "ipv4,192.0.2.10,example,core") || !strings.Contains(text, "domain_suffix,example.com,example,core") {
		t.Fatalf("artifact = %q", text)
	}
	if err := renderer.Validate(artifact); err != nil {
		t.Fatalf("the plugin cannot validate its own artifact: %v", err)
	}
	// The plugin's validator is what decides, so a corrupted artifact is refused.
	if err := renderer.Validate([]byte("kind,value,service,component\nbroken\n")); err == nil {
		t.Fatal("a corrupted artifact was accepted")
	}
	if err := renderer.Validate(nil); err == nil {
		t.Fatal("an empty artifact was accepted")
	}
}

func TestAnExternalSourceCannotIntroduceAnUncheckedValue(t *testing.T) {
	installed := buildPlugin(t, "./examples/plugins/static-source", sourceManifest())
	client, err := Start(context.Background(), installed, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	source, err := NewSource(client, 0)
	if err != nil {
		t.Fatal(err)
	}
	var _ application.Source = source
	if source.Type() != domain.SourceType("example-static") {
		t.Fatalf("type = %q", source.Type())
	}

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	result, err := source.Observe(context.Background(), application.SourceRequest{
		ListID: "example", ComponentID: "core", SourceID: "static", SourceRevision: "example-static-v1",
		Names: []string{"static.example.test", "edge.example.test", "unknown.example.test"}, ObservedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sightings) != 3 {
		t.Fatalf("sightings = %#v", result.Sightings)
	}
	for _, sighting := range result.Sightings {
		if !sighting.Resource.IsValid() {
			t.Fatalf("the host stored an invalid resource: %#v", sighting.Resource)
		}
		if sighting.ListID != "example" || sighting.ComponentID != "core" || sighting.SourceRevision != "example-static-v1" {
			t.Fatalf("a plugin changed the observation identity: %#v", sighting)
		}
		// The plugin reported a TTL, so the host honors it rather than its own
		// default validity.
		if !sighting.TTLKnown || sighting.TTLSeconds != 300 || !sighting.ValidUntil.Equal(now.Add(300*time.Second)) {
			t.Fatalf("lifecycle = %#v", sighting)
		}
		if sighting.SourceClass != domain.SourceCommunity {
			t.Fatalf("a plugin observation must be community class: %#v", sighting)
		}
	}
	// An unknown name contributes nothing rather than an error.
	if _, err := source.Observe(context.Background(), application.SourceRequest{
		ListID: "example", ComponentID: "core", SourceID: "static", SourceRevision: "example-static-v1",
		Names: []string{"nothing.example.test"}, ObservedAt: now,
	}); err == nil {
		t.Fatal("a source that answers nothing must report a failure")
	}
}

func TestAnIncompatibleProtocolIsRefusedBeforeAnyWork(t *testing.T) {
	manifest := rendererManifest()
	manifest.ProtocolVersion = wire.ProtocolVersion + 1
	installed := buildPlugin(t, "./examples/plugins/csv-renderer", manifest)
	// The installed manifest claims a version this build cannot speak; the
	// plugin answers with the version it actually implements, so the reported and
	// installed manifests disagree and the plugin is refused at handshake.
	if _, err := Start(context.Background(), installed, Options{}); err == nil {
		t.Fatal("a plugin claiming an unsupported protocol version was accepted")
	} else if !errors.Is(err, ErrManifestMismatch) && !errors.Is(err, wire.ErrIncompatible) {
		t.Fatalf("err = %v", err)
	}
}

func TestAReportedManifestThatDiffersFromTheInstalledOneIsRefused(t *testing.T) {
	manifest := rendererManifest()
	// The operator installed a manifest claiming a different format identity than
	// the plugin implements. The installed copy is what was reviewed, so the
	// disagreement is a refusal.
	manifest.Renderer.ID = "something-else"
	installed := buildPlugin(t, "./examples/plugins/csv-renderer", manifest)
	if _, err := Start(context.Background(), installed, Options{}); !errors.Is(err, ErrManifestMismatch) {
		t.Fatalf("err = %v, want ErrManifestMismatch", err)
	}
}

func TestAReplacedExecutableIsRefusedBeforeItRuns(t *testing.T) {
	installed := buildPlugin(t, "./examples/plugins/csv-renderer", rendererManifest())
	// Replacing the binary after Load used to leave a check/use gap. Start must
	// verify the bytes it snapshots and refuse the replacement before it runs.
	if err := os.WriteFile(installed.Executable, []byte("not the plugin"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(context.Background(), installed, Options{}); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
}

func TestTheVerifiedExecutionSnapshotIsRemovedOnClose(t *testing.T) {
	installed := buildPlugin(t, "./examples/plugins/csv-renderer", rendererManifest())
	client, err := Start(context.Background(), installed, Options{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := client.snapshotDirectory
	if snapshot == "" || filepath.Clean(snapshot) == filepath.Clean(installed.Directory) {
		t.Fatalf("snapshot directory = %q", snapshot)
	}
	if _, err := os.Stat(filepath.Join(snapshot, installed.Manifest.Executable)); err != nil {
		t.Fatalf("execution snapshot is missing: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if client.command.ProcessState == nil {
		t.Fatal("the cleanly closed plugin process was not reaped")
	}
	if _, err := os.Stat(snapshot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("execution snapshot still exists: %v", err)
	}
}

func TestDiscoveryRefusesUnusableInstallationsAndToleratesNone(t *testing.T) {
	// No plugin directory at all behaves exactly as a build without plugins.
	discovered, err := Discover(filepath.Join(t.TempDir(), "absent"))
	if err != nil || len(discovered) != 0 {
		t.Fatalf("discovered = %#v err = %v", discovered, err)
	}
	if discovered, err := Discover(""); err != nil || len(discovered) != 0 {
		t.Fatalf("discovered = %#v err = %v", discovered, err)
	}

	root := t.TempDir()
	broken := filepath.Join(root, "broken")
	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatal(err)
	}
	// A directory that exists but holds no manifest is an error: ignoring it
	// would look like the plugin was working.
	if _, err := Discover(root); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("err = %v, want ErrManifestInvalid", err)
	}
}

func TestManifestValidationRefusesUnsafeAndOverreachingClaims(t *testing.T) {
	base := rendererManifest()
	base.Executable = "plugin"
	base.SHA256 = strings.Repeat("a", 64)
	if err := ValidateManifest(base); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*wire.Manifest){
		"no name":            func(m *wire.Manifest) { m.Name = "" },
		"uppercase name":     func(m *wire.Manifest) { m.Name = "Example" },
		"no version":         func(m *wire.Manifest) { m.Version = "" },
		"no protocol":        func(m *wire.Manifest) { m.ProtocolVersion = 0 },
		"unknown kind":       func(m *wire.Manifest) { m.Kind = wire.Kind("deployer") },
		"path in executable": func(m *wire.Manifest) { m.Executable = "../escape" },
		"absolute executable": func(m *wire.Manifest) {
			m.Executable = "/usr/bin/env"
		},
		"short checksum":  func(m *wire.Manifest) { m.SHA256 = "abc" },
		"nonhex checksum": func(m *wire.Manifest) { m.SHA256 = strings.Repeat("z", 64) },
		"unknown permission": func(m *wire.Manifest) {
			m.Permissions = []wire.Permission{wire.Permission("read_database")}
		},
		// A renderer asking for a source permission is exactly the overreach the
		// permission model exists to refuse.
		"permission for another kind": func(m *wire.Manifest) {
			m.Permissions = []wire.Permission{wire.PermissionObserveNames}
		},
		"renderer without metadata": func(m *wire.Manifest) { m.Renderer = nil },
		"renderer claiming a source": func(m *wire.Manifest) {
			m.Source = &wire.SourceManifest{Type: "x", Revision: "y"}
		},
		"incomplete renderer": func(m *wire.Manifest) {
			m.Renderer = &wire.RendererManifest{ID: "x"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			manifest := base
			renderer := *base.Renderer
			manifest.Renderer = &renderer
			mutate(&manifest)
			if err := ValidateManifest(manifest); err == nil {
				t.Fatalf("accepted %#v", manifest)
			}
		})
	}
}

func TestSourceManifestIdentityUsesTheCatalogSlugGrammar(t *testing.T) {
	base := sourceManifest()
	base.Executable = "plugin"
	base.SHA256 = strings.Repeat("a", 64)
	if err := ValidateManifest(base); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*wire.Manifest){
		func(manifest *wire.Manifest) { manifest.Source.Type = "ExampleStatic" },
		func(manifest *wire.Manifest) { manifest.Source.Revision = "version 1" },
	} {
		manifest := base
		source := *base.Source
		manifest.Source = &source
		change(&manifest)
		if err := ValidateManifest(manifest); !errors.Is(err, ErrManifestInvalid) {
			t.Fatalf("invalid source manifest accepted: %#v err=%v", manifest.Source, err)
		}
	}
}

func TestThePluginProcessNeverSeesTheHostEnvironment(t *testing.T) {
	// A secret in the host's environment must not reach the child.
	t.Setenv("ROUTEVANE_DEVICE_PASSWORD", "device-secret-value")
	t.Setenv("ROUTEVANE_TEST_SECRET", "another-secret")
	environment := minimalEnvironment()
	for _, entry := range environment {
		if strings.Contains(entry, "device-secret-value") || strings.Contains(entry, "another-secret") {
			t.Fatalf("the child environment carried a host secret: %q", entry)
		}
		if strings.HasPrefix(entry, "ROUTEVANE_") {
			t.Fatalf("the child environment carried a Routevane variable: %q", entry)
		}
	}
}

func TestPermissionsAreGrantedByKindRatherThanRequested(t *testing.T) {
	if got := wire.RequiredPermissions(wire.KindRenderer); len(got) != 1 || got[0] != wire.PermissionRenderPlan {
		t.Fatalf("renderer permissions = %#v", got)
	}
	source := wire.RequiredPermissions(wire.KindSource)
	if len(source) != 2 {
		t.Fatalf("source permissions = %#v", source)
	}
	if len(wire.RequiredPermissions(wire.Kind("deployer"))) != 0 {
		t.Fatal("an unknown kind must hold no permission")
	}
}

func fixtureManifest(name, rendererID string) wire.Manifest {
	return wire.Manifest{
		Name: name, Version: "1.0.0", ProtocolVersion: wire.ProtocolVersion,
		Kind: wire.KindRenderer, Permissions: []wire.Permission{wire.PermissionRenderPlan},
		Renderer: &wire.RendererManifest{
			ID: rendererID, FormatVersion: rendererID + "-v1", ContentType: "text/plain", FileExtension: "txt",
			SupportedRuleKinds: []string{"ipv4"},
		},
	}
}

func TestAPluginThatDiesFailsItsOwnCallAndNothingElse(t *testing.T) {
	crashing := buildPlugin(t, "./internal/plugin/testdata/crashing", fixtureManifest("crashing-plugin", "crashing"))
	client, err := Start(context.Background(), crashing, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	renderer, err := NewRenderer(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := renderer.Render(testPlan(t)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	// The host is still running and can still use another plugin, which is the
	// property that matters: one plugin's crash is not the product's crash.
	healthy := buildPlugin(t, "./examples/plugins/csv-renderer", rendererManifest())
	second, err := Start(context.Background(), healthy, Options{})
	if err != nil {
		t.Fatalf("the host could not start another plugin after a crash: %v", err)
	}
	defer func() { _ = second.Close() }()
	working, err := NewRenderer(second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := working.Render(testPlan(t)); err != nil {
		t.Fatalf("a healthy plugin failed after another crashed: %v", err)
	}
}

func TestAPluginThatHangsIsTerminated(t *testing.T) {
	hanging := buildPlugin(t, "./internal/plugin/testdata/hanging", fixtureManifest("hanging-plugin", "hanging"))
	client, err := Start(context.Background(), hanging, Options{CallTimeout: 500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	renderer, err := NewRenderer(client)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := renderer.Render(testPlan(t)); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Fatalf("the deadline was not enforced: %v", elapsed)
	}
	// The process was killed rather than left running.
	if client.command.ProcessState == nil {
		_ = client.command.Wait()
	}
	if client.command.ProcessState == nil {
		t.Fatal("the hung plugin process was not reaped")
	}
}

func TestCloseIsBoundedWhenAPluginStopsReadingAFullInputPipe(t *testing.T) {
	installed := buildPlugin(t, "./internal/plugin/testdata/blockinginput", fixtureManifest("blocking-input-plugin", "blocking-input"))
	client, err := Start(context.Background(), installed, Options{CallTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.terminate()
		_ = client.Close()
	})
	snapshot := client.snapshotDirectory

	writeStarted := make(chan struct{})
	client.stdin = &observedWriteCloser{WriteCloser: client.stdin, started: writeStarted}
	callDone := make(chan error, 1)
	go func() {
		_, callErr := client.Call(context.Background(), wire.Envelope{
			Type:     wire.MessageValidate,
			Validate: &wire.ValidateCall{Payload: bytes.Repeat([]byte("a"), 5<<20)},
		})
		callDone <- callErr
	}()
	select {
	case <-writeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the allowed large call never reached the plugin input pipe")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- client.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(DefaultShutdownGrace + 3*time.Second):
		_ = client.terminate()
		select {
		case <-closeDone:
		case <-time.After(5 * time.Second):
			t.Fatal("Close stayed blocked after forced process termination")
		}
		t.Fatal("Close did not enforce its shutdown grace while plugin input was blocked")
	}

	select {
	case callErr := <-callDone:
		if !errors.Is(callErr, ErrUnavailable) && !errors.Is(callErr, ErrTimeout) {
			t.Fatalf("blocked call error = %v", callErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the blocked call remained after Close returned")
	}
	if client.command.ProcessState == nil {
		t.Fatal("the input-blocked plugin process was not reaped")
	}
	if _, err := os.Stat(snapshot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("execution snapshot still exists: %v", err)
	}
	if _, err := client.Call(context.Background(), wire.Envelope{Type: wire.MessageValidate, Validate: &wire.ValidateCall{}}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("call after close err = %v, want ErrUnavailable", err)
	}
}

func TestAHandshakeThatNeverArrivesIsBounded(t *testing.T) {
	hanging := buildPlugin(t, "./internal/plugin/testdata/hanging", fixtureManifest("hanging-plugin", "hanging"))
	// The fixture answers the handshake, so a zero-length handshake budget is
	// what proves the bound is the host's rather than the plugin's goodwill.
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if _, err := Start(ctx, hanging, Options{HandshakeTimeout: time.Nanosecond}); err == nil {
		t.Fatal("an unbounded handshake was accepted")
	}
}
