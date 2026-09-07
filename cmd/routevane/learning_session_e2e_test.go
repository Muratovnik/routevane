package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/discovery"
	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/infrastructure/catalogyaml"
	"github.com/Muratovnik/routevane/internal/infrastructure/sqlite"
	"github.com/Muratovnik/routevane/internal/planner"
	"github.com/Muratovnik/routevane/internal/renderers/singbox"
)

// sessionHARFixture is a captured exploration: opening the site, signing in, and playing
// media. Each page carries the taxonomy identity of the action it belongs to.
const sessionHARFixture = `{
  "log": {
    "version": "1.2",
    "pages": [
      {"id": "p1", "title": "Home", "comment": "core"},
      {"id": "p2", "title": "Sign in", "comment": "auth"},
      {"id": "p3", "title": "Watch", "comment": "media"},
      {"id": "p4", "title": "Voice", "comment": "voice"},
      {"id": "p5", "title": "Analytics", "comment": "telemetry"},
      {"id": "p6", "title": "Promotions", "comment": "advertising"}
    ],
    "entries": [
      {"pageref": "p1", "request": {"method": "GET", "url": "https://app.example.co.uk/", "headers": [{"name": "Accept", "value": "text/html"}]}, "response": {"status": 200}},
      {"pageref": "p1", "request": {"method": "GET", "url": "https://static.example.co.uk/app.css", "headers": []}, "response": {"status": 200}},
      {"pageref": "p5", "request": {"method": "GET", "url": "https://beacon.example.co.uk/t", "headers": []}, "response": {"status": 204}},
      {"pageref": "p6", "request": {"method": "GET", "url": "https://promo.example.co.uk/ad", "headers": []}, "response": {"status": 200}},
      {"pageref": "p2", "request": {"method": "GET", "url": "https://app.example.co.uk/login", "headers": [{"name": "Accept", "value": "text/html"}]}, "response": {"status": 302, "redirectURL": "https://auth.example.co.uk/authorize"}},
      {"pageref": "p2", "request": {"method": "GET", "url": "https://auth.example.co.uk/authorize", "headers": []}, "response": {"status": 200}},
      {"pageref": "p2", "request": {"method": "GET", "url": "https://login.identityvendor.test/oauth", "headers": []}, "response": {"status": 200}},
      {"pageref": "p3", "request": {"method": "GET", "url": "https://app.example.co.uk/watch", "headers": [{"name": "Accept", "value": "text/html"}]}, "response": {"status": 200}},
      {"pageref": "p3", "request": {"method": "GET", "url": "https://media.example.co.uk/segment.ts", "headers": []}, "response": {"status": 200}},
      {"pageref": "p3", "request": {"method": "GET", "url": "https://cdn.thirdparty.test/player.js", "headers": []}, "response": {"status": 200}}
    ]
  }
}`

