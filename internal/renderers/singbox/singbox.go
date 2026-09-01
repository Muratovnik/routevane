// Package singbox renders the sing-box source rule-set JSON document. It owns
// format only: target limits and policy decisions are supplied by the target
// profile and the planner. Unlike the Keenetic dialect this format is a
// declarative match set rather than a list of imperative route commands, and it
// carries domains and both address families instead of IPv4 routes alone.
package singbox

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

const (
	ID      = "singbox-ruleset-json"
	Version = "singbox-source-json-v1"
	// FormatVersion is the sing-box source rule-set schema version this
	// renderer emits and the only version its validator accepts.
	FormatVersion   = 3
	MaxEntries      = 8192
	MaxArtifactSize = 1 << 20
	ContentType     = "application/json"
	FileExtension   = "json"
)

type Renderer struct{}

func (Renderer) ID() string      { return ID }
func (Renderer) Version() string { return Version }
func (Renderer) Descriptor() domain.RendererDescriptor {
	return domain.RendererDescriptor{ID: ID, Version: Version, ContentType: ContentType, FileExtension: FileExtension}
}

// SupportedRuleKinds is deliberately wider than the Keenetic dialect: the same
// plan therefore reaches this format with domain rules the router format has to
// drop.
func (Renderer) SupportedRuleKinds() []domain.RuleKind {
	return []domain.RuleKind{
		domain.RuleDomainExact, domain.RuleDomainSuffix,
		domain.RuleIPv4, domain.RuleIPv6,
		domain.RulePrefix4, domain.RulePrefix6,
	}
}

func (Renderer) ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	return ProjectedRuleCount(plan)
}
func (Renderer) Render(plan domain.RoutingPlan) ([]byte, error) { return Render(plan) }
func (Renderer) Validate(payload []byte) error                  { return Validate(payload) }

// RuleSet is the independent validator's typed projection of the document.
type RuleSet struct {
	Version int
	Rule    Rule
}

// Rule is one sing-box headless rule. Only the match fields this renderer emits
// are modelled; an unknown field makes a document invalid rather than ignored.
type Rule struct {
	Domain       []string
	DomainSuffix []string
	IPCIDR       []string
}

type wireRuleSet struct {
	Version int        `json:"version"`
	Rules   []wireRule `json:"rules"`
}

type wireRule struct {
	Domain       []string `json:"domain,omitempty"`
	DomainSuffix []string `json:"domain_suffix,omitempty"`
	IPCIDR       []string `json:"ip_cidr,omitempty"`
}

// Render projects already-decided rules into one canonical rule-set document.
func Render(plan domain.RoutingPlan) ([]byte, error) {
	rule, err := projectPlan(plan)
	if err != nil {
		return nil, err
	}
	return renderRule(rule)
}

// ProjectedRuleCount counts the canonical match entries the document will carry.
// It is the number a target rule limit is compared against, so it must not
// count a duplicate twice.
func ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	rule, err := projectPlan(plan)
	if err != nil {
		return 0, err
	}
	canonical, err := canonicalRule(rule)
	if err != nil {
		return 0, err
	}
	return len(canonical.Domain) + len(canonical.DomainSuffix) + len(canonical.IPCIDR), nil
}

func projectPlan(plan domain.RoutingPlan) (Rule, error) {
	rule := Rule{}
	for _, route := range plan.Rules {
		if !route.IsValid() || route.Action != domain.ActionRoute {
			return Rule{}, fmt.Errorf("routing plan contains an invalid route rule")
		}
		switch route.Kind {
		case domain.RuleDomainExact:
			rule.Domain = append(rule.Domain, route.CanonicalValue())
		case domain.RuleDomainSuffix:
			rule.DomainSuffix = append(rule.DomainSuffix, route.CanonicalValue())
		case domain.RuleIPv4:
			rule.IPCIDR = append(rule.IPCIDR, netip.PrefixFrom(route.Addr, 32).String())
		case domain.RuleIPv6:
			rule.IPCIDR = append(rule.IPCIDR, netip.PrefixFrom(route.Addr, 128).String())
		case domain.RulePrefix4, domain.RulePrefix6:
			rule.IPCIDR = append(rule.IPCIDR, route.Prefix.Masked().String())
		default:
			return Rule{}, fmt.Errorf("routing plan contains an unsupported rule kind")
		}
	}
	return rule, nil
}

