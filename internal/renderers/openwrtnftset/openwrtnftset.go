// Package openwrtnftset renders the dnsmasq configuration fragment OpenWrt uses
// to populate an nftables set from resolved answers. It owns format only: target
// limits and coverage decisions are supplied by the target definition and planner.
//
// This format is the product's first dynamic set: the artifact carries no
// address at all. dnsmasq resolves each listed domain on the device and adds the
// answers to a firewall set, so the routing decision follows the list as its
// addresses change instead of freezing the addresses observed here.
//
// The consequence is that an exact domain cannot be expressed. The nftset option
// matches a domain and every name under it, exactly as the address option does,
// so a renderer that accepted an exact rule would silently widen it. Such a plan
// is refused rather than approximated.
//
// The option syntax is the documented one:
//
//	--nftset=/<domain>[/<domain>...]/[(6|4)#[<family>#]<table>#<set>[,...]
//
// One line carries one domain and both address families, which keeps the
// document canonical without repeating a domain.
package openwrtnftset

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

const (
	ID      = "openwrt-nftset-conf"
	Version = "openwrt-nftset-conf-v1"
	// Family and Table are the fw4 identities the OpenWrt firewall owns. They
	// are fixed rather than configurable: a set in another table would not be
	// matched by the rules the installation hint tells the operator to add.
	Family = "inet"
	Table  = "fw4"
	// SetIPv4 and SetIPv6 are the sets the operator creates once. Two sets are
	// needed because an nftables set holds one address type.
	SetIPv4 = "routevane4"
	SetIPv6 = "routevane6"
	// Header identifies the fragment on a device whose dnsmasq.d directory holds
	// files from several sources. The validator requires exactly this line.
	Header          = "# routevane " + Version
	MaxLines        = 4096
	MaxArtifactSize = 256 << 10
	ContentType     = "text/plain; charset=utf-8"
	FileExtension   = "conf"
	// maxLineBytes bounds one directive. dnsmasq itself accepts more, but a
	// domain long enough to approach this is not a routing target.
	maxLineBytes = 320
)

type Renderer struct{}

func (Renderer) ID() string      { return ID }
func (Renderer) Version() string { return Version }
func (Renderer) Descriptor() domain.RendererDescriptor {
	return domain.RendererDescriptor{ID: ID, Version: Version, ContentType: ContentType, FileExtension: FileExtension}
}

// SupportedRuleKinds is narrower than every other built-in format: this
// document expresses suffix matching and nothing else. An address rule belongs
// to a format that can carry an address.
func (Renderer) SupportedRuleKinds() []domain.RuleKind {
	return []domain.RuleKind{domain.RuleDomainSuffix}
}

func (Renderer) ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	return ProjectedRuleCount(plan)
}
func (Renderer) Render(plan domain.RoutingPlan) ([]byte, error) { return Render(plan) }
func (Renderer) Validate(payload []byte) error                  { return Validate(payload) }

// Rule is the independent validator's typed projection of one directive.
type Rule struct {
	Suffix string
}

// Render projects already-decided suffix rules into deterministic directives.
func Render(plan domain.RoutingPlan) ([]byte, error) {
	rules, err := projectPlan(plan)
	if err != nil {
		return nil, err
	}
	return renderRules(rules)
}

// ProjectedRuleCount counts canonical directives, so a plan carrying the same
// suffix twice is compared against the target limit once.
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
		if route.Kind != domain.RuleDomainSuffix {
			return nil, fmt.Errorf("routing plan contains a rule kind this format cannot express")
		}
		rules = append(rules, Rule{Suffix: route.CanonicalValue()})
	}
	return rules, nil
}

