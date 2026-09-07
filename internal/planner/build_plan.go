// Package planner implements the deterministic Auto v1 policy.  It does not
// perform I/O: callers provide a list definition, observations, target
// constraints and an explicit cutoff.
package planner

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/netpolicy"
)

var (
	ErrRuleLimitExceeded = errors.New("global routing rule limit exceeded")
	ErrRequiredCoverage  = errors.New("required list component has no safe route")
)

const (
	PolicyVersion                           = "auto-v1"
	M0CatalogRevision                       = "m0-example-v1"
	ReasonManualRule                        = "manual_rule"
	ReasonOfficialRule                      = "official_rule"
	ReasonFreshDNSObservation               = "fresh_dns_observation"
	ReasonFreshCommunityObservation         = "fresh_community_observation"
	ReasonStaleObservation                  = "stale_observation"
	ReasonInvalidResource                   = "invalid_resource"
	ReasonUnsupportedByTarget               = "unsupported_by_target"
	ReasonNotRequiredForDomainCapableTarget = "not_required_for_domain_capable_target"
	ReasonWideNetworkExpansion              = "wide_network_expansion"
	ReasonSharedCDNorCloud                  = "shared_cdn_or_cloud"
	ReasonCriticalDirectConflict            = "critical_direct_conflict"
	ReasonRuleLimitExceeded                 = "rule_limit_exceeded"
	ReasonOptionalComponentRemoved          = "optional_component_removed"
	ReasonLosslessCollapsed                 = "lossless_collapsed"
	ReasonSourceDegraded                    = "source_degraded"
	ReasonSpecialUseDestination             = "special_use_destination"
	ReasonPrefixTooWide                     = "prefix_too_wide"
)

// A routing rule is a destination the device will send traffic to. These bounds
// are what stops a hostile or misconfigured source from steering the operator's
// own network, or most of the internet, into the tunnel:
//
//   - a special-use address is never a routable destination, whoever declared
//     it. The classification is netpolicy's, deliberately: a destination policy
//     that two callers each reimplement is a policy that will diverge.
//   - a prefix wide enough to swallow a whole address space is refused even
//     when a source is entitled to declare its own networks. One observation
//     never proves ownership of that much of the internet.
const (
	minRoutablePrefixBitsV4 = 8
	minRoutablePrefixBitsV6 = 16
)

