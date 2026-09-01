// Package mikrotik renders the RouterOS script that populates the firewall
// address lists a selective-routing rule matches. It owns format only: target
// limits and coverage decisions are supplied by the target profile and planner.
//
// Two properties make this format a distinct case rather than a second copy of
// the router dialect. It is replacing rather than additive: each section removes
// the entries this product owns before adding the current ones, so re-importing
// a newer artifact converges instead of accumulating. And RouterOS accepts a DNS
// name in the address property, resolving it and maintaining dynamic entries, so
// an exact domain is carried as a name rather than as the addresses observed
// here. A domain therefore appears in both sections when the target carries both
// families, because the IPv4 list resolves A records and the IPv6 list resolves
// AAAA records.
//
// A suffix cannot be expressed: an address-list entry is one name, not a match
// pattern, so such a plan is refused rather than approximated.
package mikrotik

import (
	"bytes"
	"cmp"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

const (
	ID      = "mikrotik-address-list-rsc"
	Version = "mikrotik-address-list-rsc-v1"
	// ListIPv4 and ListIPv6 are the two lists this product owns. They are fixed
	// rather than configurable: the script removes every entry of the list it
	// writes, so a configurable name would let one artifact clear a list the
	// operator maintains by hand.
	ListIPv4 = "routevane4"
	ListIPv6 = "routevane6"
	// Header identifies the script in a RouterOS file list. The validator
	// requires exactly this line.
	Header          = "# routevane " + Version
	MaxLines        = 4096
	MaxArtifactSize = 256 << 10
	ContentType     = "text/plain; charset=utf-8"
	FileExtension   = "rsc"
	maxLineBytes    = 320

	sectionIPv4 = "/ip firewall address-list"
	sectionIPv6 = "/ipv6 firewall address-list"
)

type Renderer struct{}

func (Renderer) ID() string      { return ID }
func (Renderer) Version() string { return Version }
func (Renderer) Descriptor() domain.RendererDescriptor {
	return domain.RendererDescriptor{ID: ID, Version: Version, ContentType: ContentType, FileExtension: FileExtension}
}

// SupportedRuleKinds carries exact domains and both address families. A suffix
// is absent on purpose: an address-list entry is a single name.
func (Renderer) SupportedRuleKinds() []domain.RuleKind {
	return []domain.RuleKind{
		domain.RuleDomainExact,
		domain.RuleIPv4, domain.RuleIPv6,
		domain.RulePrefix4, domain.RulePrefix6,
	}
}

func (Renderer) ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	return ProjectedRuleCount(plan)
}
func (Renderer) Render(plan domain.RoutingPlan) ([]byte, error) { return Render(plan) }
func (Renderer) Validate(payload []byte) error                  { return Validate(payload) }

// Script is the independent validator's typed projection of the document. Each
// section holds the entry values in the order the script writes them.
type Script struct {
	IPv4 []string
	IPv6 []string
}

// Render projects already-decided rules into one canonical RouterOS script.
func Render(plan domain.RoutingPlan) ([]byte, error) {
	script, err := projectPlan(plan)
	if err != nil {
		return nil, err
	}
	return renderScript(script)
}

// ProjectedRuleCount counts the address-list entries the script writes. A domain
// counts once per section because each section carries its own entry, and that
// is what a device-side list limit actually holds.
func ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	script, err := projectPlan(plan)
	if err != nil {
		return 0, err
	}
	canonical, err := canonicalScript(script)
	if err != nil {
		return 0, err
	}
	return len(canonical.IPv4) + len(canonical.IPv6), nil
}

func projectPlan(plan domain.RoutingPlan) (Script, error) {
	script := Script{}
	for _, route := range plan.Rules {
		if !route.IsValid() || route.Action != domain.ActionRoute {
			return Script{}, fmt.Errorf("routing plan contains an invalid route rule")
		}
		switch route.Kind {
		case domain.RuleDomainExact:
			// RouterOS resolves the name per family, so the same name belongs in
			// both lists.
			name := route.CanonicalValue()
			script.IPv4 = append(script.IPv4, name)
			script.IPv6 = append(script.IPv6, name)
		case domain.RuleIPv4:
			script.IPv4 = append(script.IPv4, netip.PrefixFrom(route.Addr, 32).String())
		case domain.RuleIPv6:
			script.IPv6 = append(script.IPv6, netip.PrefixFrom(route.Addr, 128).String())
		case domain.RulePrefix4:
			script.IPv4 = append(script.IPv4, route.Prefix.Masked().String())
		case domain.RulePrefix6:
			script.IPv6 = append(script.IPv6, route.Prefix.Masked().String())
		default:
			return Script{}, fmt.Errorf("routing plan contains a rule kind this format cannot express")
		}
	}
	return script, nil
}