// Parse decodes the fragment independently of Render and returns the typed
// projection. Canonical order and form are then proven by rendering the
// projection again and requiring byte equality.
func Parse(payload []byte) ([]Rule, error) {
	if len(payload) == 0 || len(payload) > MaxArtifactSize {
		return nil, fmt.Errorf("dnsmasq fragment size is outside the supported bound")
	}
	if !bytes.HasSuffix(payload, []byte("\n")) {
		return nil, fmt.Errorf("dnsmasq fragment must end with one newline")
	}
	if bytes.ContainsRune(payload, '\r') {
		return nil, fmt.Errorf("dnsmasq fragment must use LF line endings")
	}
	lines := strings.Split(strings.TrimSuffix(string(payload), "\n"), "\n")
	if len(lines) == 0 || lines[0] != Header {
		return nil, fmt.Errorf("dnsmasq fragment must begin with the routevane header line")
	}
	directives := lines[1:]
	if len(directives) == 0 {
		return nil, fmt.Errorf("dnsmasq fragment contains no directive")
	}
	if len(directives) > MaxLines {
		return nil, fmt.Errorf("dnsmasq fragment exceeds the directive bound")
	}
	rules := make([]Rule, 0, len(directives))
	for _, line := range directives {
		rule, err := parseDirective(line)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	canonical, err := canonicalRules(rules)
	if err != nil {
		return nil, err
	}
	rendered, err := renderRules(canonical)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(rendered, payload) {
		return nil, fmt.Errorf("dnsmasq fragment is not in canonical order or form")
	}
	return canonical, nil
}

func Validate(payload []byte) error {
	_, err := Parse(payload)
	return err
}

// parseDirective reads one directive without reusing the renderer's formatting,
// so a document that merely looks similar cannot pass by construction.
func parseDirective(line string) (Rule, error) {
	if len(line) == 0 || len(line) > maxLineBytes {
		return Rule{}, fmt.Errorf("dnsmasq directive length is outside the supported bound")
	}
	if strings.TrimSpace(line) != line {
		return Rule{}, fmt.Errorf("dnsmasq directive %q is padded", line)
	}
	body, found := strings.CutPrefix(line, "nftset=/")
	if !found {
		return Rule{}, fmt.Errorf("dnsmasq directive %q is not an nftset option", line)
	}
	name, sets, found := strings.Cut(body, "/")
	if !found {
		return Rule{}, fmt.Errorf("dnsmasq directive %q has no set specification", line)
	}
	normalized, err := domain.NormalizeDomain(name)
	if err != nil || normalized != name {
		return Rule{}, fmt.Errorf("dnsmasq directive %q carries a non-canonical domain", line)
	}
	if sets != setSpecification() {
		return Rule{}, fmt.Errorf("dnsmasq directive %q does not target the routevane sets", line)
	}
	return Rule{Suffix: normalized}, nil
}

// setSpecification is the fixed comma-separated pair both families are added to.
func setSpecification() string {
	return "4#" + Family + "#" + Table + "#" + SetIPv4 + ",6#" + Family + "#" + Table + "#" + SetIPv6
}

func renderRules(input []Rule) ([]byte, error) {
	rules, err := canonicalRules(input)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	output.WriteString(Header)
	output.WriteString("\n")
	specification := setSpecification()
	for _, rule := range rules {
		line := "nftset=/" + rule.Suffix + "/" + specification
		if len(line) > maxLineBytes {
			return nil, fmt.Errorf("dnsmasq directive for %q exceeds the line bound", rule.Suffix)
		}
		output.WriteString(line)
		output.WriteString("\n")
	}
	if output.Len() > MaxArtifactSize {
		return nil, fmt.Errorf("dnsmasq fragment projection exceeds the byte bound")
	}
	return output.Bytes(), nil
}

// canonicalRules sorts and deduplicates directives and re-validates each domain.
// A fragment with no directive is refused: it would silently route nothing.
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
	if total > MaxLines {
		return nil, fmt.Errorf("projection exceeds entry bound")
	}
	return result, nil
}

func projectedRules(input []Rule) ([]Rule, error) {
	seen := make(map[string]struct{}, len(input))
	for _, rule := range input {
		normalized, err := domain.NormalizeDomain(rule.Suffix)
		if err != nil || normalized != rule.Suffix {
			return nil, fmt.Errorf("dnsmasq fragment contains a non-canonical domain %q", rule.Suffix)
		}
		seen[normalized] = struct{}{}
	}
	result := make([]Rule, 0, len(seen))
	for value := range seen {
		result = append(result, Rule{Suffix: value})
	}
	slices.SortFunc(result, func(a, b Rule) int { return cmp.Compare(a.Suffix, b.Suffix) })
	return result, nil
}
