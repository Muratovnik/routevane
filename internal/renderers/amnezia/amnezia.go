// Package amnezia renders the split-tunnelling site list the AmneziaVPN client
// imports. It owns format only: target limits and coverage decisions are
// supplied by the target profile and planner.
//
// The reason this adapter exists is the consumer, not the capability set: an
// operator running that client cannot import a router script or a sing-box
// rule-set, and this is the document its own importer reads.
//
// The schema is the one the client implements. The document is a JSON array —
// the importer refuses anything else — and each element carries a "hostname"
// plus an "ips" array. The client routes an entry whose hostname is an address
// or CIDR directly, and resolves an entry whose hostname is a name when the
// tunnel comes up, adding the answers as routes. A name therefore needs no
// address here, and a suffix cannot be expressed at all because the client
// resolves the one name it was given.
//
// Addresses are IPv4 only: the client collects IPv4 answers when it resolves a
// site, so an IPv6 entry would be carried and never routed.
package amnezia

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

const (
	ID              = "amnezia-split-tunnel-json"
	Version         = "amnezia-split-tunnel-json-v1"
	MaxEntries      = 4096
	MaxArtifactSize = 256 << 10
	ContentType     = "application/json"
	FileExtension   = "json"
)

type Renderer struct{}

func (Renderer) ID() string      { return ID }
func (Renderer) Version() string { return Version }
func (Renderer) Descriptor() domain.RendererDescriptor {
	return domain.RendererDescriptor{ID: ID, Version: Version, ContentType: ContentType, FileExtension: FileExtension}
}

// SupportedRuleKinds carries exact names and IPv4 addresses. IPv6 is absent
// because the client would store such an entry without ever routing it.
func (Renderer) SupportedRuleKinds() []domain.RuleKind {
	return []domain.RuleKind{domain.RuleDomainExact, domain.RuleIPv4, domain.RulePrefix4}
}

func (Renderer) ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	return ProjectedRuleCount(plan)
}
func (Renderer) Render(plan domain.RoutingPlan) ([]byte, error) { return Render(plan) }
func (Renderer) Validate(payload []byte) error                  { return Validate(payload) }

// Site is the independent validator's typed projection of one entry.
type Site struct {
	Hostname string
}

type wireSite struct {
	Hostname string   `json:"hostname"`
	IPs      []string `json:"ips"`
}

// Render projects already-decided rules into one canonical site list.
func Render(plan domain.RoutingPlan) ([]byte, error) {
	sites, err := projectPlan(plan)
	if err != nil {
		return nil, err
	}
	return renderSites(sites)
}

// ProjectedRuleCount counts the canonical entries the document will carry.
func ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	sites, err := projectPlan(plan)
	if err != nil {
		return 0, err
	}
	canonical, err := canonicalSites(sites)
	if err != nil {
		return 0, err
	}
	return len(canonical), nil
}

func projectPlan(plan domain.RoutingPlan) ([]Site, error) {
	sites := make([]Site, 0, len(plan.Rules))
	for _, route := range plan.Rules {
		if !route.IsValid() || route.Action != domain.ActionRoute {
			return nil, fmt.Errorf("routing plan contains an invalid route rule")
		}
		switch route.Kind {
		case domain.RuleDomainExact:
			sites = append(sites, Site{Hostname: route.CanonicalValue()})
		case domain.RuleIPv4:
			sites = append(sites, Site{Hostname: netip.PrefixFrom(route.Addr, 32).String()})
		case domain.RulePrefix4:
			sites = append(sites, Site{Hostname: route.Prefix.Masked().String()})
		default:
			return nil, fmt.Errorf("routing plan contains a rule kind this format cannot express")
		}
	}
	return sites, nil
}