// Parse decodes the script independently of Render and returns the typed
// projection. Canonical order and form are then proven by rendering the
// projection again and requiring byte equality.
func Parse(payload []byte) (Script, error) {
	if len(payload) == 0 || len(payload) > MaxArtifactSize {
		return Script{}, fmt.Errorf("RouterOS script size is outside the supported bound")
	}
	if !bytes.HasSuffix(payload, []byte("\n")) {
		return Script{}, fmt.Errorf("RouterOS script must end with one newline")
	}
	if bytes.ContainsRune(payload, '\r') {
		return Script{}, fmt.Errorf("RouterOS script must use LF line endings")
	}
	lines := strings.Split(strings.TrimSuffix(string(payload), "\n"), "\n")
	if len(lines) == 0 || lines[0] != Header {
		return Script{}, fmt.Errorf("RouterOS script must begin with the routevane header line")
	}
	body := lines[1:]
	if len(body) > MaxLines {
		return Script{}, fmt.Errorf("RouterOS script exceeds the line bound")
	}
	script := Script{}
	current := ""
	for index, line := range body {
		if len(line) == 0 || len(line) > maxLineBytes || strings.TrimSpace(line) != line {
			return Script{}, fmt.Errorf("RouterOS script line %q is empty, padded, or too long", line)
		}
		switch {
		case line == sectionIPv4 || line == sectionIPv6:
			// A section may appear once, in family order, and must be followed
			// immediately by the removal that makes the import replacing. A
			// section that only added would accumulate entries the operator
			// never asked to keep.
			if current == line || (line == sectionIPv4 && current == sectionIPv6) {
				return Script{}, fmt.Errorf("RouterOS script repeats a section or orders them wrongly")
			}
			current = line
			expected := "remove [find list=" + listFor(current) + "]"
			if index+1 >= len(body) || body[index+1] != expected {
				return Script{}, fmt.Errorf("RouterOS section %q must be followed by %q", line, expected)
			}
		case strings.HasPrefix(line, "remove "):
			if current == "" {
				return Script{}, fmt.Errorf("RouterOS script removes entries outside a section")
			}
			if line != "remove [find list="+listFor(current)+"]" {
				return Script{}, fmt.Errorf("RouterOS script line %q does not clear the routevane list", line)
			}
		case strings.HasPrefix(line, "add "):
			if current == "" {
				return Script{}, fmt.Errorf("RouterOS script adds an entry outside a section")
			}
			value, err := parseAddCommand(line, listFor(current))
			if err != nil {
				return Script{}, err
			}
			if current == sectionIPv4 {
				script.IPv4 = append(script.IPv4, value)
			} else {
				script.IPv6 = append(script.IPv6, value)
			}
		default:
			return Script{}, fmt.Errorf("RouterOS script line %q is not a supported command", line)
		}
	}
	canonical, err := canonicalScript(script)
	if err != nil {
		return Script{}, err
	}
	rendered, err := renderScript(canonical)
	if err != nil {
		return Script{}, err
	}
	if !bytes.Equal(rendered, payload) {
		return Script{}, fmt.Errorf("RouterOS script is not in canonical order or form")
	}
	return canonical, nil
}

func Validate(payload []byte) error {
	_, err := Parse(payload)
	return err
}

func listFor(section string) string {
	if section == sectionIPv6 {
		return ListIPv6
	}
	return ListIPv4
}

// parseAddCommand reads one entry without reusing the renderer's formatting, so
// a script that merely looks similar cannot pass by construction.
func parseAddCommand(line, list string) (string, error) {
	rest, found := strings.CutPrefix(line, "add address=")
	if !found {
		return "", fmt.Errorf("RouterOS command %q is not a supported add", line)
	}
	value, suffix, found := strings.Cut(rest, " ")
	if !found || suffix != "list="+list {
		return "", fmt.Errorf("RouterOS command %q does not target the routevane list", line)
	}
	if value == "" {
		return "", fmt.Errorf("RouterOS command %q has no address", line)
	}
	return value, nil
}