// admissionOrder returns the sightings to read, names first. It is a reading
// order for one pass, not a property of the plan: the caller keeps the canonical
// order for everything that is stored or hashed. Ties keep the canonical order,
// so the result is deterministic for a given canonical input.
func admissionOrder(sightings []domain.Sighting) []domain.Sighting {
	ordered := append([]domain.Sighting(nil), sightings...)
	namesFirst := func(sighting domain.Sighting) int {
		if sighting.Resource.Kind == domain.ResourceDomain {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(ordered, func(a, b domain.Sighting) int { return cmp.Compare(namesFirst(a), namesFirst(b)) })
	return ordered
}

// routableValue reports whether a rule's own value may become a route, and the
// reason code when it may not. It judges the value, never its provenance: a
// loopback address declared by an official source is still loopback.
func routableValue(rule domain.RouteRule) (string, bool) {
	switch {
	case rule.Kind.IsIP():
		if !netpolicy.RoutableDestination(rule.Addr) {
			return ReasonSpecialUseDestination, false
		}
	case rule.Kind.IsPrefix():
		prefix := rule.Prefix
		if !netpolicy.RoutableDestination(prefix.Addr()) {
			return ReasonSpecialUseDestination, false
		}
		minimum := minRoutablePrefixBitsV6
		if prefix.Addr().Unmap().Is4() {
			minimum = minRoutablePrefixBitsV4
		}
		if prefix.Bits() < minimum {
			return ReasonPrefixTooWide, false
		}
	}
	return "", true
}

// BuildPlan builds a plan from explicit inputs.  The optional relations
// argument keeps the primary four-argument API small while allowing the DNS
// source to pass terminal CNAME observations without a second planner type.
func BuildPlan(def domain.ListDefinition, sightings []domain.Sighting, target domain.TargetDefinition, cutoff time.Time, relations ...[]domain.Relation) (domain.RoutingPlan, error) {
	return buildPlan(ListInput{Definition: def, Sightings: sightings, Relations: firstRelations(relations)}, target, cutoff, len(relations) > 0)
}

func firstRelations(relations [][]domain.Relation) []domain.Relation {
	if len(relations) == 0 {
		return nil
	}
	return relations[0]
}

// buildPlan is the single policy implementation.  withRelations preserves the
// distinction between "no relations argument" and "an empty relation set", which
// callers rely on for plan shape.
func buildPlan(input ListInput, target domain.TargetDefinition, cutoff time.Time, withRelations bool) (domain.RoutingPlan, error) {
	def, sightings := input.Definition, input.Sightings
	if def.ID == "" {
		return domain.RoutingPlan{}, fmt.Errorf("list definition has no id")
	}
	if target.ID == "" {
		return domain.RoutingPlan{}, fmt.Errorf("target profile has no id")
	}
	if cutoff.IsZero() {
		return domain.RoutingPlan{}, fmt.Errorf("observation cutoff is zero")
	}
	cutoff = cutoff.UTC()

	plan := domain.RoutingPlan{
		InterfaceVersion: domain.RoutingPlanInterfaceVersion,
		TargetID:         target.ID, FormatKey: target.FormatKey,
		PolicyVersion:     PolicyVersion,
		CatalogRevision:   def.CatalogRevision,
		ObservationCutoff: cutoff,
	}
	if plan.FormatKey == "" {
		plan.FormatKey = target.ID
	}
	plan.Lists = []string{def.ID}
	plan.Sightings = canonicalSightings(sightings)
	if withRelations {
		plan.Relations = canonicalRelations(input.Relations)
	}
	grace := make(map[string]time.Time, len(input.Degraded))
	for _, degraded := range input.Degraded {
		if degraded.SourceID == "" || !degraded.GraceUntil.After(cutoff) {
			continue
		}
		grace[degraded.SourceID] = degraded.GraceUntil.UTC()
	}

	components := make(map[string]domain.ComponentDefinition, len(def.Components))
	for _, component := range def.Components {
		if component.ID != "" {
			components[component.ID] = component
		}
	}
	if len(components) == 0 {
		return domain.RoutingPlan{}, fmt.Errorf("list %q has no components", def.ID)
	}

	accepted := make([]domain.RouteRule, 0, len(def.Seeds)+len(sightings))
	excluded := make([]domain.Excluded, 0)
	for _, rawSeed := range def.Seeds {
		seed, err := rawSeed.Normalize()
		if err != nil {
			excluded = append(excluded, domain.Excluded{Outcome: "rejected", ReasonCodes: []string{ReasonInvalidResource}, ProvenanceRefs: []string{rawSeed.SourceID}})
			continue
		}
		if _, ok := components[seed.ComponentID]; !ok {
			continue
		}
		rule, err := seedRule(seed, def.ID)
		if err != nil {
			excluded = append(excluded, domain.Excluded{Outcome: "rejected", ReasonCodes: []string{ReasonInvalidResource}, ProvenanceRefs: []string{seed.SourceID}})
			continue
		}
		if reason, routable := routableValue(rule); !routable {
			excluded = append(excluded, domain.Excluded{Candidate: rule, Outcome: "rejected", ReasonCodes: []string{reason}, ProvenanceRefs: provenance(seed.SourceID)})
			continue
		}
		if !supportsRule(rule.Kind, target.Constraints) {
			excluded = append(excluded, domain.Excluded{Candidate: rule, Outcome: "rejected", ReasonCodes: []string{ReasonUnsupportedByTarget}, ProvenanceRefs: provenance(seed.SourceID)})
			continue
		}
		accepted = append(accepted, rule)
	}

	// domainSufficient is kept current as rules are accepted, not sampled once
	// before the sightings are read. A feed that reports a name and the
	// addresses it resolves to in one answer would otherwise have its addresses
	// kept: the exclusion below would consult a map that predates the name.
	domainSufficient := make(map[string]bool)
	noteDomainCoverage := func(rule domain.RouteRule) {
		if rule.Kind.IsDomain() {
			domainSufficient[rule.ComponentID] = true
		}
	}
	for _, rule := range accepted {
		noteDomainCoverage(rule)
	}
	// Names are read before addresses so that a name accepted in this pass
	// covers the addresses read after it, whatever order the source reported
	// them in. The plan's own canonical sighting order is left untouched: this
	// is an admission order, and it must not reach the semantic hash.
	ordered := admissionOrder(plan.Sightings)
	for _, sighting := range ordered {
		if sighting.ListID != "" && sighting.ListID != def.ID {
			continue
		}
		candidate, err := sightingRule(sighting, def.ID, components)
		if err != nil {
			excluded = append(excluded, domain.Excluded{Candidate: invalidCandidate(sighting, def.ID), Outcome: "rejected", ReasonCodes: []string{ReasonInvalidResource}, ProvenanceRefs: provenance(sighting.SourceID)})
			continue
		}
		if !sighting.IsFresh(cutoff) {
			// A source that cannot refresh must not silently drop routes inside
			// its grace window. Expiry alone is not evidence that an address
			// stopped belonging to the list.
			if !admissibleUnderGrace(sighting, grace) {
				excluded = append(excluded, domain.Excluded{Candidate: candidate, Outcome: "rejected", ReasonCodes: []string{ReasonStaleObservation}, ProvenanceRefs: provenance(sighting.SourceID)})
				continue
			}
			candidate.ReasonCodes = domain.StableStrings(append(candidate.ReasonCodes, ReasonSourceDegraded))
			// The rule now expires with the grace window, not with the
			// observation, so downstream staleness checks stay meaningful.
			graceUntil := grace[sighting.SourceID]
			candidate.ExpiresAt = &graceUntil
		}
		// A target that routes by name does not need the addresses that name
		// resolves to, nor a third party's aggregation of them: both are a
		// rendering of the same names, and carrying them spends the device's
		// budget twice for the same coverage.
		//
		// A range the operator publishes about itself is kept. It is not
		// derived from these names and may serve endpoints no name in the list
		// reaches, so dropping it would lose coverage nobody chose to lose.
		redundantUnderDomains := candidate.Kind.IsIP() ||
			(candidate.Kind.IsPrefix() && sighting.SourceClass != domain.SourceOfficial)
		if domainSufficient[candidate.ComponentID] && redundantUnderDomains {
			excluded = append(excluded, domain.Excluded{Candidate: candidate, Outcome: "rejected", ReasonCodes: []string{ReasonNotRequiredForDomainCapableTarget}, ProvenanceRefs: provenance(sighting.SourceID)})
			continue
		}
		if candidate.Kind.IsPrefix() && !declaredNetwork(sighting) {
			// A prefix observed from metadata is a policy candidate, never
			// proof that the whole network belongs to this list.  Keep it
			// visible as quarantined diagnostics without routing expansion.
			reasons := []string{ReasonWideNetworkExpansion}
			if sighting.SharedNetworkEvidence == domain.SharedNetworkEvidenceTrusted {
				reasons = append(reasons, ReasonSharedCDNorCloud)
			}
			excluded = append(excluded, domain.Excluded{Candidate: candidate, Outcome: "quarantined", ReasonCodes: reasons, ProvenanceRefs: provenance(sighting.SourceID)})
			continue
		}
		if reason, routable := routableValue(candidate); !routable {
			excluded = append(excluded, domain.Excluded{Candidate: candidate, Outcome: "rejected", ReasonCodes: []string{reason}, ProvenanceRefs: provenance(sighting.SourceID)})
			continue
		}
		if !supportsRule(candidate.Kind, target.Constraints) {
			excluded = append(excluded, domain.Excluded{Candidate: candidate, Outcome: "rejected", ReasonCodes: []string{ReasonUnsupportedByTarget}, ProvenanceRefs: provenance(sighting.SourceID)})
			continue
		}
		noteDomainCoverage(candidate)
		accepted = append(accepted, candidate)
	}

	accepted = dedupeRules(accepted)
	if target.Constraints.SupportsPrefixes {
		accepted = collapseObserved(accepted)
	}
	accepted = dedupeRules(accepted)

	// MaxRules is a target limit, not an optimizer.  Canonical ordering makes
	// the deterministic truncation and the corresponding exclusions explicit.
	if target.Constraints.MaxRules > 0 && len(accepted) > target.Constraints.MaxRules {
		sortRules(accepted)
		for _, candidate := range accepted[target.Constraints.MaxRules:] {
			excluded = append(excluded, domain.Excluded{Candidate: candidate, Outcome: "quarantined", ReasonCodes: []string{ReasonRuleLimitExceeded}, ProvenanceRefs: candidate.ProvenanceRefs})
		}
		accepted = accepted[:target.Constraints.MaxRules]
	}

	plan.Rules = accepted
	plan.Excluded = excluded
	plan.Warnings = buildWarnings(plan.Rules, components, def.ID, grace)
	plan.Coverage = buildCoverage(plan.Rules, components, def.ID)
	CanonicalizePlan(&plan)
	plan.SemanticHash = SemanticHash(plan, target)
	return plan, nil
}

func BuildPlanWithRelations(def domain.ListDefinition, sightings []domain.Sighting, relations []domain.Relation, target domain.TargetDefinition, cutoff time.Time) (domain.RoutingPlan, error) {
	return BuildPlan(def, sightings, target, cutoff, relations)
}

// ListInput is one list's coherent observation snapshot at the cutoff
// shared by BuildPlanSet. It keeps multi-list composition in policy code,
// rather than teaching a renderer about lists or persistence.
type ListInput struct {
	Definition domain.ListDefinition
	Sightings  []domain.Sighting
	Relations  []domain.Relation
	// Degraded lists the sources whose latest cycle failed while an earlier
	// success is still inside its grace window. The caller owns the window; the
	// planner owns what a window is allowed to admit.
	Degraded []DegradedSource
}

// DegradedSource is one source identity inside its grace window.
type DegradedSource struct {
	SourceID   string
	GraceUntil time.Time
}

// admissibleUnderGrace accepts only an expired-but-retained observation from a
// source that is inside its grace window. Invalid and archived observations are
// never revived, so grace cannot resurrect data the retention policy dropped.
func admissibleUnderGrace(sighting domain.Sighting, grace map[string]time.Time) bool {
	if len(grace) == 0 || sighting.Validity != domain.ValidityStale || !sighting.Resource.IsValid() {
		return false
	}
	_, degraded := grace[sighting.SourceID]
	return degraded
}

// declaredNetwork reports whether a prefix was published as a prefix rather
// than derived from something narrower. A vendor's own network list and a
// curated third-party list are both declarations; an observed address, an ASN
// or an RDAP record is an inference and stays quarantined. Who declared it is
// the source class, which travels into the diagnostics with the rule, and
// explicit shared-network evidence still quarantines the prefix because a
// shared CDN range is not list specific.
//
// Community was admitted on 2026-08-22 (ADR 0015): every third-party list this
// product adopts declares that class, and refusing it would have made honest
// declaration the reason the material could not be used.
func declaredNetwork(sighting domain.Sighting) bool {
	switch sighting.SourceClass {
	case domain.SourceOfficial, domain.SourceCommunity:
		return sighting.SharedNetworkEvidence != domain.SharedNetworkEvidenceTrusted
	default:
		return false
	}
}

// BuildPlanSet composes one canonical plan for a set of lists. Per-list
// reduction runs without MaxRules; the target rule limit is checked exactly
// once after composition. A limit or required-coverage error returns the full
// diagnostic plan and never truncates it.
func BuildPlanSet(inputs []ListInput, target domain.TargetDefinition, cutoff time.Time) (domain.RoutingPlan, error) {
	if len(inputs) == 0 {
		return domain.RoutingPlan{}, fmt.Errorf("list set is empty")
	}
	ordered := append([]ListInput(nil), inputs...)
	slices.SortFunc(ordered, func(a, b ListInput) int { return cmp.Compare(a.Definition.ID, b.Definition.ID) })
	for i := range ordered {
		if domain.ValidateSlug(ordered[i].Definition.ID) != nil {
			return domain.RoutingPlan{}, fmt.Errorf("invalid list identity")
		}
		if i > 0 && ordered[i-1].Definition.ID == ordered[i].Definition.ID {
			return domain.RoutingPlan{}, fmt.Errorf("duplicate list identity %q", ordered[i].Definition.ID)
		}
		if i > 0 && ordered[i-1].Definition.CatalogRevision != ordered[i].Definition.CatalogRevision {
			return domain.RoutingPlan{}, fmt.Errorf("list definitions are from different catalog revisions")
		}
	}

	unboundedTarget := target
	unboundedTarget.Constraints.MaxRules = 0
	combined := domain.RoutingPlan{
		InterfaceVersion:  domain.RoutingPlanInterfaceVersion,
		TargetID:          target.ID,
		FormatKey:         target.FormatKey,
		PolicyVersion:     PolicyVersion,
		CatalogRevision:   ordered[0].Definition.CatalogRevision,
		ObservationCutoff: cutoff.UTC(),
	}
	for _, input := range ordered {
		listPlan, err := buildPlan(input, unboundedTarget, cutoff, true)
		if err != nil {
			return domain.RoutingPlan{}, fmt.Errorf("build list %q: %w", input.Definition.ID, err)
		}
		combined.Lists = append(combined.Lists, listPlan.Lists...)
		combined.Rules = append(combined.Rules, listPlan.Rules...)
		combined.Excluded = append(combined.Excluded, listPlan.Excluded...)
		combined.Warnings = append(combined.Warnings, listPlan.Warnings...)
		combined.Coverage = append(combined.Coverage, listPlan.Coverage...)
		combined.Relations = append(combined.Relations, listPlan.Relations...)
		combined.Sightings = append(combined.Sightings, listPlan.Sightings...)
		if hasPartialCoverage(listPlan.Excluded) {
			combined.Warnings = append(combined.Warnings, "partial_coverage:unsupported_or_quarantined:"+input.Definition.ID)
		}
	}
	CanonicalizePlan(&combined)
	combined.SemanticHash = SemanticHash(combined, target)

	for _, coverage := range combined.Coverage {
		if !coverage.Complete {
			return combined, fmt.Errorf("%w: %s/%s", ErrRequiredCoverage, coverage.ListID, coverage.ComponentID)
		}
	}
	if target.Constraints.MaxRules > 0 && len(combined.Rules) > target.Constraints.MaxRules {
		combined.Warnings = domain.StableStrings(append(combined.Warnings, "partial_coverage:rule_limit"))
		return combined, fmt.Errorf("%w: have %d, maximum %d", ErrRuleLimitExceeded, len(combined.Rules), target.Constraints.MaxRules)
	}
	return combined, nil
}

func hasPartialCoverage(excluded []domain.Excluded) bool {
	for _, item := range excluded {
		for _, reason := range item.ReasonCodes {
			if reason == ReasonUnsupportedByTarget || reason == ReasonWideNetworkExpansion || reason == ReasonRuleLimitExceeded {
				return true
			}
		}
	}
	return false
}

func seedRule(seed domain.Seed, listID string) (domain.RouteRule, error) {
	reasons := []string{ReasonManualRule}
	if seed.SourceClass == domain.SourceOfficial {
		reasons = []string{ReasonOfficialRule}
	}
	provenanceRef := seed.SourceID
	if provenanceRef == "" {
		provenanceRef = "seed:" + seed.Value
	}
	switch seed.Kind {
	case domain.RuleDomainExact, domain.RuleDomainSuffix:
		return domain.NewDomainRule(seed.Kind, seed.Value, listID, seed.ComponentID, seed.SourceClass, reasons, provenance(provenanceRef))
	case domain.RuleIPv4, domain.RuleIPv6:
		a, err := domain.ParseAddr(seed.Value)
		if err != nil {
			return domain.RouteRule{}, err
		}
		return domain.NewAddrRule(a, listID, seed.ComponentID, seed.SourceClass, reasons, provenance(provenanceRef))
	case domain.RulePrefix4, domain.RulePrefix6:
		p, err := domain.ParsePrefix(seed.Value)
		if err != nil {
			return domain.RouteRule{}, err
		}
		return domain.NewPrefixRule(p, listID, seed.ComponentID, seed.SourceClass, reasons, provenance(provenanceRef))
	default:
		return domain.RouteRule{}, fmt.Errorf("unsupported seed kind %q", seed.Kind)
	}
}

func sightingRule(sighting domain.Sighting, listID string, components map[string]domain.ComponentDefinition) (domain.RouteRule, error) {
	componentID := sighting.ComponentID
	if componentID == "" {
		componentID = "web"
	}
	if _, ok := components[componentID]; !ok {
		return domain.RouteRule{}, fmt.Errorf("unknown component %q", componentID)
	}
	switch sighting.Resource.Kind {
	case domain.ResourceIP, domain.ResourcePrefix, domain.ResourceDomain:
	default:
		return domain.RouteRule{}, fmt.Errorf("only addresses, prefixes and domains are routable candidates")
	}
	if !sighting.Resource.IsValid() {
		return domain.RouteRule{}, domain.ErrInvalidAddr
	}
	reason := ReasonFreshDNSObservation
	switch sighting.SourceClass {
	case domain.SourceCommunity:
		reason = ReasonFreshCommunityObservation
	case domain.SourceOfficial:
		reason = ReasonOfficialRule
	}
	if sighting.SourceClass == "" {
		sighting.SourceClass = domain.SourceObserved
	}
	ref := sighting.SourceID
	if ref == "" {
		ref = sighting.Fingerprint()
	}
	var rule domain.RouteRule
	var err error
	switch sighting.Resource.Kind {
	case domain.ResourcePrefix:
		rule, err = domain.NewPrefixRule(sighting.Resource.Prefix, listID, componentID, sighting.SourceClass, []string{reason}, provenance(ref))
	case domain.ResourceDomain:
		// A feed publishes a name, and a target that understands names expands
		// its subdomains itself. Recording it as exact would drop every
		// subdomain the publisher meant to include.
		rule, err = domain.NewDomainRule(domain.RuleDomainSuffix, sighting.Resource.Domain, listID, componentID, sighting.SourceClass, []string{reason}, provenance(ref))
	default:
		rule, err = domain.NewAddrRule(sighting.Resource.Addr, listID, componentID, sighting.SourceClass, []string{reason}, provenance(ref))
	}
	if err != nil {
		return domain.RouteRule{}, err
	}
	if !sighting.ValidUntil.IsZero() {
		expires := sighting.ValidUntil.UTC()
		rule.ExpiresAt = &expires
	}
	return rule, nil
}

func invalidCandidate(sighting domain.Sighting, listID string) domain.RouteRule {
	kind := domain.RuleIPv6
	if sighting.Resource.Kind == domain.ResourceIP && sighting.Resource.Addr.Is4() {
		kind = domain.RuleIPv4
	}
	return domain.RouteRule{Kind: kind, Action: domain.ActionRoute, ListID: listID, ComponentID: sighting.ComponentID, SourceClass: sighting.SourceClass}
}

func supportsRule(kind domain.RuleKind, c domain.TargetConstraints) bool {
	switch kind {
	case domain.RuleDomainExact:
		return c.SupportsDomainExact
	case domain.RuleDomainSuffix:
		return c.SupportsDomainSuffix
	case domain.RuleIPv4:
		return c.SupportsIPv4
	case domain.RuleIPv6:
		return c.SupportsIPv6
	case domain.RulePrefix4:
		return c.SupportsPrefixes && c.SupportsIPv4
	case domain.RulePrefix6:
		return c.SupportsPrefixes && c.SupportsIPv6
	default:
		return false
	}
}

func provenance(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

// collapseObserved reduces the address and network rules of one list to the
// smallest set of disjoint prefixes covering exactly the same addresses. It
// runs over every source class, not only DNS observations: a feed publishes
// networks, and leaving those uncollapsed spent the target's rule budget on
// values that merge exactly.
//
// Grouping stays per list, component and class. Merging across lists
// would produce a route nobody could attribute, and the diagnostics state which
// list and which kind of source each rule came from.
func collapseObserved(rules []domain.RouteRule) []domain.RouteRule {
	groups := make(map[string][]domain.RouteRule)
	others := make([]domain.RouteRule, 0, len(rules))
	for _, rule := range rules {
		if rule.Kind.IsIP() || rule.Kind.IsPrefix() {
			key := rule.ListID + "\x00" + rule.ComponentID + "\x00" + string(rule.SourceClass)
			groups[key] = append(groups[key], rule)
		} else {
			others = append(others, rule)
		}
	}
	for key, group := range groups {
		inputs := make([]netip.Prefix, 0, len(group))
		for _, rule := range group {
			if rule.Kind.IsPrefix() {
				inputs = append(inputs, rule.Prefix)
				continue
			}
			addr := rule.Addr.Unmap()
			inputs = append(inputs, netip.PrefixFrom(addr, addr.BitLen()))
		}
		prefixes := CollapseLosslessPrefixes(inputs)
		listID, componentID, sourceClass := splitGroupKey(key)
		for _, prefix := range prefixes {
			// A collapse is lossless only if the reason codes and expiry of the
			// merged rules survive it. Rebuilding from a fixed reason would drop
			// provenance such as source_degraded and silently change policy.
			refs := make([]string, 0)
			reasons := make([]string, 0)
			var expires *time.Time
			for _, rule := range group {
				if !covers(prefix, rule) {
					continue
				}
				refs = append(refs, rule.ProvenanceRefs...)
				reasons = append(reasons, rule.ReasonCodes...)
				if rule.ExpiresAt != nil && (expires == nil || rule.ExpiresAt.After(*expires)) {
					value := *rule.ExpiresAt
					expires = &value
				}
			}
			refs = domain.StableStrings(refs)
			reasons = domain.StableStrings(reasons)
			if len(reasons) == 0 {
				reasons = []string{ReasonFreshDNSObservation}
			}
			network := prefix.Bits() != prefix.Addr().BitLen()
			// A merge earns its reason code only when it actually merged
			// something: a feed's own /24 arrived as a network already.
			if network && len(group) > 1 {
				reasons = domain.StableStrings(append(reasons, ReasonLosslessCollapsed))
			}
			var rule domain.RouteRule
			var err error
			if network {
				rule, err = domain.NewPrefixRule(prefix, listID, componentID, sourceClass, reasons, refs)
			} else {
				rule, err = domain.NewAddrRule(prefix.Addr(), listID, componentID, sourceClass, reasons, refs)
			}
			if err == nil {
				rule.ExpiresAt = expires
				others = append(others, rule)
			}
		}
	}
	return others
}

// covers answers whether the collapsed prefix represents this rule, so the
// rule's provenance, reasons and expiry are carried onto it.
func covers(prefix netip.Prefix, rule domain.RouteRule) bool {
	if rule.Kind.IsPrefix() {
		return prefix.Bits() <= rule.Prefix.Bits() && prefix.Contains(rule.Prefix.Addr().Unmap())
	}
	return prefix.Contains(rule.Addr.Unmap())
}

func splitGroupKey(key string) (string, string, domain.SourceClass) {
	parts := strings.SplitN(key, "\x00", 3)
	if len(parts) == 3 {
		return parts[0], parts[1], domain.SourceClass(parts[2])
	}
	return key, "web", domain.SourceObserved
}

func dedupeRules(input []domain.RouteRule) []domain.RouteRule {
	byKey := make(map[string]domain.RouteRule, len(input))
	for _, rule := range input {
		key := rule.CandidateKey()
		if prior, ok := byKey[key]; ok {
			prior.ReasonCodes = domain.StableStrings(append(prior.ReasonCodes, rule.ReasonCodes...))
			prior.ProvenanceRefs = domain.StableStrings(append(prior.ProvenanceRefs, rule.ProvenanceRefs...))
			if prior.ExpiresAt == nil || (rule.ExpiresAt != nil && rule.ExpiresAt.After(*prior.ExpiresAt)) {
				prior.ExpiresAt = rule.ExpiresAt
			}
			byKey[key] = prior
			continue
		}
		rule.ReasonCodes = domain.StableStrings(rule.ReasonCodes)
		rule.ProvenanceRefs = domain.StableStrings(rule.ProvenanceRefs)
		byKey[key] = rule
	}
	out := make([]domain.RouteRule, 0, len(byKey))
	for _, rule := range byKey {
		out = append(out, rule)
	}
	sortRules(out)
	return out
}

func buildWarnings(rules []domain.RouteRule, components map[string]domain.ComponentDefinition, listID string, grace map[string]time.Time) []string {
	covered := make(map[string]bool)
	for _, rule := range rules {
		if rule.ListID == listID {
			covered[rule.ComponentID] = true
		}
	}
	warnings := make([]string, 0)
	for componentID, component := range components {
		if component.Required && !covered[componentID] {
			warnings = append(warnings, "required_component_without_rule:"+componentID)
		}
	}
	for sourceID := range grace {
		warnings = append(warnings, ReasonSourceDegraded+":"+listID+":"+sourceID)
	}
	slices.Sort(warnings)
	return warnings
}

func buildCoverage(rules []domain.RouteRule, components map[string]domain.ComponentDefinition, listID string) []domain.Coverage {
	counts := make(map[string]int)
	for _, rule := range rules {
		if rule.ListID == listID {
			counts[rule.ComponentID]++
		}
	}
	ids := slices.Sorted(maps.Keys(components))
	out := make([]domain.Coverage, 0, len(ids))
	for _, id := range ids {
		out = append(out, domain.Coverage{ListID: listID, ComponentID: id, Complete: counts[id] > 0 || !components[id].Required, RuleCount: counts[id]})
	}
	return out
}
