package planner

import (
	"cmp"
	"slices"

	"github.com/Muratovnik/routevane/internal/domain"
)

// ReasonLowerPriorityOverlap records a safe omission: the destination remains
// routed by a rule owned by a list that the operator placed higher.
const ReasonLowerPriorityOverlap = "lower_priority_overlap"

// ApplyListPriority assigns equal destinations to the first list and
// removes a lower-priority rule when a higher-priority rule wholly covers it.
// A broader lower-priority rule remains because deleting it would lose the
// addresses that only it contributes. The operation therefore changes
// ownership without changing the requested routing union.
func ApplyListPriority(plan *domain.RoutingPlan, priority []string) {
	if plan == nil || len(plan.Rules) < 2 {
		return
	}
	rank := make(map[string]int, len(priority))
	for index, id := range priority {
		if _, exists := rank[id]; !exists {
			rank[id] = index
		}
	}
	position := func(id string) int {
		if value, ok := rank[id]; ok {
			return value
		}
		return len(priority)
	}

	groups := make(map[string]*overlapGroup)
	widths := map[domain.RuleKind][]int{}
	for _, rule := range plan.Rules {
		if !rule.IsValid() {
			continue
		}
		key := overlapKey(rule.Kind, rule.CanonicalValue())
		group := groups[key]
		if group == nil {
			group = &overlapGroup{rule: rule, entry: OverlapValue{RuleKind: rule.Kind, Value: rule.CanonicalValue()}}
			groups[key] = group
		}
		group.entry.Lists = append(group.entry.Lists, rule.ListID)
		if rule.Kind.IsPrefix() {
			widths[rule.Kind] = append(widths[rule.Kind], rule.Prefix.Bits())
		}
	}
	for kind, bits := range widths {
		slices.Sort(bits)
		bits = slices.Compact(bits)
		slices.Reverse(bits)
		widths[kind] = bits
	}

	drop := make(map[string]struct{})
	dropKey := func(kind domain.RuleKind, value, listID string) string {
		return overlapKey(kind, value) + "\x00" + listID
	}
	better := func(a, b string) bool {
		return cmp.Or(cmp.Compare(position(a), position(b)), cmp.Compare(a, b)) < 0
	}

	for _, group := range groups {
		owners := domain.StableStrings(group.entry.Lists)
		if len(owners) > 1 {
			winner := owners[0]
			for _, owner := range owners[1:] {
				if better(owner, winner) {
					winner = owner
				}
			}
			for _, owner := range owners {
				if owner != winner {
					drop[dropKey(group.rule.Kind, group.rule.CanonicalValue(), owner)] = struct{}{}
				}
			}
		}
		for _, parentKey := range overlapParents(group.rule, widths) {
			parent := groups[parentKey]
			if parent == nil {
				continue
			}
			for _, owner := range owners {
				for _, coveringOwner := range domain.StableStrings(parent.entry.Lists) {
					if owner != coveringOwner && better(coveringOwner, owner) {
						drop[dropKey(group.rule.Kind, group.rule.CanonicalValue(), owner)] = struct{}{}
						break
					}
				}
			}
		}
	}

	if len(drop) == 0 {
		return
	}
	kept := make([]domain.RouteRule, 0, len(plan.Rules)-len(drop))
	for _, rule := range plan.Rules {
		if _, remove := drop[dropKey(rule.Kind, rule.CanonicalValue(), rule.ListID)]; !remove {
			kept = append(kept, rule)
			continue
		}
		plan.Excluded = append(plan.Excluded, domain.Excluded{
			Candidate: rule, Outcome: "rejected",
			ReasonCodes:    []string{ReasonLowerPriorityOverlap},
			ProvenanceRefs: append([]string(nil), rule.ProvenanceRefs...),
		})
	}
	plan.Rules = kept
}
