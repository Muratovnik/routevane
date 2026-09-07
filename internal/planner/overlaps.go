package planner

import (
	"maps"
	"net/netip"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

// OverlapValue groups equal typed rules by distinct service, not component or
// category. It describes planned destinations, never raw or expired evidence.
type OverlapValue struct {
	RuleKind domain.RuleKind
	Value    string
	Lists    []string
}

type RuleOverlap struct {
	Kind     string
	Entry    OverlapValue
	Covering *OverlapValue
}

type RuleOverlaps struct {
	Items     []RuleOverlap
	Truncated bool
	// Summary is the complete undirected service adjacency graph for the
	// supplied rules. It is intentionally independent from Items' diagnostic
	// limit: callers may cap detail rows without losing which selected services
	// overlap. Every service represented by a valid rule gets a row, including a
	// zero-degree service; the application layer adds rows for selected services
	// that contributed no rules at all.
	Summary []OverlapSummary
}

// OverlapSummary is one row of the complete overlap adjacency graph. The
// service id never appears in its own Overlaps slice.
type OverlapSummary struct {
	ListID   string
	Overlaps []string
}

type overlapGroup struct {
	rule  domain.RouteRule
	entry OverlapValue
}

func overlapKey(kind domain.RuleKind, value string) string {
	return string(kind) + "\x00" + value
}

// AnalyzeRuleOverlaps explains a prepared plan without changing its rules or
// claiming renderer savings. Equal typed values come first, then containment in
// canonical value/owner order. At most limit details are returned; Truncated
// means another real relation was found, not that a guessed total was reached.
//
// The index searches only domain-label ancestors and network widths present in
// the plan. It avoids an all-pairs scan on large disjoint source lists. Ownership
// remains separate from set union: a service is never its own covering owner.
func AnalyzeRuleOverlaps(rules []domain.RouteRule, limit int) RuleOverlaps {
	result := RuleOverlaps{Items: []RuleOverlap{}, Summary: []OverlapSummary{}}
	limit = max(0, limit)
	groups := make(map[string]*overlapGroup)
	lists := make(map[string]struct{})
	adjacency := make(map[string]map[string]struct{})
	addEdge := func(a, b string) {
		if a == "" || b == "" || a == b {
			return
		}
		if adjacency[a] == nil {
			adjacency[a] = map[string]struct{}{}
		}
		if adjacency[b] == nil {
			adjacency[b] = map[string]struct{}{}
		}
		adjacency[a][b] = struct{}{}
		adjacency[b][a] = struct{}{}
	}
	for _, rule := range rules {
		if !rule.IsValid() {
			continue
		}
		lists[rule.ListID] = struct{}{}
		value := rule.CanonicalValue()
		key := overlapKey(rule.Kind, value)
		group := groups[key]
		if group == nil {
			group = &overlapGroup{rule: rule, entry: OverlapValue{RuleKind: rule.Kind, Value: value}}
			groups[key] = group
		}
		group.entry.Lists = append(group.entry.Lists, rule.ListID)
	}
	keys := make([]string, 0, len(groups))
	widths := map[domain.RuleKind][]int{}
	for key, group := range groups {
		keys = append(keys, key)
		group.entry.Lists = domain.StableStrings(group.entry.Lists)
		if group.rule.Kind.IsPrefix() {
			widths[group.rule.Kind] = append(widths[group.rule.Kind], group.rule.Prefix.Bits())
		}
	}
	slices.Sort(keys)
	for kind, bits := range widths {
		slices.Sort(bits)
		bits = slices.Compact(bits)
		slices.Reverse(bits)
		widths[kind] = bits
	}
	appendDetail := func(item RuleOverlap) {
		if len(result.Items) >= limit {
			result.Truncated = true
			return
		}
		result.Items = append(result.Items, item)
	}
	for _, key := range keys {
		entry := groups[key].entry
		if len(entry.Lists) > 1 {
			for i, listID := range entry.Lists {
				for _, other := range entry.Lists[i+1:] {
					addEdge(listID, other)
				}
			}
			appendDetail(RuleOverlap{Kind: "duplicate", Entry: entry})
		}
	}
	for _, key := range keys {
		group := groups[key]
		for _, parentKey := range overlapParents(group.rule, widths) {
			parent := groups[parentKey]
			if parent == nil {
				continue
			}
			for _, listID := range group.entry.Lists {
				owners := make([]string, 0, len(parent.entry.Lists))
				for _, owner := range parent.entry.Lists {
					if owner != listID {
						owners = append(owners, owner)
						addEdge(listID, owner)
					}
				}
				if len(owners) == 0 {
					continue
				}
				entry, covering := group.entry, parent.entry
				entry.Lists, covering.Lists = []string{listID}, owners
				appendDetail(RuleOverlap{Kind: "covered", Entry: entry, Covering: &covering})
			}
		}
	}
	listIDs := slices.Sorted(maps.Keys(lists))
	for _, listID := range listIDs {
		overlaps := slices.Sorted(maps.Keys(adjacency[listID]))
		result.Summary = append(result.Summary, OverlapSummary{ListID: listID, Overlaps: overlaps})
	}
	return result
}

func overlapParents(rule domain.RouteRule, widths map[domain.RuleKind][]int) []string {
	parents := []string{}
	if rule.Kind.IsDomain() {
		name := rule.Domain
		if rule.Kind == domain.RuleDomainExact {
			parents = append(parents, overlapKey(domain.RuleDomainSuffix, name))
		}
		for {
			_, tail, found := strings.Cut(name, ".")
			if !found {
				break
			}
			parents = append(parents, overlapKey(domain.RuleDomainSuffix, tail))
			name = tail
		}
		return parents
	}
	addr, maximum := rule.Addr, rule.Addr.BitLen()
	if rule.Kind.IsPrefix() {
		addr, maximum = rule.Prefix.Addr(), rule.Prefix.Bits()-1
	}
	kind := domain.RulePrefix6
	if addr.Is4() {
		kind = domain.RulePrefix4
	}
	for _, bits := range widths[kind] {
		if bits <= maximum {
			prefix := netip.PrefixFrom(addr, bits).Masked()
			parents = append(parents, overlapKey(kind, prefix.String()))
		}
	}
	return parents
}
