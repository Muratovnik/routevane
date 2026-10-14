//go:build !routevane_testmode

package netpolicy

import "net/netip"

// testBuildDevice is the test build's documentation-range allowance. A release
// build has none: this file is what it compiles, so no address is added to the
// device policy (ADR 0052).
func testBuildDevice(netip.Addr) bool { return false }
