package domain

import (
	"fmt"
	"net/netip"
	"slices"
	"time"
)

type Action string

const ActionRoute Action = "route"

type RouteRule struct {
	Kind        RuleKind
	Domain      string
	Addr        netip.Addr
	Prefix      netip.Prefix
	Action      Action
	ServiceID   string
	ComponentID string
	ExpiresAt   *time.Time
	SourceClass SourceClass
	// Labels are stable human provenance labels assigned by the publication
	// boundary. They are part of the routing decision, but renderers may ignore
	// them when their file format has no native description field.
	Labels         []string
	ReasonCodes    []string
	ProvenanceRefs []string
}

func NewDomainRule(kind RuleKind, value, serviceID, componentID string, source SourceClass, reasons, provenance []string) (RouteRule, error) {
	if kind != RuleDomainExact && kind != RuleDomainSuffix {
		return RouteRule{}, fmt.Errorf("invalid domain rule kind %q", kind)
	}
	d, err := NormalizeDomain(value)
	if err != nil {
		return RouteRule{}, err
	}
	return RouteRule{Kind: kind, Domain: d, Action: ActionRoute, ServiceID: serviceID, ComponentID: componentID, SourceClass: source, ReasonCodes: append([]string(nil), reasons...), ProvenanceRefs: append([]string(nil), provenance...)}, nil
}

func NewAddrRule(a netip.Addr, serviceID, componentID string, source SourceClass, reasons, provenance []string) (RouteRule, error) {
	if !a.IsValid() {
		return RouteRule{}, ErrInvalidAddr
	}
	a = a.Unmap()
	kind := RuleIPv6
	if a.Is4() {
		kind = RuleIPv4
	}
	return RouteRule{Kind: kind, Addr: a, Action: ActionRoute, ServiceID: serviceID, ComponentID: componentID, SourceClass: source, ReasonCodes: append([]string(nil), reasons...), ProvenanceRefs: append([]string(nil), provenance...)}, nil
}

func NewPrefixRule(p netip.Prefix, serviceID, componentID string, source SourceClass, reasons, provenance []string) (RouteRule, error) {
	if !p.IsValid() {
		return RouteRule{}, ErrInvalidPrefix
	}
	if p.Addr().Is4In6() {
		bits := p.Bits()
		if bits < 96 {
			return RouteRule{}, ErrInvalidPrefix
		}
		p = netip.PrefixFrom(p.Addr().Unmap(), bits-96)
	}
	p = p.Masked()
	kind := RulePrefix6
	if p.Addr().Is4() {
		kind = RulePrefix4
	}
	return RouteRule{Kind: kind, Prefix: p, Action: ActionRoute, ServiceID: serviceID, ComponentID: componentID, SourceClass: source, ReasonCodes: append([]string(nil), reasons...), ProvenanceRefs: append([]string(nil), provenance...)}, nil
}

func (r RouteRule) CanonicalValue() string {
	switch r.Kind {
	case RuleDomainExact, RuleDomainSuffix:
		return NormalizeDomainValue(r.Domain)
	case RuleIPv4, RuleIPv6:
		return r.Addr.Unmap().String()
	case RulePrefix4, RulePrefix6:
		return r.Prefix.Masked().String()
	default:
		return ""
	}
}

func (r RouteRule) CandidateKey() string {
	return string(r.Kind) + "\x00" + r.CanonicalValue() + "\x00" + r.ServiceID + "\x00" + r.ComponentID
}

func (r RouteRule) IsValid() bool {
	if r.Action != ActionRoute || ValidateSlug(r.ServiceID) != nil || ValidateSlug(r.ComponentID) != nil {
		return false
	}
	switch r.Kind {
	case RuleDomainExact, RuleDomainSuffix:
		domain, err := NormalizeDomain(r.Domain)
		return err == nil && domain == r.Domain
	case RuleIPv4:
		return r.Addr.IsValid() && !r.Addr.Is4In6() && r.Addr.Is4()
	case RuleIPv6:
		return r.Addr.IsValid() && !r.Addr.Is4In6() && r.Addr.Is6()
	case RulePrefix4:
		return r.Prefix.IsValid() && !r.Prefix.Addr().Is4In6() && r.Prefix.Addr().Is4() && r.Prefix == r.Prefix.Masked()
	case RulePrefix6:
		return r.Prefix.IsValid() && !r.Prefix.Addr().Is4In6() && r.Prefix.Addr().Is6() && r.Prefix == r.Prefix.Masked()
	default:
		return false
	}
}

type Excluded struct {
	Candidate      RouteRule
	Outcome        string
	ReasonCodes    []string
	ProvenanceRefs []string
}

type Coverage struct {
	ServiceID   string
	ComponentID string
	Complete    bool
	RuleCount   int
}

// RoutingPlanInterfaceVersion is written into every published snapshot and
// artifact, so its value is stored data rather than a name. Changing it would
// change the bytes and the semantic hash of files that are already published
// and are required to stay identical. Rename the constant if you must; the
// string stays.
const RoutingPlanInterfaceVersion = "m0-spike-v1"

type RoutingPlan struct {
	InterfaceVersion  string
	TargetID          string
	ProfileKey        string
	Services          []string
	Rules             []RouteRule
	Excluded          []Excluded
	Warnings          []string
	Coverage          []Coverage
	Relations         []Relation
	Sightings         []Sighting
	PolicyVersion     string
	CatalogRevision   string
	ObservationCutoff time.Time
	SemanticHash      string
}

// StableStrings returns a sorted, deduplicated copy.  It is shared by the
// planner and renderer for reason/provenance and warning canonicalization.
func StableStrings(values []string) []string {
	// The copy is empty rather than nil for an empty input: these values are
	// encoded as JSON arrays and hashed as such, and null is another document.
	out := make([]string, len(values))
	copy(out, values)
	slices.Sort(out)
	return slices.Compact(out)
}
