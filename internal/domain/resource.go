// Package domain contains the small, format-independent routing model. Values
// cross the package boundary only after they have been normalized; the planner
// and renderers can therefore reason about netip values instead of unchecked
// strings.
package domain

import (
	"errors"
	"net/netip"
	"strings"
)

var (
	ErrInvalidDomain = errors.New("invalid domain")
	ErrInvalidAddr   = errors.New("invalid IP address")
	ErrInvalidPrefix = errors.New("invalid IP prefix")
	ErrInvalidSlug   = errors.New("invalid lowercase slug")
)

// ValidateSlug accepts the deliberately narrow identifier grammar used by
// catalog, service, component, and source identities. Keeping the grammar
// smaller than a filesystem name prevents identifiers from becoming paths.
func ValidateSlug(value string) error {
	if value == "" || len(value) > 63 || value[0] < 'a' || value[0] > 'z' || value[len(value)-1] == '-' {
		return ErrInvalidSlug
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return ErrInvalidSlug
		}
	}
	return nil
}

// NormalizeDomain accepts a deliberately narrow ASCII domain grammar. One
// terminal dot is presentation syntax and is removed; an additional terminal
// dot remains an empty label and is rejected.
func NormalizeDomain(raw string) (string, error) {
	if raw == "" {
		return "", ErrInvalidDomain
	}
	raw = strings.TrimSuffix(raw, ".")
	if raw == "" || len(raw) > 253 {
		return "", ErrInvalidDomain
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c > 0x7f || c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\v' || c == '\f' {
			return "", ErrInvalidDomain
		}
	}
	labels := strings.Split(raw, ".")
	if len(labels) == 0 {
		return "", ErrInvalidDomain
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalidDomain
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') || c == '-') {
				return "", ErrInvalidDomain
			}
		}
	}
	return strings.ToLower(raw), nil
}

// ParseAddr validates an address at a string boundary and normalizes IPv4
// mapped IPv6 values to their IPv4 representation.
func ParseAddr(raw string) (netip.Addr, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.Contains(raw, "%") {
		return netip.Addr{}, ErrInvalidAddr
	}
	a, err := netip.ParseAddr(raw)
	if err != nil || !a.IsValid() {
		return netip.Addr{}, ErrInvalidAddr
	}
	return a.Unmap(), nil
}

// ParsePrefix validates and canonicalizes a prefix.  IPv4-mapped IPv6
// prefixes are converted to their IPv4 width (for example /120 becomes /24).
func ParsePrefix(raw string) (netip.Prefix, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.Contains(raw, "%") {
		return netip.Prefix{}, ErrInvalidPrefix
	}
	p, err := netip.ParsePrefix(raw)
	if err != nil || !p.IsValid() {
		return netip.Prefix{}, ErrInvalidPrefix
	}
	a := p.Addr()
	bits := p.Bits()
	if a.Is4In6() {
		if bits < 96 {
			return netip.Prefix{}, ErrInvalidPrefix
		}
		a = a.Unmap()
		bits -= 96
	}
	if !a.IsValid() || bits < 0 || bits > a.BitLen() {
		return netip.Prefix{}, ErrInvalidPrefix
	}
	return netip.PrefixFrom(a.Unmap(), bits).Masked(), nil
}

type ResourceKind string

const (
	ResourceDomain ResourceKind = "domain"
	ResourceIP     ResourceKind = "ip"
	ResourcePrefix ResourceKind = "prefix"
)

// Resource is a tagged union.  Only the field corresponding to Kind is
// meaningful.  Constructors below are preferred so malformed values cannot
// enter the planner.
type Resource struct {
	Kind   ResourceKind
	Domain string
	Addr   netip.Addr
	Prefix netip.Prefix
}

func NewDomainResource(raw string) (Resource, error) {
	d, err := NormalizeDomain(raw)
	if err != nil {
		return Resource{}, err
	}
	return Resource{Kind: ResourceDomain, Domain: d}, nil
}