func renderScript(input Script) ([]byte, error) {
	script, err := canonicalScript(input)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	output.WriteString(Header)
	output.WriteString("\n")
	for _, section := range []struct {
		path    string
		list    string
		entries []string
	}{
		{sectionIPv4, ListIPv4, script.IPv4},
		{sectionIPv6, ListIPv6, script.IPv6},
	} {
		if len(section.entries) == 0 {
			continue
		}
		output.WriteString(section.path)
		output.WriteString("\n")
		output.WriteString("remove [find list=" + section.list + "]\n")
		for _, entry := range section.entries {
			line := "add address=" + entry + " list=" + section.list
			if len(line) > maxLineBytes {
				return nil, fmt.Errorf("RouterOS command for %q exceeds the line bound", entry)
			}
			output.WriteString(line)
			output.WriteString("\n")
		}
	}
	if output.Len() > MaxArtifactSize {
		return nil, fmt.Errorf("RouterOS script projection exceeds the byte bound")
	}
	return output.Bytes(), nil
}

// canonicalScript sorts and deduplicates each section and re-validates every
// entry against the family that section carries. A script with no entry at all
// is refused: it would clear both lists and route nothing.
func canonicalScript(input Script) (Script, error) {
	ipv4, err := canonicalSection(input.IPv4, true)
	if err != nil {
		return Script{}, err
	}
	ipv6, err := canonicalSection(input.IPv6, false)
	if err != nil {
		return Script{}, err
	}
	total := len(ipv4) + len(ipv6)
	if total == 0 {
		return Script{}, fmt.Errorf("RouterOS script projection contains no address-list entry")
	}
	if total > MaxLines {
		return Script{}, fmt.Errorf("RouterOS script projection exceeds the entry bound")
	}
	return Script{IPv4: ipv4, IPv6: ipv6}, nil
}

// canonicalSection orders prefixes before names. Both are valid values of the
// same property, so the order has to be fixed by this renderer rather than left
// to the order the plan happened to carry.
func canonicalSection(values []string, wantIPv4 bool) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	prefixes := make([]netip.Prefix, 0, len(values))
	seenPrefix := make(map[string]struct{}, len(values))
	names := make([]string, 0, len(values))
	seenName := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != value || value == "" {
			return nil, fmt.Errorf("RouterOS script contains a padded entry %q", value)
		}
		// A bare address is refused rather than accepted as an equivalent of its
		// host prefix: one value must have one canonical spelling.
		if _, err := netip.ParseAddr(value); err == nil {
			return nil, fmt.Errorf("RouterOS script contains the bare address %q instead of a prefix", value)
		}
		if strings.Contains(value, "/") {
			prefix, err := domain.ParsePrefix(value)
			if err != nil || prefix.String() != value {
				return nil, fmt.Errorf("RouterOS script contains a non-canonical prefix %q", value)
			}
			if prefix.Addr().Is4() != wantIPv4 {
				return nil, fmt.Errorf("RouterOS script contains %q in the wrong address family section", value)
			}
			if _, duplicate := seenPrefix[prefix.String()]; duplicate {
				continue
			}
			seenPrefix[prefix.String()] = struct{}{}
			prefixes = append(prefixes, prefix)
			continue
		}
		normalized, err := domain.NormalizeDomain(value)
		if err != nil || normalized != value {
			return nil, fmt.Errorf("RouterOS script contains a non-canonical domain %q", value)
		}
		if _, duplicate := seenName[normalized]; duplicate {
			continue
		}
		seenName[normalized] = struct{}{}
		names = append(names, normalized)
	}
	slices.SortFunc(prefixes, func(a, b netip.Prefix) int {
		return cmp.Or(a.Addr().Compare(b.Addr()), cmp.Compare(a.Bits(), b.Bits()))
	})
	slices.Sort(names)
	result := make([]string, 0, len(prefixes)+len(names))
	for _, prefix := range prefixes {
		result = append(result, prefix.String())
	}
	return append(result, names...), nil
}
