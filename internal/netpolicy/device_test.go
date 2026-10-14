package netpolicy

import (
	"net/netip"
	"testing"
)

// documentationAddresses are the edges and an inside address of each RFC 5737
// range. Only a test build may treat them as devices.
var documentationAddresses = []string{
	"192.0.2.0", "192.0.2.10", "192.0.2.255",
	"198.51.100.0", "198.51.100.7", "198.51.100.255",
	"203.0.113.0", "203.0.113.10", "203.0.113.255",
	"::ffff:192.0.2.10",
}

// The release policy's own facts hold in every build: the test allowance adds
// the documentation ranges and nothing else.
func TestDevicePolicyIsTheSameOutsideTheDocumentationRanges(t *testing.T) {
	for _, value := range []string{"10.255.0.1", "172.16.9.9", "192.168.100.7", "169.254.10.10", "fd00::1", "fe80::1"} {
		if !DeviceDestination(netip.MustParseAddr(value)) {
			t.Errorf("refused local device address %s", value)
		}
	}
	for _, value := range []string{
		"127.0.0.1", "0.0.0.0", "169.254.169.254", "100.64.0.1", "8.8.8.8",
		// Neighbours of the documentation ranges stay public.
		"192.0.1.255", "192.0.3.0", "198.51.99.255", "198.51.101.0", "203.0.112.255", "203.0.114.0",
		"198.18.0.1", "224.0.0.1", "2001:db8::1", "::1",
	} {
		if DeviceDestination(netip.MustParseAddr(value)) {
			t.Errorf("accepted %s as a device", value)
		}
	}
	if _, err := ValidateDeviceURL("http://10.255.0.1"); err != nil {
		t.Fatalf("a private literal must stay a device URL: %v", err)
	}
}
