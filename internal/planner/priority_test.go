package planner

import (
	"slices"
	"testing"

	"github.com/Muratovnik/routevane/internal/domain"
)

func TestServicePriorityAssignsAnEqualRuleToTheFirstList(t *testing.T) {
	plan := domain.RoutingPlan{Rules: []domain.RouteRule{
		overlapTestRule(t, domain.RuleIPv4, "192.0.2.1", "alpha"),
		overlapTestRule(t, domain.RuleIPv4, "192.0.2.1", "beta"),
	}}

	ApplyServicePriority(&plan, []string{"beta", "alpha"})

	if len(plan.Rules) != 1 || plan.Rules[0].ServiceID != "beta" {
		t.Fatalf("rules = %#v", plan.Rules)
	}
	if len(plan.Excluded) != 1 || plan.Excluded[0].Candidate.ServiceID != "alpha" || !slices.Equal(plan.Excluded[0].ReasonCodes, []string{ReasonLowerPriorityOverlap}) {
		t.Fatalf("excluded = %#v", plan.Excluded)
	}
}

func TestServicePriorityDropsAChildCoveredByAHigherList(t *testing.T) {
	plan := domain.RoutingPlan{Rules: []domain.RouteRule{
		overlapTestRule(t, domain.RulePrefix4, "192.0.2.0/24", "alpha"),
		overlapTestRule(t, domain.RuleIPv4, "192.0.2.1", "beta"),
	}}

	ApplyServicePriority(&plan, []string{"alpha", "beta"})

	if len(plan.Rules) != 1 || plan.Rules[0].ServiceID != "alpha" || plan.Rules[0].CanonicalValue() != "192.0.2.0/24" {
		t.Fatalf("rules = %#v", plan.Rules)
	}
}

func TestServicePriorityKeepsALowerBroadRuleWhenItHasUniqueAddresses(t *testing.T) {
	plan := domain.RoutingPlan{Rules: []domain.RouteRule{
		overlapTestRule(t, domain.RuleIPv4, "192.0.2.1", "alpha"),
		overlapTestRule(t, domain.RulePrefix4, "192.0.2.0/24", "beta"),
	}}

	ApplyServicePriority(&plan, []string{"alpha", "beta"})

	if len(plan.Rules) != 2 || len(plan.Excluded) != 0 {
		t.Fatalf("priority changed the requested routing union: rules=%#v excluded=%#v", plan.Rules, plan.Excluded)
	}
}
