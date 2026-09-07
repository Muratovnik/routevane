// Package keeneticdns renders the FQDN object groups KeeneticOS 5.0 and later
// route by name. It owns format only: target limits and coverage decisions are
// supplied by the target definition and planner.
//
// Three properties make this a distinct format rather than a second copy of the
// static-route dialect. The device expands the subdomains of a listed name
// itself, so a suffix is the natural shape and an exact name cannot be
// expressed. A group is a named object on the device with a device-wide budget,
// so this renderer namespaces every group it writes and never touches a name it
// did not create. And a group holds a bounded number of entries, so a list
// that exceeds the bound is split into numbered sub-groups: the operator asked
// for one list, and the split is the adapter's business.
//
// The file defines groups and nothing else. Pointing a group at an interface is
// a decision about this router that the operator makes on the device, and a
// file that guessed it would either be wrong or carry a placeholder that fails
// when pasted.
package keeneticdns

import (
	"bytes"
	"cmp"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

const (
	ID      = "keenetic-fqdn-group"
	Version = "keenetic-fqdn-group-v1"
	// GroupPrefix namespaces every group this product writes. The group budget
	// is shared with whatever the operator created by hand or with another
	// tool, and a collision would silently rewrite their group.
	GroupPrefix = "routevane-"
	// MaxEntriesPerGroup bounds one group. Keenetic documents no entry limit for
	// an FQDN group, so this is our value, not theirs: it matches the figure the
	// field tooling settled on, and it lives here and in the target definition so a
	// firmware change is an edit rather than a release.
	MaxEntriesPerGroup = 300
	// MaxGroups bounds how many groups one artifact defines. The only published
	// number is a KeeneticOS 5.0 changelog line raising the object-group limit
	// to 128, written before FQDN groups existed; it is used because it is the
	// only number there is.
	MaxGroups       = 128
	MaxArtifactSize = 256 << 10
	ContentType     = "text/plain; charset=utf-8"
	FileExtension   = "txt"

	maxLineBytes   = 320
	maxGroupName   = 64
	maxEntryLength = 253
)

type Renderer struct{}

func (Renderer) ID() string      { return ID }
func (Renderer) Version() string { return Version }
func (Renderer) Descriptor() domain.RendererDescriptor {
	return domain.RendererDescriptor{ID: ID, Version: Version, ContentType: ContentType, FileExtension: FileExtension}
}

// SupportedRuleKinds carries name suffixes and both address families, which is
// what an FQDN object group accepts. An exact name is absent on purpose: the
// device includes every subdomain of a listed name and cannot be told not to.
func (Renderer) SupportedRuleKinds() []domain.RuleKind {
	return []domain.RuleKind{domain.RuleDomainSuffix, domain.RuleIPv4, domain.RuleIPv6, domain.RulePrefix4, domain.RulePrefix6}
}

func (Renderer) ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	if len(plan.Rules) == 0 {
		return 0, nil
	}
	groups, err := projectPlan(plan)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, group := range groups {
		total += len(group.Entries)
	}
	return total, nil
}

func (Renderer) Render(plan domain.RoutingPlan) ([]byte, error) { return Render(plan) }
func (Renderer) Validate(payload []byte) error                  { return Validate(payload) }

// Group is one named object group as the independent validator projects it.
type Group struct {
	Name    string
	Entries []string
}

func Render(plan domain.RoutingPlan) ([]byte, error) {
	groups, err := projectPlan(plan)
	if err != nil {
		return nil, err
	}
	return renderGroups(groups)
}

// projectPlan turns already-decided rules into one group per list, split into
// numbered sub-groups when a list carries more entries than a group holds.
// Grouping by list is what makes a group on the router attributable: an
// operator reading the name knows what it is and what removing it costs.
func projectPlan(plan domain.RoutingPlan) ([]Group, error) {
	byList := make(map[string][]string)
	for _, rule := range plan.Rules {
		if !rule.IsValid() || rule.Action != domain.ActionRoute {
			return nil, fmt.Errorf("routing plan contains an invalid route rule")
		}
		if domain.ValidateSlug(rule.ListID) != nil {
			return nil, fmt.Errorf("routing plan contains an invalid list identity")
		}
		value, err := entryValue(rule)
		if err != nil {
			return nil, err
		}
		byList[rule.ListID] = append(byList[rule.ListID], value)
	}
	lists := slices.Sorted(maps.Keys(byList))
	groups := make([]Group, 0, len(lists))
	for _, list := range lists {
		entries := domain.StableStrings(byList[list])
		for index := 0; index < len(entries); index += MaxEntriesPerGroup {
			end := min(index+MaxEntriesPerGroup, len(entries))
			name := GroupPrefix + list
			if index > 0 {
				name = fmt.Sprintf("%s-%d", name, index/MaxEntriesPerGroup+1)
			}
			if len(name) > maxGroupName {
				return nil, fmt.Errorf("keenetic FQDN group name exceeds the bound")
			}
			groups = append(groups, Group{Name: name, Entries: entries[index:end]})
		}
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("routing plan contains no group")
	}
	return groups, nil
}

