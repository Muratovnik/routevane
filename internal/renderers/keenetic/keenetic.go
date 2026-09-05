// Package keenetic renders the bounded IPv4 route ADD BAT dialect accepted by
// KeeneticOS 5.0.4+ static-route upload. It owns format only: target limits and
// coverage decisions are supplied by the target profile and planner.
package keenetic

import (
	"bytes"
	"cmp"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

const (
	ID              = "keenetic-route-bat"
	Version         = "keenetic-bat-ipv4-v1"
	MaxLines        = 1024
	MaxArtifactSize = 128 << 10
	ContentType     = "application/x-bat"
	FileExtension   = "bat"
	maxLineBytes    = 96
)

type Renderer struct{}

func (Renderer) ID() string      { return ID }
func (Renderer) Version() string { return Version }
func (Renderer) Descriptor() domain.RendererDescriptor {
	return domain.RendererDescriptor{ID: ID, Version: Version, ContentType: ContentType, FileExtension: FileExtension}
}
func (Renderer) SupportedRuleKinds() []domain.RuleKind {
	return []domain.RuleKind{domain.RuleIPv4, domain.RulePrefix4}
}
func (Renderer) ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	return ProjectedRuleCount(plan)
}
func (Renderer) Render(plan domain.RoutingPlan) ([]byte, error) { return Render(plan) }
func (Renderer) Validate(payload []byte) error                  { return Validate(payload) }

// Rule is the independent validator's typed projection of one BAT line.
type Rule struct {
	Prefix netip.Prefix
}

// Render projects already-decided IPv4 rules into deterministic BAT lines.
// Format-identical rules are deduplicated because one route is sufficient and
// the validator deliberately rejects duplicate commands.
func Render(plan domain.RoutingPlan) ([]byte, error) {
	rules, err := projectPlan(plan)
	if err != nil {
		return nil, err
	}
	return renderRules(rules)
}

// ProjectedRuleCount returns the number of canonical BAT lines without
// discarding policy rules or their provenance from the RoutingPlan.
func ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	rules, err := projectPlan(plan)
	if err != nil {
		return 0, err
	}
	canonical, err := projectedRules(rules)
	if err != nil {
		return 0, err
	}
	return len(canonical), nil
}

func projectPlan(plan domain.RoutingPlan) ([]Rule, error) {
	rules := make([]Rule, 0, len(plan.Rules))
	for _, route := range plan.Rules {
		if !route.IsValid() || route.Action != domain.ActionRoute {
			return nil, fmt.Errorf("routing plan contains an invalid route rule")
		}
		var prefix netip.Prefix
		switch route.Kind {
		case domain.RuleIPv4:
			prefix = netip.PrefixFrom(route.Addr, 32)
		case domain.RulePrefix4:
			prefix = route.Prefix.Masked()
		default:
			return nil, fmt.Errorf("routing plan contains an unsupported rule kind")
		}
		if !prefix.IsValid() || !prefix.Addr().Is4() || prefix.Bits() <= 0 {
			return nil, fmt.Errorf("routing plan contains an unsupported IPv4 prefix")
		}
		rules = append(rules, Rule{Prefix: prefix})
	}
	return rules, nil
}

