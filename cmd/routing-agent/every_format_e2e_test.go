package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/renderers/amnezia"
	"github.com/Muratovnik/routevane/internal/renderers/keenetic"
	"github.com/Muratovnik/routevane/internal/renderers/keeneticdns"
	"github.com/Muratovnik/routevane/internal/renderers/mikrotik"
	"github.com/Muratovnik/routevane/internal/renderers/openwrtnftset"
	"github.com/Muratovnik/routevane/internal/renderers/singbox"
)

// TestPublishesEveryBuiltInFormatFromOneProfile is the visible outcome of
// publishing every built-in device format: one profile and one refresh reach
// four device formats, each carrying what it can honestly express and nothing
// more.
func TestPublishesEveryBuiltInFormatFromOneProfile(t *testing.T) {
	catalogDir := filepath.Join(t.TempDir(), "catalog")
	dataDir := filepath.Join(t.TempDir(), "data")
	outputDir := filepath.Join(dataDir, "out")
	for _, directory := range []string{filepath.Join(catalogDir, "builtin"), filepath.Join(catalogDir, "targets")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "testdata", "expiry", "catalog", "builtin", "youtube.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalogDir, "builtin", "youtube.yaml"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"keenetic.yaml", "keenetic-dns.yaml", "singbox.yaml", "openwrt.yaml", "mikrotik.yaml", "amnezia.yaml"} {
		payload, err := os.ReadFile(filepath.Join("..", "..", "catalog", "targets", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(catalogDir, "targets", name), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	resolver := &hostAddressResolver{}
	resolver.set(map[string][]string{"youtube.expiry.test": {"192.0.2.10"}})
	deps := runtimeDeps{Resolver: resolver, Now: func() time.Time { return now }, Context: context.Background()}

	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	if code := runWithDeps(stdout, stderr, []string{"refresh", "--service", "youtube", "--catalog-dir", catalogDir, "--data-dir", dataDir}, deps); code != 0 {
		t.Fatalf("refresh failed: %s %s", stdout.String(), stderr.String())
	}

	router := buildArtifactFile(t, deps, catalogDir, dataDir, outputDir, "keenetic")
	client := buildArtifactFile(t, deps, catalogDir, dataDir, outputDir, "singbox")
	device := buildArtifactFile(t, deps, catalogDir, dataDir, outputDir, "openwrt")
	script := buildArtifactFile(t, deps, catalogDir, dataDir, outputDir, "mikrotik")
	splitTunnel := buildArtifactFile(t, deps, catalogDir, dataDir, outputDir, "amnezia")
	names := buildArtifactFile(t, deps, catalogDir, dataDir, outputDir, "keenetic-dns")

	// The same profile reaches the two Keenetic mechanisms differently: the
	// static-route file freezes the observed address, and the FQDN group carries
	// the name, because that device expands its subdomains itself.
	groups, err := keeneticdns.Parse(names.payload)
	if err != nil {
		t.Fatalf("group file invalid: %v\n%s", err, names.payload)
	}
	if len(groups) != 1 || groups[0].Name != "routevane-youtube" {
		t.Fatalf("groups = %#v", groups)
	}
	if !containsEntry(groups[0].Entries, "youtube.com") {
		t.Fatalf("group entries = %#v", groups[0].Entries)
	}

	if !strings.HasSuffix(device.path, ".conf") {
		t.Fatalf("fragment path = %s", device.path)
	}
	rules, err := openwrtnftset.Parse(device.payload)
	if err != nil {
		t.Fatalf("fragment invalid: %v\n%s", err, device.payload)
	}
	if len(rules) != 1 || rules[0].Suffix != "youtube.com" {
		t.Fatalf("fragment rules = %#v", rules)
	}

	// The device resolves the domain itself, so the observed address the router
	// format needs must not appear in the fragment at all.
	if strings.Contains(string(device.payload), "192.0.2.10") {
		t.Fatalf("a dynamic set fragment must not freeze an observed address:\n%s", device.payload)
	}
	if !strings.Contains(string(router.payload), "route ADD 192.0.2.10") {
		t.Fatalf("router artifact = %s", router.payload)
	}

	// The address-list format cannot express a suffix, so the same profile
	// reaches it as the observed address instead. One plan, two honest
	// projections.
	if !strings.HasSuffix(script.path, ".rsc") {
		t.Fatalf("script path = %s", script.path)
	}
	entries, err := mikrotik.Parse(script.payload)
	if err != nil {
		t.Fatalf("script invalid: %v\n%s", err, script.payload)
	}
	if len(entries.IPv4) != 1 || entries.IPv4[0] != "192.0.2.10/32" || len(entries.IPv6) != 0 {
		t.Fatalf("script entries = %#v", entries)
	}
	if strings.Contains(string(script.payload), "youtube.com") {
		t.Fatalf("a suffix must not be carried as an address-list entry:\n%s", script.payload)
	}
	// The script replaces what it owns rather than adding to it.
	if !strings.Contains(string(script.payload), "remove [find list=routevane4]") {
		t.Fatalf("script = %s", script.payload)
	}

	// The client resolves each site itself, so its list carries the observed
	// address as a host prefix and no populated address list.
	sites, err := amnezia.Parse(splitTunnel.payload)
	if err != nil {
		t.Fatalf("site list invalid: %v\n%s", err, splitTunnel.payload)
	}
	if len(sites) != 1 || sites[0].Hostname != "192.0.2.10/32" {
		t.Fatalf("site list = %#v", sites)
	}

	// Five formats, five validators, no cross-acceptance. Two of them even share
	// a file extension, so identity comes from the validator rather than the
	// suffix.
	artifacts := map[string][]byte{
		"keenetic": router.payload, "singbox": client.payload,
		"openwrt": device.payload, "mikrotik": script.payload, "amnezia": splitTunnel.payload,
		"keenetic-dns": names.payload,
	}
	validators := map[string]func([]byte) error{
		"keenetic": keenetic.Validate, "singbox": singbox.Validate,
		"openwrt": openwrtnftset.Validate, "mikrotik": mikrotik.Validate,
		"amnezia": amnezia.Validate, "keenetic-dns": keeneticdns.Validate,
	}
	for owner, validate := range validators {
		for name, payload := range artifacts {
			err := validate(payload)
			if name == owner && err != nil {
				t.Fatalf("%s artifact failed its own validator: %v", name, err)
			}
			if name != owner && err == nil {
				t.Fatalf("the %s validator accepted the %s artifact", owner, name)
			}
		}
	}
}

type builtArtifact struct {
	path    string
	payload []byte
}

func buildArtifactFile(t *testing.T, deps runtimeDeps, catalogDir, dataDir, outputDir, target string) builtArtifact {
	t.Helper()
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	args := []string{"build", "--target", target, "--service", "youtube", "--catalog-dir", catalogDir, "--data-dir", dataDir, "--output", outputDir}
	if code := runWithDeps(stdout, stderr, args, deps); code != 0 {
		t.Fatalf("build %s failed: code=%d stdout=%s stderr=%s", target, code, stdout.String(), stderr.String())
	}
	path := strings.TrimSpace(stdout.String())
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return builtArtifact{path: path, payload: payload}
}

func containsEntry(entries []string, want string) bool {
	for _, entry := range entries {
		if entry == want {
			return true
		}
	}
	return false
}
