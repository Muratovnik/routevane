//go:build !routevane_testmode

package netpolicy

import (
	"errors"
	"net/netip"
	"testing"
)

// A release build refuses every documentation address as a device: a TEST-NET
// connection is something only a test build may register (ADR 0052).
func TestReleaseBuildRefusesDocumentationAddressesAsDevices(t *testing.T) {
	for _, value := range documentationAddresses {
		if DeviceDestination(netip.MustParseAddr(value)) {
			t.Errorf("release build accepted %s as a device", value)
		}
	}
	if _, err := ValidateDeviceURL("http://192.0.2.10"); !errors.Is(err, ErrNotADeviceDestination) {
		t.Fatalf("release build accepted a documentation device URL: %v", err)
	}
	if AllDeviceDestinations([]netip.Addr{netip.MustParseAddr("10.255.0.1"), netip.MustParseAddr("203.0.113.10")}) {
		t.Fatal("a mixed answer with a documentation address must be refused whole")
	}
}
