package netpolicy

import (
	"errors"
	"net/netip"
	"net/url"
	"strings"
)

// ErrNotADeviceDestination reports an address that is not a plausible local
// network device.
var ErrNotADeviceDestination = errors.New("destination is not a local network device")

// DeviceDestination is the narrowly named exception to the public-unicast
// policy: deploying to the operator's own router necessarily contacts a private
// address, and nothing else in this product may.
//
// It is deliberately not the inverse of PublicUnicast. It permits private and
// link-local unicast only, and continues to refuse loopback, the unspecified
// address, multicast, carrier-grade NAT, the cloud metadata address, and every
// other special-use range, so widening this allowance cannot reach a service
// running on the machine itself or a metadata endpoint.
func DeviceDestination(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || address.IsUnspecified() || address.IsLoopback() ||
		address.IsMulticast() || address.IsLinkLocalMulticast() || address.IsInterfaceLocalMulticast() {
		return false
	}
	if address.Is4() {
		octets := address.As4()
		switch {
		case octets[0] == 10:
			return true
		case octets[0] == 172 && octets[1] >= 16 && octets[1] <= 31:
			return true
		case octets[0] == 192 && octets[1] == 168:
			return true
		case octets[0] == 169 && octets[1] == 254:
			// Link-local unicast is how a device answers before it has a lease,
			// but the cloud metadata address is never a router.
			return octets[2] != 169 || octets[3] != 254
		}
		return false
	}
	// IPv6: unique-local and link-local unicast only.
	return address.IsPrivate() || address.IsLinkLocalUnicast()
}

// AllDeviceDestinations reports whether every candidate address is a plausible
// local device. A mixed answer is refused whole, exactly as the public policy
// refuses one, so a name that resolves to both a device and a public host cannot
// be used to reach the public host with device credentials.
func AllDeviceDestinations(candidates []netip.Addr) bool {
	if len(candidates) == 0 {
		return false
	}
	for _, candidate := range candidates {
		if !DeviceDestination(candidate) {
			return false
		}
	}
	return true
}

// ValidateDeviceURL accepts only an explicit http or https URL whose host is a
// local device address literal.
//
// A hostname is refused on purpose: a device URL must be unambiguous at the
// moment the operator types it, and resolving one would make the destination
// depend on a name server the router itself may be serving.
func ValidateDeviceURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > 512 || trimmed != raw {
		return nil, ErrNotADeviceDestination
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.User != nil || parsed.Opaque != "" || parsed.Fragment != "" || parsed.RawQuery != "" {
		return nil, ErrNotADeviceDestination
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, ErrNotADeviceDestination
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, ErrNotADeviceDestination
	}
	address, err := netip.ParseAddr(parsed.Hostname())
	if err != nil || !DeviceDestination(address) {
		return nil, ErrNotADeviceDestination
	}
	return parsed, nil
}