// Parse validates the document independently of Render and returns the typed
// projection. Canonical ordering and form are checked by rendering the
// projection again, exactly as the Keenetic validator does.
func Parse(payload []byte) (RuleSet, error) {
	if len(payload) == 0 || len(payload) > MaxArtifactSize {
		return RuleSet{}, fmt.Errorf("sing-box rule set size is outside the supported bound")
	}
	if !bytes.HasSuffix(payload, []byte("\n")) {
		return RuleSet{}, fmt.Errorf("sing-box rule set must end with one newline")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var wire wireRuleSet
	if err := decoder.Decode(&wire); err != nil {
		return RuleSet{}, fmt.Errorf("sing-box rule set is not a valid document: %w", err)
	}
	if decoder.More() {
		return RuleSet{}, fmt.Errorf("sing-box rule set contains trailing content")
	}
	if wire.Version != FormatVersion {
		return RuleSet{}, fmt.Errorf("sing-box rule set version %d is not supported", wire.Version)
	}
	if len(wire.Rules) != 1 {
		return RuleSet{}, fmt.Errorf("sing-box rule set must contain exactly one headless rule")
	}
	rule := Rule{Domain: wire.Rules[0].Domain, DomainSuffix: wire.Rules[0].DomainSuffix, IPCIDR: wire.Rules[0].IPCIDR}
	canonical, err := canonicalRule(rule)
	if err != nil {
		return RuleSet{}, err
	}
	rendered, err := renderRule(canonical)
	if err != nil {
		return RuleSet{}, err
	}
	if !bytes.Equal(rendered, payload) {
		return RuleSet{}, fmt.Errorf("sing-box rule set is not in canonical order or form")
	}
	return RuleSet{Version: wire.Version, Rule: canonical}, nil
}

func Validate(payload []byte) error {
	_, err := Parse(payload)
	return err
}

func renderRule(input Rule) ([]byte, error) {
	rule, err := canonicalRule(input)
	if err != nil {
		return nil, err
	}
	document := wireRuleSet{Version: FormatVersion, Rules: []wireRule{{Domain: rule.Domain, DomainSuffix: rule.DomainSuffix, IPCIDR: rule.IPCIDR}}}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("encode sing-box rule set: %w", err)
	}
	if output.Len() > MaxArtifactSize {
		return nil, fmt.Errorf("sing-box rule set projection exceeds the byte bound")
	}
	return output.Bytes(), nil
}

// canonicalRule sorts and deduplicates every match list and re-validates each
// entry. A document with no entry at all is refused: an empty match set would
// silently match nothing rather than the requested services.
func canonicalRule(input Rule) (Rule, error) {
	domains, err := canonicalDomains(input.Domain)
	if err != nil {
		return Rule{}, err
	}
	suffixes, err := canonicalDomains(input.DomainSuffix)
	if err != nil {
		return Rule{}, err
	}
	prefixes, err := canonicalPrefixes(input.IPCIDR)
	if err != nil {
		return Rule{}, err
	}
	total := len(domains) + len(suffixes) + len(prefixes)
	if total == 0 {
		return Rule{}, fmt.Errorf("sing-box rule set projection contains no match entry")
	}
	if total > MaxEntries {
		return Rule{}, fmt.Errorf("sing-box rule set projection exceeds the entry bound")
	}
	return Rule{Domain: domains, DomainSuffix: suffixes, IPCIDR: prefixes}, nil
}

func canonicalDomains(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized, err := domain.NormalizeDomain(value)
		if err != nil || normalized != value {
			return nil, fmt.Errorf("sing-box rule set contains a non-canonical domain %q", value)
		}
		seen[normalized] = struct{}{}
	}
	result := slices.Sorted(maps.Keys(seen))
	return result, nil
}

func canonicalPrefixes(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := make(map[string]netip.Prefix, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != value {
			return nil, fmt.Errorf("sing-box rule set contains a padded prefix %q", value)
		}
		prefix, err := domain.ParsePrefix(value)
		if err != nil || prefix.String() != value {
			return nil, fmt.Errorf("sing-box rule set contains a non-canonical prefix %q", value)
		}
		seen[prefix.String()] = prefix
	}
	ordered := make([]netip.Prefix, 0, len(seen))
	for _, prefix := range seen {
		ordered = append(ordered, prefix)
	}
	// Addr.Compare orders by address length first, so IPv4 already precedes
	// IPv6 and the family needs no key of its own.
	slices.SortFunc(ordered, func(a, b netip.Prefix) int {
		return cmp.Or(a.Addr().Compare(b.Addr()), cmp.Compare(a.Bits(), b.Bits()))
	})
	result := make([]string, 0, len(ordered))
	for _, prefix := range ordered {
		result = append(result, prefix.String())
	}
	return result, nil
}