func NewAddrResource(a netip.Addr) (Resource, error) {
	if !a.IsValid() {
		return Resource{}, ErrInvalidAddr
	}
	a = a.Unmap()
	return Resource{Kind: ResourceIP, Addr: a}, nil
}

func NewAddrResourceFromString(raw string) (Resource, error) {
	a, err := ParseAddr(raw)
	if err != nil {
		return Resource{}, err
	}
	return NewAddrResource(a)
}

func NewPrefixResource(p netip.Prefix) (Resource, error) {
	if !p.IsValid() {
		return Resource{}, ErrInvalidPrefix
	}
	if p.Addr().Is4In6() {
		bits := p.Bits()
		if bits < 96 {
			return Resource{}, ErrInvalidPrefix
		}
		p = netip.PrefixFrom(p.Addr().Unmap(), bits-96)
	}
	return Resource{Kind: ResourcePrefix, Prefix: p.Masked()}, nil
}

func NewPrefixResourceFromString(raw string) (Resource, error) {
	p, err := ParsePrefix(raw)
	if err != nil {
		return Resource{}, err
	}
	return NewPrefixResource(p)
}

func (r Resource) IsValid() bool {
	switch r.Kind {
	case ResourceDomain:
		domain, err := NormalizeDomain(r.Domain)
		return err == nil && domain == r.Domain
	case ResourceIP:
		return r.Addr.IsValid() && !r.Addr.Is4In6()
	case ResourcePrefix:
		return r.Prefix.IsValid() && !r.Prefix.Addr().Is4In6()
	default:
		return false
	}
}

// NormalizeDomainValue is intentionally non-erroring for use in canonical
// sorting and validation.  Call NormalizeDomain for untrusted strings.
func NormalizeDomainValue(value string) string {
	return strings.ToLower(strings.TrimSuffix(value, "."))
}

func (r Resource) CanonicalValue() string {
	switch r.Kind {
	case ResourceDomain:
		return NormalizeDomainValue(r.Domain)
	case ResourceIP:
		return r.Addr.Unmap().String()
	case ResourcePrefix:
		return r.Prefix.Masked().String()
	default:
		return ""
	}
}

func (r Resource) String() string { return r.CanonicalValue() }

func (k ResourceKind) String() string { return string(k) }

type RuleKind string

const (
	RuleDomainExact  RuleKind = "domain_exact"
	RuleDomainSuffix RuleKind = "domain_suffix"
	RuleIPv4         RuleKind = "ipv4"
	RuleIPv6         RuleKind = "ipv6"
	RulePrefix4      RuleKind = "prefix4"
	RulePrefix6      RuleKind = "prefix6"
)

func (k RuleKind) IsDomain() bool { return k == RuleDomainExact || k == RuleDomainSuffix }
func (k RuleKind) IsIP() bool     { return k == RuleIPv4 || k == RuleIPv6 }
func (k RuleKind) IsPrefix() bool { return k == RulePrefix4 || k == RulePrefix6 }

// NormalizeRuleValue accepts the three shapes an operator states a destination
// in — an IP address, a network, a domain — and answers with the canonical
// string plus the seed kind that value plants. Addresses are tried before
// domains because a dotted-quad like "1.2.3.4" is also a syntactically valid
// domain name.
func NormalizeRuleValue(raw string) (string, RuleKind, error) {
	if a, err := ParseAddr(raw); err == nil {
		kind := RuleIPv6
		if a.Is4() {
			kind = RuleIPv4
		}
		return a.String(), kind, nil
	}
	if p, err := ParsePrefix(raw); err == nil {
		kind := RulePrefix6
		if p.Addr().Is4() {
			kind = RulePrefix4
		}
		return p.String(), kind, nil
	}
	if d, err := NormalizeDomain(raw); err == nil {
		return d, RuleDomainSuffix, nil
	}
	return "", "", ErrInvalidDomain
}
