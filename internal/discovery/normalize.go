// Package discovery turns one user-supplied URL into a safe local service
// draft. Every decision here is deterministic and structural: a domain is
// accepted because of its relationship to the entered site, never because of a
// vendor list, an ASN, an RDAP record, or a certificate.
package discovery

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"golang.org/x/net/publicsuffix"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/netpolicy"
)

var (
	ErrInvalidTarget     = errors.New("discovery target is not a usable web address")
	ErrUnsafeDestination = netpolicy.ErrUnsafeDestination
	// ErrNoRegistrableDomain reports a host that has no registrable domain under
	// the Public Suffix List, such as a bare public suffix or an address literal.
	ErrNoRegistrableDomain = errors.New("discovery target has no registrable domain")
)

// MaxTargetLength bounds the user-supplied value before it is parsed.
const MaxTargetLength = 2048

// Target is the normalized starting point of a discovery session. It is the
// exact value shown to the user before a browser is launched.
type Target struct {
	// URL is the canonical absolute HTTPS URL that will be loaded.
	URL string
	// Host is the normalized hostname of that URL.
	Host string
	// RegistrableDomain is the Public Suffix List registrable domain. It is the
	// only value a draft may turn into a routing seed.
	RegistrableDomain string
	// PublicSuffix is the suffix the registrable domain sits under.
	PublicSuffix string
	// ICANNSuffix reports whether the suffix comes from the ICANN section of the
	// list rather than the private section.
	ICANNSuffix bool
}

// NormalizeTarget accepts what a user actually types — a bare host, a host with
// a path, or a full URL — and returns the canonical HTTPS URL to load.
//
// The registrable domain comes from the Public Suffix List, not from a manual
// list of zones, so a multi-label suffix such as co.uk is handled by the same
// code path as com.
func NormalizeTarget(raw string) (Target, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > MaxTargetLength || containsControl(trimmed) {
		return Target{}, ErrInvalidTarget
	}
	if !strings.Contains(trimmed, "://") {
		// A bare host or host/path is the common case; assume HTTPS rather than
		// silently downgrading to plaintext.
		trimmed = "https://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Opaque != "" || parsed.User != nil {
		return Target{}, ErrInvalidTarget
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return Target{}, ErrInvalidTarget
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || netpolicy.LocalHostname(host) {
		return Target{}, ErrInvalidTarget
	}
	if address, addrErr := netip.ParseAddr(host); addrErr == nil {
		// An address literal has no registrable domain, so it can never produce
		// a domain-based draft. Report the reason precisely.
		if !netpolicy.PublicUnicast(address) {
			return Target{}, ErrUnsafeDestination
		}
		return Target{}, ErrNoRegistrableDomain
	}
	normalizedHost, err := domain.NormalizeDomain(host)
	if err != nil {
		return Target{}, ErrInvalidTarget
	}
	registrable, err := publicsuffix.EffectiveTLDPlusOne(normalizedHost)
	if err != nil {
		return Target{}, ErrNoRegistrableDomain
	}
	registrable, err = domain.NormalizeDomain(registrable)
	if err != nil {
		return Target{}, ErrNoRegistrableDomain
	}
	suffix, icann := publicsuffix.PublicSuffix(normalizedHost)
	if suffix == "" || suffix == normalizedHost {
		return Target{}, ErrNoRegistrableDomain
	}
	canonical := &url.URL{Scheme: "https", Host: normalizedHost, Path: parsed.Path, RawQuery: parsed.RawQuery}
	if canonical.Path == "" {
		canonical.Path = "/"
	}
	if parsed.Port() != "" {
		// A non-default port is allowed but must be explicit in the value the
		// user confirms, so it is never hidden by canonicalization.
		canonical.Host = normalizedHost + ":" + parsed.Port()
	}
	return Target{
		URL:               canonical.String(),
		Host:              normalizedHost,
		RegistrableDomain: registrable,
		PublicSuffix:      suffix,
		ICANNSuffix:       icann,
	}, nil
}

// SameSite reports whether an observed host belongs to the same registrable
// domain as the target. This relationship, and nothing else, is what allows a
// host to reach the draft.
func SameSite(registrableDomain, host string) bool {
	normalizedHost, err := domain.NormalizeDomain(strings.ToLower(host))
	if err != nil {
		return false
	}
	observed, err := publicsuffix.EffectiveTLDPlusOne(normalizedHost)
	if err != nil {
		return false
	}
	return observed == registrableDomain
}

// RegistrableDomainOf returns the registrable domain of an observed host.
func RegistrableDomainOf(host string) (string, error) {
	normalizedHost, err := domain.NormalizeDomain(strings.ToLower(strings.TrimSpace(host)))
	if err != nil {
		return "", ErrInvalidTarget
	}
	if _, addrErr := netip.ParseAddr(normalizedHost); addrErr == nil {
		return "", ErrNoRegistrableDomain
	}
	registrable, err := publicsuffix.EffectiveTLDPlusOne(normalizedHost)
	if err != nil {
		return "", ErrNoRegistrableDomain
	}
	return registrable, nil
}

// ServiceIDFor derives the default local service identity from a registrable
// domain. The result is validated against the catalog slug grammar, so it can
// never become a path.
func ServiceIDFor(registrableDomain string) (string, error) {
	label := registrableDomain
	if index := strings.Index(label, "."); index > 0 {
		label = label[:index]
	}
	// The label is validated, never rewritten: silently trimming a hostile
	// identity into a valid one would hide what the user actually entered.
	if err := domain.ValidateSlug(label); err != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidTarget, registrableDomain)
	}
	return label, nil
}

func containsControl(value string) bool {
	return strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f })
}
