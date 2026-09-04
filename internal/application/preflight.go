package application

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/planner"
)

// RuleLimitError keeps the only safe details an operator can act on while
// still matching ErrRuleLimit for existing callers.
type RuleLimitError struct {
	Projected int
	Maximum   int
}

func (e *RuleLimitError) Error() string {
	return fmt.Sprintf("projected rule count %d exceeds %d", e.Projected, e.Maximum)
}

func (e *RuleLimitError) Unwrap() error { return ErrRuleLimit }

// PreflightPlan is deliberately independent from Render. It refuses malformed,
// stale, incompatible, non-canonical, or hash-injected plans before any format
// implementation sees them.
func PreflightPlan(plan domain.RoutingPlan, target domain.TargetProfile, renderer Renderer, cutoff time.Time) error {
	if renderer == nil || cutoff.IsZero() || plan.InterfaceVersion != domain.RoutingPlanInterfaceVersion || plan.PolicyVersion != planner.PolicyVersion || plan.TargetID != target.ID || plan.ProfileKey != target.ProfileKey || plan.ObservationCutoff.IsZero() || !plan.ObservationCutoff.Equal(cutoff.UTC()) || domain.ValidateSlug(target.ID) != nil || domain.ValidateSlug(target.ProfileKey) != nil || domain.ValidateSlug(target.RendererID) != nil || renderer.ID() != target.RendererID || renderer.Version() != target.ProfileKey {
		return fmt.Errorf("%w: identity", ErrPreflight)
	}
	if len(plan.Services) == 0 {
		return fmt.Errorf("%w: service set", ErrPreflight)
	}
	for i, serviceID := range plan.Services {
		if domain.ValidateSlug(serviceID) != nil || (i > 0 && plan.Services[i-1] >= serviceID) {
			return fmt.Errorf("%w: service set", ErrPreflight)
		}
	}
	supported := make(map[domain.RuleKind]struct{})
	for _, kind := range renderer.SupportedRuleKinds() {
		if !knownRuleKind(kind) {
			return fmt.Errorf("%w: renderer kinds", ErrPreflight)
		}
		if _, duplicate := supported[kind]; duplicate {
			return fmt.Errorf("%w: renderer kinds", ErrPreflight)
		}
		supported[kind] = struct{}{}
	}
	if len(supported) == 0 {
		return fmt.Errorf("%w: renderer kinds", ErrPreflight)
	}
	seenRules := make(map[string]struct{}, len(plan.Rules))
	lastRuleKey := ""
	for i, rule := range plan.Rules {
		if !rule.IsValid() || rule.Action != domain.ActionRoute || !slices.Contains(plan.Services, rule.ServiceID) || !targetSupportsRule(target.Constraints, rule.Kind) {
			return fmt.Errorf("%w: invalid or target-incompatible rule", ErrPreflight)
		}
		if _, ok := supported[rule.Kind]; !ok {
			return fmt.Errorf("%w: renderer-incompatible rule", ErrPreflight)
		}
		if rule.ExpiresAt != nil && !rule.ExpiresAt.After(cutoff) {
			return fmt.Errorf("%w: stale rule", ErrPreflight)
		}
		if !canonicalStrings(rule.Labels) || !canonicalStrings(rule.ReasonCodes) || !canonicalStrings(rule.ProvenanceRefs) {
			return fmt.Errorf("%w: non-canonical rule metadata", ErrPreflight)
		}
		key := ruleSortKey(rule)
		if i > 0 && lastRuleKey >= key {
			return fmt.Errorf("%w: rule order", ErrPreflight)
		}
		lastRuleKey = key
		identity := rule.CandidateKey()
		if _, duplicate := seenRules[identity]; duplicate {
			return fmt.Errorf("%w: duplicate rule", ErrPreflight)
		}
		seenRules[identity] = struct{}{}
	}
	seenCoverage := make(map[string]struct{}, len(plan.Coverage))
	for _, coverage := range plan.Coverage {
		key := coverage.ServiceID + "\x00" + coverage.ComponentID
		if domain.ValidateSlug(coverage.ServiceID) != nil || domain.ValidateSlug(coverage.ComponentID) != nil || coverage.RuleCount < 0 {
			return fmt.Errorf("%w: invalid coverage", ErrPreflight)
		}
		if _, duplicate := seenCoverage[key]; duplicate {
			return fmt.Errorf("%w: duplicate coverage", ErrPreflight)
		}
		seenCoverage[key] = struct{}{}
		if !coverage.Complete {
			return fmt.Errorf("%w: incomplete coverage", ErrPartialCoverage)
		}
	}
	projectedRuleCount, err := renderer.ProjectedRuleCount(cloneRoutingPlan(plan))
	if err != nil || projectedRuleCount < 0 {
		return fmt.Errorf("%w: invalid renderer projection", ErrPreflight)
	}
	if target.Constraints.MaxRules > 0 && projectedRuleCount > target.Constraints.MaxRules {
		return fmt.Errorf("%w: %w", ErrPreflight, &RuleLimitError{Projected: projectedRuleCount, Maximum: target.Constraints.MaxRules})
	}
	if plan.SemanticHash == "" || plan.SemanticHash != planner.SemanticHash(plan, target) {
		return fmt.Errorf("%w: semantic hash", ErrPreflight)
	}
	return nil
}

func targetSupportsRule(constraints domain.TargetConstraints, kind domain.RuleKind) bool {
	switch kind {
	case domain.RuleDomainExact:
		return constraints.SupportsDomainExact
	case domain.RuleDomainSuffix:
		return constraints.SupportsDomainSuffix
	case domain.RuleIPv4:
		return constraints.SupportsIPv4
	case domain.RuleIPv6:
		return constraints.SupportsIPv6
	case domain.RulePrefix4:
		return constraints.SupportsPrefixes && constraints.SupportsIPv4
	case domain.RulePrefix6:
		return constraints.SupportsPrefixes && constraints.SupportsIPv6
	default:
		return false
	}
}

func knownRuleKind(kind domain.RuleKind) bool {
	switch kind {
	case domain.RuleDomainExact, domain.RuleDomainSuffix, domain.RuleIPv4, domain.RuleIPv6, domain.RulePrefix4, domain.RulePrefix6:
		return true
	default:
		return false
	}
}

func ruleSortKey(rule domain.RouteRule) string {
	return strings.Join([]string{string(rule.Kind), rule.CanonicalValue(), rule.ServiceID, rule.ComponentID, string(rule.SourceClass)}, "\x00")
}

func canonicalStrings(values []string) bool {
	stable := domain.StableStrings(values)
	return equalStrings(values, stable)
}
