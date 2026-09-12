package netpolicy

import (
	"net/netip"
	"testing"
)

func TestPublicUnicastRefusesLocalAndSpecialUseAddresses(t *testing.T) {
	refused := []string{
		"127.0.0.1", "::1", "0.0.0.0", "::",
		"10.0.0.5", "172.16.9.9", "192.168.1.1", "fd00::1",
		"169.254.10.10", "169.254.169.254", "fe80::1",
		"100.64.0.1", "192.0.0.1", "198.18.0.1", "198.19.255.255",
		"224.0.0.1", "ff02::1",
		"fe80::1%eth0", "fd00::1%eth0", "ff02::1%eth0",
		"64:ff9b::c000:221", "2002::1", "2001:0:1::1",
		// An IPv4-mapped address is judged as the IPv4 destination it is.
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254",
	}
	for _, value := range refused {
		t.Run("refused/"+value, func(t *testing.T) {
			if PublicUnicast(netip.MustParseAddr(value)) {
				t.Fatalf("accepted %q", value)
			}
		})
	}
	accepted := []string{"203.0.113.10", "192.0.2.1", "198.51.100.7", "2001:db8::1", "8.8.8.8", "::ffff:192.0.2.1"}
	for _, value := range accepted {
		t.Run("accepted/"+value, func(t *testing.T) {
			if !PublicUnicast(netip.MustParseAddr(value)) {
				t.Fatalf("refused %q", value)
			}
		})
	}
	if PublicUnicast(netip.Addr{}) {
		t.Fatal("accepted the zero address")
	}
}

func TestAllPublicUnicastRefusesAnEmptyOrMixedAnswer(t *testing.T) {
	public := netip.MustParseAddr("203.0.113.10")
	local := netip.MustParseAddr("127.0.0.1")
	if AllPublicUnicast(nil) {
		t.Fatal("accepted an empty answer")
	}
	if !AllPublicUnicast([]netip.Addr{public, netip.MustParseAddr("2001:db8::1")}) {
		t.Fatal("refused an all-public answer")
	}
	if AllPublicUnicast([]netip.Addr{public, local}) {
		t.Fatal("a split answer containing a local address must be refused whole")
	}
	if AllPublicUnicast([]netip.Addr{local, public}) {
		t.Fatal("order must not change the decision")
	}
}

func TestRoutablePrefixChecksTheWholeRange(t *testing.T) {
	for _, value := range []string{
		"172.0.0.0/10", "100.0.0.0/9", "169.0.0.0/8", "192.0.0.0/8",
		"2001::/16", "fe00::/8", "::/0", "::ffff:172.0.0.0/106",
	} {
		if RoutablePrefix(netip.MustParsePrefix(value)) {
			t.Errorf("accepted prefix containing forbidden destinations: %s", value)
		}
	}
	for _, value := range []string{
		"172.0.0.0/12", "172.32.0.0/11", "100.0.0.0/10", "100.128.0.0/9",
		"169.0.0.0/9", "169.255.0.0/16", "192.0.2.0/24", "198.51.100.0/24",
		"198.18.0.0/15", "203.0.113.0/24", "2001:db8::/32", "::ffff:192.0.2.0/120",
	} {
		if !RoutablePrefix(netip.MustParsePrefix(value)) {
			t.Errorf("refused allowed prefix: %s", value)
		}
	}
	if RoutablePrefix(netip.Prefix{}) {
		t.Fatal("accepted invalid prefix")
	}
}

func FuzzRoutablePrefixNeverContainsForbiddenAddress(f *testing.F) {
	f.Add("172.0.0.0/10", []byte{172, 16, 0, 1})
	f.Add("2001:db8::/32", netip.MustParseAddr("2001:db8::1").AsSlice())
	f.Fuzz(func(t *testing.T, value string, raw []byte) {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !RoutablePrefix(prefix) {
			return
		}
		address, ok := netip.AddrFromSlice(raw)
		if ok && prefix.Contains(address) && !RoutableDestination(address) {
			t.Fatalf("allowed prefix %v contains forbidden %v", prefix, address)
		}
	})
}

func TestLocalHostnameRefusesNamesForThisMachine(t *testing.T) {
	local := []string{"", "localhost", "localhost.localdomain", "ip6-localhost", "ip6-loopback",
		"router.localhost", "printer.local", "host.localdomain", "service.internal", "device.home.arpa"}
	for _, value := range local {
		if !LocalHostname(value) {
			t.Fatalf("accepted local hostname %q", value)
		}
	}
	remote := []string{"example.com", "www.example.co.uk", "localhostile.example.com", "local.example.com", "internal.example.com"}
	for _, value := range remote {
		if LocalHostname(value) {
			t.Fatalf("refused remote hostname %q", value)
		}
	}
}

func FuzzPublicUnicastNeverAcceptsALocalAddress(f *testing.F) {
	f.Add([]byte{127, 0, 0, 1})
	f.Add([]byte{10, 0, 0, 1})
	f.Add([]byte{203, 0, 113, 10})
	f.Add(netip.MustParseAddr("fd00::1").AsSlice())
	f.Fuzz(func(t *testing.T, raw []byte) {
		address, ok := netip.AddrFromSlice(raw)
		if !ok || !PublicUnicast(address) {
			return
		}
		normalized := address.Unmap()
		if normalized.IsLoopback() || normalized.IsPrivate() || normalized.IsUnspecified() ||
			normalized.IsMulticast() || normalized.IsLinkLocalUnicast() || normalized.IsLinkLocalMulticast() ||
			normalized.IsInterfaceLocalMulticast() || !normalized.IsGlobalUnicast() {
			t.Fatalf("accepted a non-public address: %v", address)
		}
	})
}
