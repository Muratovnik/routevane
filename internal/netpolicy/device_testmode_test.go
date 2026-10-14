//go:build routevane_testmode

package netpolicy

import (
	"net/netip"
	"testing"
)

// TestTestMode... names are what the tagged gate runs (tools/dev.ps1).

// A test build accepts every documentation address as a device, as an address
// literal and inside a device URL, so a suite never needs a real LAN address.
func TestTestModeAcceptsDocumentationAddressesAsDevices(t *testing.T) {
	for _, value := range documentationAddresses {
		if !DeviceDestination(netip.MustParseAddr(value)) {
			t.Errorf("test build refused %s as a device", value)
		}
	}
	for _, raw := range []string{"http://192.0.2.10", "https://198.51.100.7/", "http://203.0.113.10:8080"} {
		if _, err := ValidateDeviceURL(raw); err != nil {
			t.Errorf("test build refused %s: %v", raw, err)
		}
	}
}

// The allowance does not reach anything the release policy refuses for
// another reason.
func TestTestModeKeepsTheReleasePolicyElsewhere(t *testing.T) {
	TestDevicePolicyIsTheSameOutsideTheDocumentationRanges(t)
}
