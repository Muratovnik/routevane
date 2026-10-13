// Package keeneticdns renders the FQDN object groups KeeneticOS 5.0 and later
// route by name. Target declarations are checked against this format's bounds;
// coverage decisions belong to the planner.
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
	"crypto/sha256"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Muratovnik/routevane/internal/domain"
)

const (
	ID      = "keenetic-fqdn-group"
	Version = "keenetic-fqdn-group-v1"
	// GroupPrefix namespaces every group this product writes. The group budget
	// is shared with whatever the operator created by hand or with another
	// tool, and a collision would silently rewrite their group.
	GroupPrefix     = "routevane-"
	MaxPrefixLength = 24
	// MaxEntriesPerGroup bounds one group. Keenetic documents no entry limit for
	// an FQDN group, so this is our value, not theirs: it matches the figure the
	// field tooling settled on. The built-in target and aliases must declare
	// this same bound until configurable sharding is supported end to end.
	MaxEntriesPerGroup = 300
	// MaxGroups bounds how many groups one artifact defines. The only published
	// number is a KeeneticOS 5.0 changelog line raising the object-group limit
	// to 128, written before FQDN groups existed; it is used because it is the
	// only number there is.
	MaxGroups       = 128
	MaxArtifactSize = 256 << 10
	ContentType     = "text/plain; charset=utf-8"
	FileExtension   = "txt"

	maxGroupName   = 64
	maxEntryLength = 253
	maxLineBytes   = len("object-group fqdn  include \n") + maxGroupName + maxEntryLength
	// outputCodeBytes and listHashBytes are the SHA-256 prefixes in a group
	// name: the output code tells apart two outputs that render the same list
	// on one router, and the list hash keeps truncated long list IDs distinct.
	// Neither is ownership evidence; the deployer's ledger is.
	outputCodeBytes = 3
	listHashBytes   = 4
	// MaxDescriptionBytes is a product bound for a compact router list label,
	// not a claimed firmware limit.
	MaxDescriptionBytes = 96
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
	Name        string
	Entries     []string
	Description string
}

func Render(plan domain.RoutingPlan) ([]byte, error) {
	return (Renderer{}).RenderOutput(plan, "", "")
}

// RenderOutput binds names to immutable output identity. The readable prefix
// carries no ownership authority; deployment consults its persisted ledger.
func (Renderer) RenderOutput(plan domain.RoutingPlan, outputID, prefix string) ([]byte, error) {
	if err := ValidatePrefix(prefix); err != nil {
		return nil, err
	}
	groups, err := projectOutputPlan(plan, outputID, prefix)
	if err != nil {
		return nil, err
	}
	return renderGroups(groups)
}

// projectPlan turns already-decided rules into one group per list, split into
// numbered sub-groups when a list carries more entries than a group holds.
// Grouping by list preserves stable identity, while the native description
// carries the readable title independently of that identity. A group is named
// <prefix>-<list part>-<output code>-s<shard> (ADR 0049).
func projectPlan(plan domain.RoutingPlan) ([]Group, error) {
	return projectOutputPlan(plan, "", "")
}

func projectOutputPlan(plan domain.RoutingPlan, outputID, prefix string) ([]Group, error) {
	if prefix == "" {
		prefix = "routevane"
	}
	owner := sha256.Sum256([]byte(outputID))
	code := fmt.Sprintf("%x", owner[:outputCodeBytes])
	byList := make(map[string][]string)
	labels := make(map[string][]string)
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
		labels[rule.ListID] = append(labels[rule.ListID], rule.Labels...)
	}
	lists := slices.Sorted(maps.Keys(byList))
	groups := make([]Group, 0, len(lists))
	seen := make(map[string]bool)
	for _, list := range lists {
		entries := domain.StableStrings(byList[list])
		description := listDescription(list, labels[list])
		shards := (len(entries) + MaxEntriesPerGroup - 1) / MaxEntriesPerGroup
		part := listNamePart(list, prefix, shards)
		for index := 0; index < len(entries); index += MaxEntriesPerGroup {
			end := min(index+MaxEntriesPerGroup, len(entries))
			name := fmt.Sprintf("%s-%s-%s-s%d", prefix, part, code, index/MaxEntriesPerGroup+1)
			if len(name) > maxGroupName {
				return nil, fmt.Errorf("keenetic FQDN group name exceeds the bound")
			}
			if seen[name] {
				return nil, fmt.Errorf("keenetic FQDN group identity collision")
			}
			seen[name] = true
			label := description
			if shards > 1 {
				suffix := fmt.Sprintf(" (%d/%d)", index/MaxEntriesPerGroup+1, shards)
				label = truncateDescription(description, MaxDescriptionBytes-len(suffix)) + suffix
			}
			groups = append(groups, Group{Name: name, Entries: entries[index:end], Description: label})
		}
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("routing plan contains no group")
	}
	return groups, nil
}

