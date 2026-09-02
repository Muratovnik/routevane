package planner

import (
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
	Services []string
}

type RuleOverlap struct {
	Kind     string
	Entry    OverlapValue
	Covering *OverlapValue
}

type RuleOverlaps struct {
	Items     []RuleOverlap
	Truncated bool
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
	result := RuleOverlaps{Items: []RuleOverlap{}}
	limit = max(0, limit)
	groups := make(map[string]*overlapGroup)
	for _, rule := range rules {
		if !rule.IsValid() {
			continue
		}
		value := rule.CanonicalValue()
		key := overlapKey(rule.Kind, value)
		group := groups[key]
		if group == nil {
			group = &overlapGroup{rule: rule, entry: OverlapValue{RuleKind: rule.Kind, Value: value}}
			groups[key] = group
		}
		group.entry.Services = append(group.entry.Services, rule.ServiceID)
	}
	keys := make([]string, 0, len(groups))
	widths := map[domain.RuleKind][]int{}
	for key, group := range groups {
		keys = append(keys, key)
		group.entry.Services = domain.StableStrings(group.entry.Services)
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
	appendDetail := func(item RuleOverlap) bool {
		if len(result.Items) == limit {
			result.Truncated = true
			return false
		}
		result.Items = append(result.Items, item)
		return true
	}
	for _, key := range keys {
		entry := groups[key].entry
		if len(entry.Services) > 1 && !appendDetail(RuleOverlap{Kind: "duplicate", Entry: entry}) {
			return result
		}
	}
	for _, key := range keys {
		group := groups[key]
		for _, parentKey := range overlapParents(group.rule, widths) {
			parent := groups[parentKey]
			if parent == nil {
				continue
			}
			for _, serviceID := range group.entry.Services {
				owners := make([]string, 0, len(parent.entry.Services))
				for _, owner := range parent.entry.Services {
					if owner != serviceID {
						owners = append(owners, owner)
					}
				}
				if len(owners) == 0 {
					continue
				}
				entry, covering := group.entry, parent.entry
				entry.Services, covering.Services = []string{serviceID}, owners
				if !appendDetail(RuleOverlap{Kind: "covered", Entry: entry, Covering: &covering}) {
					return result
				}
			}
		}
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