// Parse decodes the document independently of Render and returns the typed
// projection. Canonical order and form are then proven by rendering the
// projection again and requiring byte equality.
func Parse(payload []byte) ([]Site, error) {
	if len(payload) == 0 || len(payload) > MaxArtifactSize {
		return nil, fmt.Errorf("split-tunnel list size is outside the supported bound")
	}
	if !bytes.HasSuffix(payload, []byte("\n")) {
		return nil, fmt.Errorf("split-tunnel list must end with one newline")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var wire []wireSite
	if err := decoder.Decode(&wire); err != nil {
		return nil, fmt.Errorf("split-tunnel list is not a valid document: %w", err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("split-tunnel list contains trailing content")
	}
	sites := make([]Site, 0, len(wire))
	for _, entry := range wire {
		// The client tolerates a stored address list, but an artifact that
		// carried one would freeze what this build observed instead of letting
		// the client resolve the name itself.
		if len(entry.IPs) != 0 {
			return nil, fmt.Errorf("split-tunnel entry %q must carry an empty address list", entry.Hostname)
		}
		sites = append(sites, Site{Hostname: entry.Hostname})
	}
	canonical, err := canonicalSites(sites)
	if err != nil {
		return nil, err
	}
	rendered, err := renderSites(canonical)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(rendered, payload) {
		return nil, fmt.Errorf("split-tunnel list is not in canonical order or form")
	}
	return canonical, nil
}

func Validate(payload []byte) error {
	_, err := Parse(payload)
	return err
}

func renderSites(input []Site) ([]byte, error) {
	sites, err := canonicalSites(input)
	if err != nil {
		return nil, err
	}
	document := make([]wireSite, 0, len(sites))
	for _, site := range sites {
		document = append(document, wireSite{Hostname: site.Hostname, IPs: []string{}})
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("encode split-tunnel list: %w", err)
	}
	if output.Len() > MaxArtifactSize {
		return nil, fmt.Errorf("split-tunnel list projection exceeds the byte bound")
	}
	return output.Bytes(), nil
}

// canonicalSites orders IPv4 prefixes before names and refuses anything the
// client would store without routing. An empty list is refused: it would replace
// the operator's site list with nothing.
func canonicalSites(input []Site) ([]Site, error) {
	prefixes := make([]netip.Prefix, 0, len(input))
	seenPrefix := make(map[string]struct{}, len(input))
	names := make([]string, 0, len(input))
	seenName := make(map[string]struct{}, len(input))
	for _, site := range input {
		value := site.Hostname
		if value == "" || strings.TrimSpace(value) != value {
			return nil, fmt.Errorf("split-tunnel list contains an empty or padded hostname %q", value)
		}
		if _, err := netip.ParseAddr(value); err == nil {
			return nil, fmt.Errorf("split-tunnel list contains the bare address %q instead of a prefix", value)
		}
		if strings.Contains(value, "/") {
			prefix, err := domain.ParsePrefix(value)
			if err != nil || prefix.String() != value {
				return nil, fmt.Errorf("split-tunnel list contains a non-canonical prefix %q", value)
			}
			if !prefix.Addr().Is4() {
				return nil, fmt.Errorf("split-tunnel list contains the IPv6 prefix %q the client would not route", value)
			}
			if _, duplicate := seenPrefix[value]; duplicate {
				continue
			}
			seenPrefix[value] = struct{}{}
			prefixes = append(prefixes, prefix)
			continue
		}
		normalized, err := domain.NormalizeDomain(value)
		if err != nil || normalized != value {
			return nil, fmt.Errorf("split-tunnel list contains a non-canonical hostname %q", value)
		}
		// The client refuses a name with no dot, so an artifact must not carry
		// one: the entry would be dropped at import without a message.
		if !strings.Contains(normalized, ".") {
			return nil, fmt.Errorf("split-tunnel list contains the single-label hostname %q the client refuses", value)
		}
		if _, duplicate := seenName[normalized]; duplicate {
			continue
		}
		seenName[normalized] = struct{}{}
		names = append(names, normalized)
	}
	total := len(prefixes) + len(names)
	if total == 0 {
		return nil, fmt.Errorf("split-tunnel list projection contains no entry")
	}
	if total > MaxEntries {
		return nil, fmt.Errorf("split-tunnel list projection exceeds the entry bound")
	}
	slices.SortFunc(prefixes, func(a, b netip.Prefix) int {
		return cmp.Or(a.Addr().Compare(b.Addr()), cmp.Compare(a.Bits(), b.Bits()))
	})
	slices.Sort(names)
	result := make([]Site, 0, total)
	for _, prefix := range prefixes {
		result = append(result, Site{Hostname: prefix.String()})
	}
	for _, name := range names {
		result = append(result, Site{Hostname: name})
	}
	return result, nil
}
