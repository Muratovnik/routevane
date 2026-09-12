package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
)

const keeneticTargetYAML = `id: keenetic
format_key: keenetic-bat-ipv4-v1
kind: router
renderer: keenetic-route-bat
constraints:
  supports_domain_exact: false
  supports_domain_suffix: false
  supports_dynamic_dns_set: false
  supports_ipv4: true
  supports_ipv6: false
  supports_prefixes: true
  max_rules: 1024
  max_artifact_size: 131072
renderer_options: []
manual_installation_hint: SENTINEL-HINT must never enter artifacts or logs.
`

type alphaBetaResolver struct{}

func (*alphaBetaResolver) LookupHost(_ context.Context, name string) ([]string, error) {
	switch name {
	case "alpha.example":
		return []string{"192.0.2.10", "192.0.2.10"}, nil
	case "beta.example":
		return []string{"198.51.100.20"}, nil
	default:
		return nil, context.DeadlineExceeded
	}
}

func (*alphaBetaResolver) LookupCNAME(context.Context, string) (string, error) { return "", nil }

func TestCommandRefreshesTwoListsReopensStateAndBuildsValidatedKeeneticFile(t *testing.T) {
	catalogRoot := writeAlphaBetaCatalog(t)
	dataRoot := filepath.Join(t.TempDir(), "data")
	outputDir := filepath.Join(dataRoot, "exports")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	deps := runtimeDeps{Resolver: &alphaBetaResolver{}, Now: func() time.Time { return now }}

	for _, listID := range []string{"beta", "alpha"} {
		runCommand(t, deps, []string{"refresh", "--list", listID, "--catalog-dir", catalogRoot, "--data-dir", dataRoot}, 0)
	}
	result := runCommand(t, deps, []string{"build", "--target", "keenetic", "--list", "beta", "--list", "alpha", "--list", "beta", "--output", outputDir, "--catalog-dir", catalogRoot, "--data-dir", dataRoot}, 0)
	if strings.Count(result.stdout, "\n") != 1 {
		t.Fatalf("build stdout must contain exactly one path: %q", result.stdout)
	}
	path := strings.TrimSpace(result.stdout)
	if !filepath.IsAbs(path) || filepath.Dir(path) != outputDir || filepath.Ext(path) != ".bat" {
		t.Fatalf("visible output path=%q", path)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := keenetic.Parse(payload)
	if err != nil {
		t.Fatalf("independent Keenetic validation failed: %v", err)
	}
	if len(rules) != 2 || rules[0].Prefix.String() != "192.0.2.10/32" || rules[1].Prefix.String() != "198.51.100.20/32" {
		t.Fatalf("parsed routes=%#v", rules)
	}
	if strings.Contains(string(payload), "SENTINEL") || strings.Contains(result.stderr, "SENTINEL") || strings.Contains(result.stderr, path) {
		t.Fatalf("catalog text or output path leaked: stderr=%q payload=%q", result.stderr, payload)
	}
	if !hasStructuredWarning(result.stderr, "partial_coverage") {
		t.Fatalf("partial coverage warning is not clear and structured: %q", result.stderr)
	}

	again := runCommand(t, deps, []string{"build", "--target", "keenetic", "--list", "alpha", "--list", "beta", "--output", outputDir, "--catalog-dir", catalogRoot, "--data-dir", dataRoot}, 0)
	if strings.TrimSpace(again.stdout) != path {
		t.Fatalf("identical list set did not reuse deterministic output: %q != %q", again.stdout, path)
	}
}

func TestCommandMissingSecondListStateCreatesNoFile(t *testing.T) {
	catalogRoot := writeAlphaBetaCatalog(t)
	dataRoot := filepath.Join(t.TempDir(), "data")
	outputDir := filepath.Join(dataRoot, "exports")
	deps := runtimeDeps{Resolver: &alphaBetaResolver{}, Now: func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }}
	runCommand(t, deps, []string{"refresh", "--list", "alpha", "--catalog-dir", catalogRoot, "--data-dir", dataRoot}, 0)
	result := runCommand(t, deps, []string{"build", "--target", "keenetic", "--list", "alpha", "--list", "beta", "--output", outputDir, "--catalog-dir", catalogRoot, "--data-dir", dataRoot}, 1)
	if result.stdout != "" {
		t.Fatalf("failed all-or-nothing build printed output: %q", result.stdout)
	}
	if entries, err := os.ReadDir(outputDir); err == nil && len(entries) != 0 {
		t.Fatalf("failed all-or-nothing build created files: %v", entries)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestCommandBuildExcludesPrefixesContainingLocalSubnets(t *testing.T) {
	catalogRoot := writeAlphaBetaCatalog(t)
	listPath := filepath.Join(catalogRoot, "builtin", "alpha.yaml")
	list, err := os.ReadFile(listPath)
	if err != nil {
		t.Fatal(err)
	}
	seeds := ""
	for _, value := range []string{"172.0.0.0/10", "100.0.0.0/9", "169.0.0.0/8", "198.18.0.0/15"} {
		seeds += "  - kind: prefix4\n    value: " + value + "\n    component: web\n    source: manual\n"
	}
	list = []byte(strings.Replace(string(list), "sources:\n", seeds+"sources:\n", 1))
	if err := os.WriteFile(listPath, list, 0o600); err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Join(t.TempDir(), "data")
	deps := runtimeDeps{Resolver: &alphaBetaResolver{}, Now: func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }}
	runCommand(t, deps, []string{"refresh", "--list", "alpha", "--catalog-dir", catalogRoot, "--data-dir", dataRoot}, 0)
	result := runCommand(t, deps, []string{"build", "--target", "keenetic", "--list", "alpha", "--output", filepath.Join(dataRoot, "exports"), "--catalog-dir", catalogRoot, "--data-dir", dataRoot}, 0)
	payload, err := os.ReadFile(strings.TrimSpace(result.stdout))
	if err != nil {
		t.Fatal(err)
	}
	routes, err := keenetic.Parse(payload)
	if err != nil {
		t.Fatal(err)
	}
	// The real exported router file keeps both the documentation address and
	// the allowed synthetic benchmark network, but none of the mixed prefixes.
	if len(routes) != 2 || routes[0].Prefix.String() != "192.0.2.10/32" || routes[1].Prefix.String() != "198.18.0.0/15" {
		t.Fatalf("exported routes = %#v", routes)
	}
}

func TestParseBuildCanonicalizesRepeatedListsAndPreservesRawCompatibility(t *testing.T) {
	options, ok := parseBuild([]string{"--target", "keenetic", "--list", "beta", "--list", "alpha", "--list", "beta"})
	if !ok || strings.Join(options.ListIDs, ",") != "alpha,beta" {
		t.Fatalf("repeatable list parse=%#v ok=%v", options, ok)
	}
	if _, ok := parseBuild([]string{"--target", "raw-json", "--list", "alpha", "--list", "beta"}); ok {
		t.Fatal("multi-list raw JSON compatibility boundary was widened")
	}
	if _, ok := parseBuild([]string{"--target", "raw-json", "--list", "alpha"}); !ok {
		t.Fatal("single-list raw JSON build stopped parsing")
	}
}

func writeAlphaBetaCatalog(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, directory := range []string{"builtin", "targets"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	list := func(id string) string {
		return "id: " + id + "\n" +
			"title: SENTINEL-TITLE-" + id + "\n" +
			"components:\n  web:\n    required: true\n" +
			"seeds:\n  - kind: domain_suffix\n    value: " + id + ".example\n    component: web\n    source: manual\n" +
			"sources:\n  - id: dns-main\n    type: dns\n    component: web\n    config:\n      names: [" + id + ".example]\n"
	}
	for _, id := range []string{"alpha", "beta"} {
		if err := os.WriteFile(filepath.Join(root, "builtin", id+".yaml"), []byte(list(id)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "targets", "keenetic.yaml"), []byte(keeneticTargetYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func hasStructuredWarning(stderr, wantCode string) bool {
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) == nil && record["code"] == wantCode && record["operation"] == "build" {
			return true
		}
	}
	return false
}
