//go:build routevane_testmode

package netpolicy

import "net/netip"

// testNetDevices are the RFC 5737 documentation ranges. No device answers on
// them, so a test build can register, save and preview a connection end to end
// without the address naming anyone's router (ADR 0052).
var testNetDevices = [...]netip.Prefix{
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
}

// testBuildDevice widens the device policy by the documentation ranges and by
// nothing else. It exists only in a binary built with the routevane_testmode
// tag; the release build compiles testnet_release.go instead.
func testBuildDevice(address netip.Addr) bool {
	for _, prefix := range testNetDevices {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