func entryValue(rule domain.RouteRule) (string, error) {
	switch rule.Kind {
	case domain.RuleDomainSuffix:
		value, err := domain.NormalizeDomain(rule.Domain)
		if err != nil {
			return "", fmt.Errorf("routing plan contains an invalid domain")
		}
		return value, nil
	case domain.RuleIPv4, domain.RuleIPv6:
		if !rule.Addr.IsValid() {
			return "", fmt.Errorf("routing plan contains an invalid address")
		}
		return rule.Addr.Unmap().String(), nil
	case domain.RulePrefix4, domain.RulePrefix6:
		prefix := rule.Prefix.Masked()
		if !prefix.IsValid() || prefix.Bits() <= 0 {
			return "", fmt.Errorf("routing plan contains an invalid prefix")
		}
		return prefix.String(), nil
	default:
		return "", fmt.Errorf("routing plan contains an unsupported rule kind")
	}
}

// renderGroups writes the one canonical form of a group set. Canonicalizing
// here rather than at projection is what lets Parse re-render its own reading
// and refuse a file that carries the same commands in another order: two files
// that mean the same thing must be the same bytes, or the artifact hash stops
// identifying content.
func renderGroups(input []Group) ([]byte, error) {
	groups := make([]Group, 0, len(input))
	for _, group := range input {
		groups = append(groups, Group{Name: group.Name, Entries: domain.StableStrings(group.Entries)})
	}
	slices.SortFunc(groups, func(a, b Group) int { return cmp.Compare(a.Name, b.Name) })
	if len(groups) > MaxGroups {
		return nil, fmt.Errorf("keenetic FQDN projection needs %d groups and the device holds %d", len(groups), MaxGroups)
	}
	var output bytes.Buffer
	for _, group := range groups {
		if !validGroupName(group.Name) {
			return nil, fmt.Errorf("keenetic FQDN projection contains an invalid group name")
		}
		if len(group.Entries) == 0 || len(group.Entries) > MaxEntriesPerGroup {
			return nil, fmt.Errorf("keenetic FQDN group holds %d entries and the bound is %d", len(group.Entries), MaxEntriesPerGroup)
		}
		for _, entry := range group.Entries {
			// One self-contained command per line. The nested form would leave a
			// half-applied group behind if a paste were cut short.
			line := "object-group fqdn " + group.Name + " include " + entry + "\n"
			if len(line) > maxLineBytes {
				return nil, fmt.Errorf("keenetic FQDN projection line exceeds the bound")
			}
			output.WriteString(line)
		}
	}
	if output.Len() > MaxArtifactSize {
		return nil, fmt.Errorf("keenetic FQDN projection exceeds the byte bound")
	}
	return output.Bytes(), nil
}

// Parse validates the dialect independently and returns the typed groups.
// Canonical ordering is checked by rendering that projection again.
func Parse(payload []byte) ([]Group, error) {
	if len(payload) == 0 || len(payload) > MaxArtifactSize {
		return nil, fmt.Errorf("keenetic FQDN size is outside the supported bound")
	}
	if !bytes.HasSuffix(payload, []byte("\n")) {
		return nil, fmt.Errorf("keenetic FQDN file must end with a newline")
	}
	for _, value := range payload {
		if value >= 0x80 {
			return nil, fmt.Errorf("keenetic FQDN file must be ASCII without a BOM")
		}
		if value != '\n' && (value < 0x20 || value == 0x7f) {
			return nil, fmt.Errorf("keenetic FQDN file contains a control byte")
		}
	}
	lines := strings.Split(string(payload[:len(payload)-1]), "\n")
	groups := make([]Group, 0, 8)
	index := make(map[string]int, 8)
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, " ")
		if len(parts) != 5 || parts[0] != "object-group" || parts[1] != "fqdn" || parts[3] != "include" {
			return nil, fmt.Errorf("keenetic FQDN file contains an unknown command")
		}
		name, entry := parts[2], parts[4]
		if !validGroupName(name) || !validEntry(entry) {
			return nil, fmt.Errorf("keenetic FQDN file contains an invalid group or entry")
		}
		if _, duplicate := seen[name+" "+entry]; duplicate {
			return nil, fmt.Errorf("keenetic FQDN file repeats an entry")
		}
		seen[name+" "+entry] = struct{}{}
		position, exists := index[name]
		if !exists {
			position = len(groups)
			index[name] = position
			groups = append(groups, Group{Name: name})
		}
		groups[position].Entries = append(groups[position].Entries, entry)
	}
	canonical, err := renderGroups(groups)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, payload) {
		return nil, fmt.Errorf("keenetic FQDN file is not in canonical order or form")
	}
	return groups, nil
}

func Validate(payload []byte) error {
	_, err := Parse(payload)
	return err
}

func validGroupName(name string) bool {
	rest, found := strings.CutPrefix(name, GroupPrefix)
	if !found || rest == "" || len(name) > maxGroupName {
		return false
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return rest[0] != '-' && rest[len(rest)-1] != '-'
}

func validEntry(value string) bool {
	if value == "" || len(value) > maxEntryLength {
		return false
	}
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		return err == nil && prefix.Bits() > 0 && prefix.Masked().String() == value
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address.Unmap().String() == value
	}
	normalized, err := domain.NormalizeDomain(value)
	return err == nil && normalized == value
}
