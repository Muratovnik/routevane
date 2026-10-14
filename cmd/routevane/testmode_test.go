//go:build routevane_testmode

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	deployerkeenetic "github.com/Muratovnik/routevane/internal/deployers/keenetic"
	"github.com/Muratovnik/routevane/internal/infrastructure/secretstore"
)

// The test build keeps saved passwords in memory, accepts documentation
// addresses as devices and says it is a test build (ADR 0052).
func TestTestModeComposesTheMemoryStoreAndTheDocumentationRanges(t *testing.T) {
	if !testBuild {
		t.Fatal("the routevane_testmode build does not report itself as a test build")
	}
	if _, ok := newSecretStore().(*secretstore.Memory); !ok {
		t.Fatalf("test build secret store = %T, want the in-memory store", newSecretStore())
	}
	keenetic, ok := deployerRegistry(productionDeps(), deployOptions{})[deployerkeenetic.DeployerID].(*deployerkeenetic.Deployer)
	if !ok {
		t.Fatal("the test registry has no Keenetic deployer")
	}
	for _, address := range []string{"http://192.0.2.10", "http://198.51.100.7", "http://203.0.113.10"} {
		if err := keenetic.ValidateStoredConnection(application.Connection{URL: address, Username: "admin", Interface: "Wireguard0"}); err != nil {
			t.Errorf("test build refused %s: %v", address, err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr, []string{"version"}); code != 0 || !strings.HasSuffix(strings.TrimSpace(stdout.String()), "(test build)") {
		t.Fatalf("version = %q (exit %d)", stdout.String(), code)
	}
}

// A test build opens no device connection: the device it would reach answers
// nowhere, and no suite may reach a real one, so the dial is refused before any
// packet leaves and the refusal is immediate rather than a connect timeout.
func TestTestModeOpensNoDeviceConnection(t *testing.T) {
	deps := normalizeDeps(runtimeDeps{})
	keenetic := deployerRegistry(deps, deployOptions{})[deployerkeenetic.DeployerID]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	password := rand.Text()
	started := time.Now()
	_, err := keenetic.Probe(ctx, application.Connection{URL: "http://192.0.2.10", Username: "admin", Password: password, Interface: "Wireguard0"})
	// The deployer reports a transport failure as text, so the refusal is
	// recognised by its own words.
	if err == nil || !strings.Contains(err.Error(), errTestBuildDial.Error()) {
		t.Fatalf("probe error = %v, want the test build's dial refusal", err)
	}
	if strings.Contains(err.Error(), password) {
		t.Fatal("the refusal carries the password")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("the refusal took %s; a dial was attempted", elapsed)
	}
}