func TestLearnsComponentsFromAnImportedSessionAndRecordsDependencies(t *testing.T) {
	catalogDir := filepath.Join(t.TempDir(), "catalog")
	dataDir := filepath.Join(t.TempDir(), "data")
	harPath := filepath.Join(t.TempDir(), "session.har")
	if err := os.WriteFile(harPath, []byte(sessionHARFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	deps := runtimeDeps{
		Resolver: staticHostResolver{hosts: map[string][]string{
			"app.example.co.uk":    {"203.0.113.50"},
			"static.example.co.uk": {"203.0.113.51"},
			"auth.example.co.uk":   {"203.0.113.52"},
			"media.example.co.uk":  {"203.0.113.53"},
		}},
		Now:     func() time.Time { return now },
		Context: context.Background(),
	}

	// The unconfirmed call shows the target and the steps and writes nothing.
	preview := learnViaCLI(t, deps, []string{"learn", "--har", harPath, "--url", "app.example.co.uk", "--catalog-dir", catalogDir, "--data-dir", dataDir})
	if preview.Confirmed || preview.DraftPath != "" || preview.Hint == "" {
		t.Fatalf("preview = %#v", preview)
	}
	if preview.URL != "https://app.example.co.uk/" || preview.RegistrableDomain != "example.co.uk" {
		t.Fatalf("preview = %#v", preview)
	}

	report := learnViaCLI(t, deps, []string{"learn", "--har", harPath, "--url", "app.example.co.uk", "--confirm", "--list-id", "shop", "--title", "Shop", "--catalog-dir", catalogDir, "--data-dir", dataDir})
	if !report.Confirmed || report.DraftPath == "" || report.EvidencePath == "" {
		t.Fatalf("report = %#v", report)
	}
	// One component per exercised area, in taxonomy order. Voice was declared by
	// a page but no request was attributed to it, so it contributes nothing.
	if strings.Join(report.Components, ",") != "core,auth,media" {
		t.Fatalf("components = %v", report.Components)
	}
	for _, host := range []string{"app.example.co.uk", "static.example.co.uk", "auth.example.co.uk", "media.example.co.uk"} {
		if !containsString(report.AcceptedHosts, host) {
			t.Fatalf("host %q was not activated: %v", host, report.AcceptedHosts)
		}
	}
	dependencies := map[string][]string{}
	for _, dependency := range report.Dependencies {
		dependencies[dependency.Host] = dependency.Reasons
	}
	// Telemetry and advertising stay dependencies even though they are same-site
	// and were observed during their own steps.
	for _, host := range []string{"beacon.example.co.uk", "promo.example.co.uk"} {
		if !containsString(dependencies[host], discovery.ReasonOptionalComponent) {
			t.Fatalf("%q must stay a dependency: %#v", host, report.Dependencies)
		}
		if containsString(report.AcceptedHosts, host) {
			t.Fatalf("%q must not be activated: %v", host, report.AcceptedHosts)
		}
	}
	// Shared third parties stay dependencies and are never widened.
	for _, host := range []string{"cdn.thirdparty.test", "login.identityvendor.test"} {
		if !containsString(dependencies[host], discovery.ReasonSharedThirdParty) {
			t.Fatalf("third party %q must stay a dependency: %#v", host, report.Dependencies)
		}
		if containsString(report.SeedDomains, host) {
			t.Fatalf("third party %q became a routing seed: %v", host, report.SeedDomains)
		}
	}
	if report.Relations == 0 {
		t.Fatalf("session provenance was not recorded: %#v", report)
	}

	// The stored evidence is a readable diagnostic and contains no address.
	evidencePayload, err := os.ReadFile(report.EvidencePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"203.0.113", "192.168", "/24"} {
		if strings.Contains(string(evidencePayload), forbidden) {
			t.Fatalf("evidence contains %q:\n%s", forbidden, evidencePayload)
		}
	}
	if !strings.Contains(string(evidencePayload), "beacon.example.co.uk") {
		t.Fatalf("evidence must record what was seen, including what was not activated:\n%s", evidencePayload)
	}

	// The persisted relations use the widened vocabulary and belong to the
	// session source, so they are provenance rather than routing input.
	store, err := sqlite.Open(context.Background(), filepath.Join(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot, err := store.ReadPlanningSnapshot(context.Background(), "shop", map[string]string{}, "raw-v1", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Relations) != 0 {
		t.Fatalf("session provenance must not enter planning: %#v", snapshot.Relations)
	}

	// The learned list is immediately usable by a domain-capable renderer,
	// and its per-component sources are what the planner consumes.
	catalog, err := catalogyaml.Load(context.Background(), catalogDir)
	if err != nil {
		t.Fatal(err)
	}
	definition, found := catalog.List("shop")
	if !found {
		t.Fatal("learned draft did not load")
	}
	if len(definition.Components) != 3 || len(definition.Sources) != 3 {
		t.Fatalf("definition = %#v", definition)
	}
	required := map[string]bool{}
	for _, component := range definition.Components {
		required[component.ID] = component.Required
	}
	for _, component := range []string{domain.ComponentCore, domain.ComponentAuth, domain.ComponentMedia} {
		if !required[component] {
			t.Fatalf("component %q must be required: %#v", component, definition.Components)
		}
	}
	target := domain.TargetDefinition{
		ID: "singbox", FormatKey: singbox.Version, RendererID: singbox.ID,
		Constraints: domain.TargetConstraints{
			SupportsDomainExact: true, SupportsDomainSuffix: true, SupportsIPv4: true, SupportsIPv6: true,
			SupportsPrefixes: true, MaxRules: singbox.MaxEntries, MaxArtifactSize: singbox.MaxArtifactSize,
		},
	}
	plan, err := planner.BuildPlanSet([]planner.ListInput{{Definition: definition}}, target, now)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := singbox.Render(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := singbox.Validate(artifact); err != nil {
		t.Fatalf("artifact from a learned service is invalid: %v", err)
	}
	if !strings.Contains(string(artifact), "example.co.uk") {
		t.Fatalf("artifact = %s", artifact)
	}
	for _, forbidden := range []string{"cdn.thirdparty.test", "login.identityvendor.test", "beacon.example.co.uk"} {
		if strings.Contains(string(artifact), forbidden) {
			t.Fatalf("a dependency reached the artifact: %s", artifact)
		}
	}
}

func TestRefusesAnUnusableScenarioOrArchive(t *testing.T) {
	catalogDir := filepath.Join(t.TempDir(), "catalog")
	dataDir := filepath.Join(t.TempDir(), "data")
	directory := t.TempDir()
	deps := runtimeDeps{Now: func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) }, Context: context.Background()}

	badScenario := filepath.Join(directory, "bad.yaml")
	if err := os.WriteFile(badScenario, []byte("target: https://app.example.com/\nsteps:\n  - id: open\n    component: guessing\n    url: https://app.example.com/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	localScenario := filepath.Join(directory, "local.yaml")
	if err := os.WriteFile(localScenario, []byte("target: http://localhost:8080/\nsteps:\n  - id: open\n    component: core\n    url: http://localhost:8080/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unknownField := filepath.Join(directory, "unknown.yaml")
	if err := os.WriteFile(unknownField, []byte("target: https://app.example.com/\nsteps:\n  - id: open\n    component: core\n    url: https://app.example.com/\n    clicks: 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	badHAR := filepath.Join(directory, "bad.har")
	if err := os.WriteFile(badHAR, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := map[string][]string{
		"unknown component":    {"learn", "--scenario", badScenario, "--confirm"},
		"local target":         {"learn", "--scenario", localScenario, "--confirm"},
		"unknown scenario key": {"learn", "--scenario", unknownField, "--confirm"},
		"malformed archive":    {"learn", "--har", badHAR, "--url", "app.example.com", "--confirm"},
		"missing archive":      {"learn", "--har", filepath.Join(directory, "absent.har"), "--url", "app.example.com", "--confirm"},
		"both sources":         {"learn", "--scenario", unknownField, "--har", badHAR, "--url", "app.example.com"},
		"archive without url":  {"learn", "--har", badHAR, "--confirm"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			stdout, stderr := &syncBuffer{}, &syncBuffer{}
			args = append(args, "--catalog-dir", catalogDir, "--data-dir", dataDir)
			if code := runWithDeps(stdout, stderr, args, deps); code == 0 {
				t.Fatalf("accepted %v: %s", args, stdout.String())
			}
			if _, err := os.Stat(filepath.Join(catalogDir, "local")); err == nil {
				entries, readErr := os.ReadDir(filepath.Join(catalogDir, "local"))
				if readErr == nil && len(entries) != 0 {
					t.Fatalf("a refused learn wrote %d definitions", len(entries))
				}
			}
		})
	}
}

func learnViaCLI(t *testing.T, deps runtimeDeps, args []string) learnReport {
	t.Helper()
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	if code := runWithDeps(stdout, stderr, args, deps); code != 0 {
		t.Fatalf("learn %v failed: code=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
	}
	var report learnReport
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("stdout=%s err=%v", stdout.String(), err)
	}
	return report
}

// TestDiscoveredListAddressesFollowTheObservationLifecycle closes the last
// Discovery Release requirement: a learned list is not a special case, so its
// stale addresses leave the artifact exactly as a built-in list's do.
func TestDiscoveredListAddressesFollowTheObservationLifecycle(t *testing.T) {
	catalogDir := filepath.Join(t.TempDir(), "catalog")
	dataDir := filepath.Join(t.TempDir(), "data")
	// The build output must live inside the data root, exactly as it does for a
	// built-in list.
	outputDir := filepath.Join(dataDir, "out")
	harPath := filepath.Join(t.TempDir(), "session.har")
	if err := os.WriteFile(harPath, []byte(sessionHARFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(catalogDir, "targets"), 0o700); err != nil {
		t.Fatal(err)
	}
	keeneticTarget, err := os.ReadFile(filepath.Join("..", "..", "catalog", "targets", "keenetic.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalogDir, "targets", "keenetic.yaml"), keeneticTarget, 0o600); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	hosts := map[string][]string{
		"app.example.co.uk":    {"192.0.2.10"},
		"static.example.co.uk": {"192.0.2.11"},
		"auth.example.co.uk":   {"192.0.2.12"},
		"media.example.co.uk":  {"192.0.2.13"},
	}
	deps := runtimeDeps{
		Resolver: staticHostResolver{hosts: hosts},
		Now:      func() time.Time { return now },
		Context:  context.Background(),
	}
	report := learnViaCLI(t, deps, []string{"learn", "--har", harPath, "--url", "app.example.co.uk", "--confirm", "--list-id", "shop", "--catalog-dir", catalogDir, "--data-dir", dataDir})
	if report.Sightings == 0 {
		t.Fatalf("the learned list recorded no observation: %#v", report)
	}
	first := buildShopArtifact(t, deps, catalogDir, dataDir, outputDir)
	if !strings.Contains(first, "192.0.2.10") {
		t.Fatalf("first artifact = %s", first)
	}

	// The addresses change and the previous ones expire.
	now = now.Add(2 * time.Hour).Add(time.Second)
	deps.Resolver = staticHostResolver{hosts: map[string][]string{
		"app.example.co.uk":    {"198.51.100.10"},
		"static.example.co.uk": {"198.51.100.11"},
		"auth.example.co.uk":   {"198.51.100.12"},
		"media.example.co.uk":  {"198.51.100.13"},
	}}
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	if code := runWithDeps(stdout, stderr, []string{"refresh", "--list", "shop", "--catalog-dir", catalogDir, "--data-dir", dataDir}, deps); code != 0 {
		t.Fatalf("refresh failed: %s %s", stdout.String(), stderr.String())
	}
	second := buildShopArtifact(t, deps, catalogDir, dataDir, outputDir)
	if !strings.Contains(second, "198.51.100.10") {
		t.Fatalf("second artifact = %s", second)
	}
	for _, expired := range []string{"192.0.2.10", "192.0.2.11", "192.0.2.12", "192.0.2.13"} {
		if strings.Contains(second, expired) {
			t.Fatalf("a stale address of a learned list survived: %s", second)
		}
	}
}

func buildShopArtifact(t *testing.T, deps runtimeDeps, catalogDir, dataDir, outputDir string) string {
	t.Helper()
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	args := []string{"build", "--target", "keenetic", "--list", "shop", "--catalog-dir", catalogDir, "--data-dir", dataDir, "--output", outputDir}
	if code := runWithDeps(stdout, stderr, args, deps); code != 0 {
		t.Fatalf("build failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	path := strings.TrimSpace(stdout.String())
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