// Parse validates the lexical BAT dialect independently and returns typed
// prefixes. Canonical ordering is checked by rendering that projection again.
func Parse(payload []byte) ([]Rule, error) {
	if len(payload) == 0 || len(payload) > MaxArtifactSize {
		return nil, fmt.Errorf("keenetic BAT size is outside the supported bound")
	}
	if len(payload) < 2 || !bytes.HasSuffix(payload, []byte("\r\n")) {
		return nil, fmt.Errorf("keenetic BAT must end with CRLF")
	}
	for i, value := range payload {
		switch {
		case value >= 0x80:
			return nil, fmt.Errorf("keenetic BAT must be ASCII without a BOM")
		case value == '\r':
			if i+1 >= len(payload) || payload[i+1] != '\n' {
				return nil, fmt.Errorf("keenetic BAT contains a bare carriage return")
			}
		case value == '\n':
			if i == 0 || payload[i-1] != '\r' {
				return nil, fmt.Errorf("keenetic BAT contains a bare line feed")
			}
		case value < 0x20 || value == 0x7f:
			return nil, fmt.Errorf("keenetic BAT contains a control byte")
		}
	}
	body := payload[:len(payload)-2]
	if len(body) == 0 {
		return nil, fmt.Errorf("keenetic BAT contains no routes")
	}
	lines := bytes.Split(body, []byte("\r\n"))
	if len(lines) > MaxLines {
		return nil, fmt.Errorf("keenetic BAT exceeds the line bound")
	}
	rules := make([]Rule, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, rawLine := range lines {
		if len(rawLine) == 0 || len(rawLine) > maxLineBytes {
			return nil, fmt.Errorf("keenetic BAT contains an invalid line")
		}
		parts := strings.Split(string(rawLine), " ")
		if len(parts) != 6 || parts[0] != "route" || parts[1] != "ADD" || parts[3] != "MASK" || parts[5] != "0.0.0.0" {
			return nil, fmt.Errorf("keenetic BAT contains an unknown command")
		}
		destination, err := netip.ParseAddr(parts[2])
		if err != nil || !destination.Is4() || destination.String() != parts[2] {
			return nil, fmt.Errorf("keenetic BAT contains an invalid IPv4 destination")
		}
		maskAddress, err := netip.ParseAddr(parts[4])
		if err != nil || !maskAddress.Is4() || maskAddress.String() != parts[4] {
			return nil, fmt.Errorf("keenetic BAT contains an invalid IPv4 mask")
		}
		maskBytes := maskAddress.As4()
		ones, bits := net.IPMask(maskBytes[:]).Size()
		if bits != 32 || ones <= 0 {
			return nil, fmt.Errorf("keenetic BAT mask is not a non-default contiguous mask")
		}
		prefix := netip.PrefixFrom(destination, ones).Masked()
		if prefix.Addr() != destination {
			return nil, fmt.Errorf("keenetic BAT destination is not masked")
		}
		key := prefix.String()
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("keenetic BAT contains a duplicate route")
		}
		seen[key] = struct{}{}
		rules = append(rules, Rule{Prefix: prefix})
	}
	canonical, err := renderRules(rules)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, payload) {
		return nil, fmt.Errorf("keenetic BAT is not in canonical order or form")
	}
	return rules, nil
}

func Validate(payload []byte) error {
	_, err := Parse(payload)
	return err
}

func renderRules(input []Rule) ([]byte, error) {
	rules, err := canonicalRules(input)
	if err != nil {
		return nil, err
	}
	if len(rules) > MaxLines {
		return nil, fmt.Errorf("keenetic BAT projection exceeds the line bound")
	}
	var output bytes.Buffer
	for _, rule := range rules {
		mask := net.CIDRMask(rule.Prefix.Bits(), 32)
		line := fmt.Sprintf("route ADD %s MASK %d.%d.%d.%d 0.0.0.0\r\n", rule.Prefix.Addr(), mask[0], mask[1], mask[2], mask[3])
		if len(line) > maxLineBytes {
			return nil, fmt.Errorf("keenetic BAT projection line exceeds the bound")
		}
		output.WriteString(line)
	}
	if output.Len() > MaxArtifactSize {
		return nil, fmt.Errorf("keenetic BAT projection exceeds the byte bound")
	}
	return output.Bytes(), nil
}

// Artifact validation owns empty/size refusal; counting must be able to
// report both zero and an overflow without pretending either is unavailable.
func canonicalRules(input []Rule) ([]Rule, error) {
	result, err := projectedRules(input)
	if err != nil {
		return nil, err
	}
	total := len(result)
	if total == 0 {
		return nil, fmt.Errorf("projection contains no entries")
	}
	return result, nil
}

func projectedRules(input []Rule) ([]Rule, error) {
	seen := make(map[string]Rule, len(input))
	for _, rule := range input {
		prefix := rule.Prefix.Masked()
		if !prefix.IsValid() || !prefix.Addr().Is4() || prefix.Bits() <= 0 {
			return nil, fmt.Errorf("keenetic BAT projection contains an invalid prefix")
		}
		seen[prefix.String()] = Rule{Prefix: prefix}
	}
	rules := make([]Rule, 0, len(seen))
	for _, rule := range seen {
		rules = append(rules, rule)
	}
	slices.SortFunc(rules, func(a, b Rule) int {
		return cmp.Or(a.Prefix.Addr().Compare(b.Prefix.Addr()), cmp.Compare(a.Prefix.Bits(), b.Prefix.Bits()))
	})
	return rules, nil
}
