package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/infrastructure/catalogyaml"
	"github.com/Muratovnik/routevane/internal/infrastructure/filesystem"
	"github.com/Muratovnik/routevane/internal/renderers/rawjson"
)

const exampleServiceYAML = `id: example
title: Example
components:
  web:
    required: true
seeds:
  - kind: domain_suffix
    value: example.com
    component: web
    source: manual
sources:
  - id: dns-main
    type: dns
    component: web
    config:
      names: [example.com]
`

type cannedAddressResolver struct {
	addresses []string
	err       error
}

func (r *cannedAddressResolver) LookupHost(context.Context, string) ([]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	return append([]string(nil), r.addresses...), nil
}

func (*cannedAddressResolver) LookupCNAME(context.Context, string) (string, error) { return "", nil }

func TestCommandPipelinePersistsLifecycleAndBuildsVisibleArtifact(t *testing.T) {
	catalogRoot := writeExampleCatalog(t, exampleServiceYAML)
	if _, err := filesystem.ResolveRoot(catalogRoot); err != nil {
		t.Fatalf("resolve test catalog: %v", err)
	}
	if _, err := catalogyaml.Load(context.Background(), catalogRoot); err != nil {
		t.Fatalf("load test catalog: %v", err)
	}
	dataRoot := filepath.Join(t.TempDir(), "data")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	resolver := &cannedAddressResolver{addresses: []string{"192.0.2.2", "::ffff:192.0.2.1", "192.0.2.2"}}
	deps := runtimeDeps{Resolver: resolver, Now: func() time.Time { return now }}

	refreshArgs := []string{"refresh", "--service", "example", "--catalog-dir", catalogRoot, "--data-dir", dataRoot}
	buildArgs := []string{"build", "--target", "raw-json", "--service", "example", "--catalog-dir", catalogRoot, "--data-dir", dataRoot}
	doctorArgs := []string{"doctor", "--catalog-dir", catalogRoot, "--data-dir", dataRoot}

	firstSummary := runCommand(t, deps, refreshArgs, 0)
	if !strings.Contains(firstSummary.stdout, `"sightings":2`) {
		t.Fatalf("refresh summary = %s", firstSummary.stdout)
	}
	resolver.addresses = []string{"192.0.2.1", "192.0.2.2", "192.0.2.1"}
	now = now.Add(time.Hour)
	runCommand(t, deps, refreshArgs, 0)

	initialBuild := runCommand(t, deps, buildArgs, 0)
	artifactPath := strings.TrimSpace(initialBuild.stdout)
	plan := readPlan(t, artifactPath)
	if len(plan.Sightings) != 2 || plan.Sightings[0].ObservationCount != 2 || plan.Sightings[0].FirstSeen != "2026-08-20T12:00:00Z" || plan.Sightings[0].Validity != "valid" {
		t.Fatalf("persisted sightings = %#v", plan.Sightings)
	}
	idempotent := runCommand(t, deps, buildArgs, 0)
	if strings.TrimSpace(idempotent.stdout) != artifactPath {
		t.Fatalf("explicit-input build path changed: %q != %q", idempotent.stdout, artifactPath)
	}

	now = time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC) // equality with T1 + 2h
	staleBuild := runCommand(t, deps, buildArgs, 0)
	stale := readPlan(t, strings.TrimSpace(staleBuild.stdout))
	if stale.Sightings[0].Validity != "stale" || !excludedFor(stale, "stale_observation") {
		t.Fatalf("expiry equality did not exclude stale observation: %#v", stale)
	}

	now = time.Date(2026, 11, 18, 15, 0, 0, 0, time.UTC) // stale retention equality
	archivedBuild := runCommand(t, deps, buildArgs, 0)
	archived := readPlan(t, strings.TrimSpace(archivedBuild.stdout))
	if archived.Sightings[0].Validity != "archived" {
		t.Fatalf("retention equality = %q, want archived", archived.Sightings[0].Validity)
	}

	resolver.addresses = []string{"192.0.2.2", "192.0.2.1"}
	runCommand(t, deps, refreshArgs, 0)
	revivedBuild := runCommand(t, deps, buildArgs, 0)
	revived := readPlan(t, strings.TrimSpace(revivedBuild.stdout))
	if revived.Sightings[0].Validity != "valid" || revived.Sightings[0].FirstSeen != "2026-08-20T12:00:00Z" || revived.Sightings[0].ObservationCount != 3 {
		t.Fatalf("revived sighting = %#v", revived.Sightings[0])
	}

	resolver.err = errors.New("SENTINEL-SECRET resolver payload")
	failed := runCommand(t, deps, refreshArgs, 1)
	if strings.Contains(failed.stderr, "SENTINEL-SECRET") {
		t.Fatalf("hostile resolver error leaked: %s", failed.stderr)
	}
	afterFailure := readPlan(t, strings.TrimSpace(runCommand(t, deps, buildArgs, 0).stdout))
	if afterFailure.Sightings[0].ObservationCount != 3 {
		t.Fatalf("failed DNS cycle changed observations: %#v", afterFailure.Sightings[0])
	}

	beforeFiles := jsonFiles(t, dataRoot)
	now = now.Add(time.Second)
	deps.ArtifactWriter.BeforeRename = func(string) error { return errors.New("injected") }
	failedBuild := runCommand(t, deps, buildArgs, 1)
	if strings.TrimSpace(failedBuild.stdout) != "" {
		t.Fatalf("failed build exposed path: %q", failedBuild.stdout)
	}
	if got := jsonFiles(t, dataRoot); !equalStrings(got, beforeFiles) {
		t.Fatalf("failed build changed final artifacts: %v != %v", got, beforeFiles)
	}
	deps.ArtifactWriter.BeforeRename = nil

	doctor := runCommand(t, deps, doctorArgs, 0)
	var report doctorReport
	if err := json.Unmarshal([]byte(doctor.stdout), &report); err != nil || !report.Healthy {
		t.Fatalf("doctor report = %s, err=%v", doctor.stdout, err)
	}
}

type commandResult struct{ stdout, stderr string }

func runCommand(t *testing.T, deps runtimeDeps, args []string, wantCode int) commandResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runWithDeps(&stdout, &stderr, args, deps)
	if code != wantCode {
		t.Fatalf("run(%v) code=%d want=%d stdout=%q stderr=%q", args, code, wantCode, stdout.String(), stderr.String())
	}
	return commandResult{stdout: stdout.String(), stderr: stderr.String()}
}

func writeExampleCatalog(t *testing.T, payload string) string {
	t.Helper()
	root := t.TempDir()
	builtin := filepath.Join(root, "builtin")
	if err := os.MkdirAll(builtin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(builtin, "example.yaml"), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func readPlan(t *testing.T, path string) rawjson.Plan {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := rawjson.Validate(payload); err != nil {
		t.Fatalf("visible artifact invalid: %v", err)
	}
	var plan rawjson.Plan
	if err := json.Unmarshal(payload, &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func excludedFor(plan rawjson.Plan, reason string) bool {
	for _, excluded := range plan.Excluded {
		for _, got := range excluded.ReasonCodes {
			if got == reason {
				return true
			}
		}
	}
	return false
}

func jsonFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() && filepath.Ext(path) == ".json" {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
