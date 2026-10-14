//go:build !routevane_testmode

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Muratovnik/routevane/internal/application"
	deployerkeenetic "github.com/Muratovnik/routevane/internal/deployers/keenetic"
	"github.com/Muratovnik/routevane/internal/infrastructure/secretstore"
	"github.com/Muratovnik/routevane/internal/netpolicy"
)

// The binary a release ships is composed with the operating system's store, the
// standard device dialer and the release device policy: a documentation address
// is not a device, and the version says nothing about a test build (ADR 0052).
// Nothing here reads or writes the credential store.
func TestReleaseBuildComposesTheOperatingSystemStoreAndTheReleasePolicy(t *testing.T) {
	if testBuild {
		t.Fatal("a build without the routevane_testmode tag reports itself as a test build")
	}
	if _, ok := newSecretStore().(secretstore.Store); !ok {
		t.Fatalf("release secret store = %T, want the operating system store", newSecretStore())
	}
	deps := productionDeps()
	if deps.DeviceDialer != nil {
		t.Fatalf("release device dialer = %T, want the standard library's", deps.DeviceDialer)
	}
	keenetic, ok := deployerRegistry(deps, deployOptions{})[deployerkeenetic.DeployerID].(*deployerkeenetic.Deployer)
	if !ok {
		t.Fatal("the release registry has no Keenetic deployer")
	}
	for _, address := range []string{"http://192.0.2.10", "http://198.51.100.7", "http://203.0.113.10"} {
		err := keenetic.ValidateStoredConnection(application.Connection{URL: address, Username: "admin", Interface: "Wireguard0"})
		if !errors.Is(err, netpolicy.ErrNotADeviceDestination) {
			t.Errorf("release build stored %s as a device: %v", address, err)
		}
	}
	if err := keenetic.ValidateStoredConnection(application.Connection{URL: "http://10.255.0.1", Username: "admin", Interface: "Wireguard0"}); err != nil {
		t.Fatalf("release build refused a private device address: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr, []string{"version"}); code != 0 || strings.Contains(stdout.String(), "test build") {
		t.Fatalf("version = %q (exit %d)", stdout.String(), code)
	}
}