// listNamePart is the readable list component of a group name: the list ID
// itself when it fits, otherwise its head and a hash of the whole ID, so two
// long lists sharing a head stay distinct. The room for the shard suffix is
// reserved for the device's largest group count, so every shard of one list
// carries the same list part. A list too large to render at all still gets a
// bounded name, leaving the refusal to the group budget that names the cause.
func listNamePart(list, prefix string, shards int) string {
	shardSuffix := len(fmt.Sprintf("-s%d", max(shards, MaxGroups)))
	budget := maxGroupName - len(prefix) - len("-") - len("-") - 2*outputCodeBytes - shardSuffix
	if len(list) <= budget {
		return list
	}
	identity := sha256.Sum256([]byte(list))
	hash := fmt.Sprintf("-%x", identity[:listHashBytes])
	return strings.TrimRight(list[:budget-len(hash)], "-") + hash
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
		if !rule.Addr.IsValid() || rule.Addr.Zone() != "" {
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
		groups = append(groups, Group{Name: group.Name, Entries: domain.StableStrings(group.Entries), Description: group.Description})
	}
	slices.SortFunc(groups, func(a, b Group) int { return cmp.Compare(a.Name, b.Name) })
	if len(groups) > MaxGroups {
		return nil, fmt.Errorf("keenetic FQDN projection needs %d groups and the device holds %d", len(groups), MaxGroups)
	}
	var output bytes.Buffer
	for _, group := range groups {
		if !ValidGroupName(group.Name) {
			return nil, fmt.Errorf("keenetic FQDN projection contains an invalid group name")
		}
		if len(group.Entries) == 0 || len(group.Entries) > MaxEntriesPerGroup {
			return nil, fmt.Errorf("keenetic FQDN group holds %d entries and the bound is %d", len(group.Entries), MaxEntriesPerGroup)
		}
		if group.Description != "" {
			if !ValidDescription(group.Description) {
				return nil, fmt.Errorf("keenetic FQDN projection contains an invalid description")
			}
			output.WriteString("object-group fqdn " + group.Name + " description \"" + group.Description + "\"\n")
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
	if !utf8.Valid(payload) {
		return nil, fmt.Errorf("keenetic FQDN file must be valid UTF-8")
	}
	for _, value := range payload {
		if value != '\n' && (value < 0x20 || value == 0x7f) {
			return nil, fmt.Errorf("keenetic FQDN file contains a control byte")
		}
	}
	lines := strings.Split(string(payload[:len(payload)-1]), "\n")
	groups := make([]Group, 0, 8)
	index := make(map[string]int, 8)
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, " ", 5)
		if len(parts) != 5 || parts[0] != "object-group" || parts[1] != "fqdn" {
			return nil, fmt.Errorf("keenetic FQDN file contains an unknown command")
		}
		name, value := parts[2], parts[4]
		if !ValidGroupName(name) {
			return nil, fmt.Errorf("keenetic FQDN file contains an invalid group")
		}
		position, exists := index[name]
		if !exists {
			position = len(groups)
			index[name] = position
			groups = append(groups, Group{Name: name})
		}
		switch parts[3] {
		case "include":
			if !ValidEntry(value) {
				return nil, fmt.Errorf("keenetic FQDN file contains an invalid entry")
			}
			if _, duplicate := seen[name+" "+value]; duplicate {
				return nil, fmt.Errorf("keenetic FQDN file repeats an entry")
			}
			seen[name+" "+value] = struct{}{}
			groups[position].Entries = append(groups[position].Entries, value)
		case "description":
			if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' || !ValidDescription(value[1:len(value)-1]) || groups[position].Description != "" {
				return nil, fmt.Errorf("keenetic FQDN file contains an invalid or repeated description")
			}
			groups[position].Description = value[1 : len(value)-1]
		default:
			return nil, fmt.Errorf("keenetic FQDN file contains an unknown command")
		}
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

// ValidGroupName accepts the command-safe object identity used by artifacts
// and the exact ownership boundary when interpreting device observations.
func ValidGroupName(name string) bool {
	rest := name
	if rest == "" || len(name) > maxGroupName {
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

// ValidatePrefix accepts only a compact CLI-safe human-readable label.
func ValidatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if len(prefix) > MaxPrefixLength || domain.ValidateSlug(prefix) != nil {
		return fmt.Errorf("FQDN group prefix must be a lowercase slug of at most %d characters", MaxPrefixLength)
	}
	return nil
}

// ValidEntry accepts one canonical domain, address or prefix CLI argument.
func ValidEntry(value string) bool {
	if value == "" || len(value) > maxEntryLength {
		return false
	}
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		return err == nil && prefix.Bits() > 0 && prefix.Masked().String() == value
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address.Zone() == "" && address.Unmap().String() == value
	}
	normalized, err := domain.NormalizeDomain(value)
	return err == nil && normalized == value
}

// ValidDescription accepts a literal quoted CLI string without escape syntax.
// The projection preserves Unicode titles while replacing quote delimiters;
// untrusted artifact text cannot close the string or introduce another command.
func ValidDescription(value string) bool {
	if value == "" || !utf8.ValidString(value) || len(value) > MaxDescriptionBytes || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\"\\") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func listDescription(list string, labels []string) string {
	title := list
	for _, label := range domain.StableStrings(labels) {
		// Publication owns the escaped (category/list) provenance grammar.
		if !strings.HasPrefix(label, "(") || !strings.HasSuffix(label, ")") {
			continue
		}
		if _, candidate, found := strings.Cut(label[1:len(label)-1], "/"); found && candidate != "" {
			title = candidate
			break
		}
	}
	title = strings.Map(func(r rune) rune {
		switch {
		case r == '"':
			return '\u201d'
		case r == '\\':
			return '\uff3c'
		case unicode.IsControl(r) || unicode.IsSpace(r):
			return ' '
		default:
			return r
		}
	}, title)
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		title = list
	}
	return truncateDescription(title, MaxDescriptionBytes)
}

func truncateDescription(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return strings.TrimSpace(value)
}
