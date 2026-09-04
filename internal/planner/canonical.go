package planner

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
)

func sortRules(rules []domain.RouteRule) {
	slices.SortFunc(rules, func(a, b domain.RouteRule) int {
		return cmp.Or(
			cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(a.CanonicalValue(), b.CanonicalValue()),
			cmp.Compare(a.ServiceID, b.ServiceID),
			cmp.Compare(a.ComponentID, b.ComponentID),
			cmp.Compare(a.SourceClass, b.SourceClass),
		)
	})
}

func canonicalSightings(input []domain.Sighting) []domain.Sighting {
	byKey := make(map[string]domain.Sighting, len(input))
	for _, sighting := range input {
		key := strings.Join([]string{sighting.ServiceID, sighting.ComponentID, sighting.Resource.Kind.String(), sighting.Resource.CanonicalValue(), sighting.SourceID, sighting.SourceRevision}, "\x00")
		if prior, ok := byKey[key]; ok {
			if prior.FirstSeen.IsZero() || (!sighting.FirstSeen.IsZero() && sighting.FirstSeen.Before(prior.FirstSeen)) {
				prior.FirstSeen = sighting.FirstSeen
			}
			if sighting.LastSeen.After(prior.LastSeen) {
				prior.LastSeen = sighting.LastSeen
			}
			if sighting.ValidUntil.After(prior.ValidUntil) {
				prior.ValidUntil = sighting.ValidUntil
			}
			// A planner input may contain the same persisted observation more than
			// once due to acquisition ordering.  Duplicates are one fact, so use
			// the deterministic maximum rather than making JSON depend on count.
			if sighting.ObservationCount > prior.ObservationCount {
				prior.ObservationCount = sighting.ObservationCount
			}
			if prior.Validity == "" || sighting.Validity == domain.ValidityValid {
				prior.Validity = sighting.Validity
			}
			if sighting.SharedNetworkEvidence == domain.SharedNetworkEvidenceTrusted {
				prior.SharedNetworkEvidence = domain.SharedNetworkEvidenceTrusted
			}
			byKey[key] = prior
			continue
		}
		sighting.FirstSeen = sighting.FirstSeen.UTC()
		sighting.LastSeen = sighting.LastSeen.UTC()
		sighting.ValidUntil = sighting.ValidUntil.UTC()
		byKey[key] = sighting
	}
	out := make([]domain.Sighting, 0, len(byKey))
	for _, sighting := range byKey {
		// IDs are internal persistence details.  Expose the stable observation
		// fingerprint in diagnostic JSON so acquisition-generated IDs cannot
		// perturb canonical output.
		sighting.ID = sighting.Fingerprint()
		out = append(out, sighting)
	}
	slices.SortFunc(out, func(a, b domain.Sighting) int {
		ak := strings.Join([]string{a.ServiceID, a.ComponentID, a.Resource.Kind.String(), a.Resource.CanonicalValue(), a.SourceID, a.SourceRevision}, "\x00")
		bk := strings.Join([]string{b.ServiceID, b.ComponentID, b.Resource.Kind.String(), b.Resource.CanonicalValue(), b.SourceID, b.SourceRevision}, "\x00")
		return cmp.Compare(ak, bk)
	})
	return out
}

func canonicalRelations(input []domain.Relation) []domain.Relation {
	byKey := make(map[string]domain.Relation, len(input))
	for _, relation := range input {
		key := relation.Fingerprint()
		if prior, ok := byKey[key]; ok {
			if prior.FirstSeen.IsZero() || (!relation.FirstSeen.IsZero() && relation.FirstSeen.Before(prior.FirstSeen)) {
				prior.FirstSeen = relation.FirstSeen
			}
			if relation.LastSeen.After(prior.LastSeen) {
				prior.LastSeen = relation.LastSeen
			}
			if relation.ValidUntil.After(prior.ValidUntil) {
				prior.ValidUntil = relation.ValidUntil
			}
			byKey[key] = prior
			continue
		}
		relation.FirstSeen = relation.FirstSeen.UTC()
		relation.LastSeen = relation.LastSeen.UTC()
		relation.ValidUntil = relation.ValidUntil.UTC()
		byKey[key] = relation
	}
	out := make([]domain.Relation, 0, len(byKey))
	for _, relation := range byKey {
		out = append(out, relation)
	}
	slices.SortFunc(out, func(a, b domain.Relation) int { return cmp.Compare(a.Fingerprint(), b.Fingerprint()) })
	return out
}

// CanonicalizePlan normalizes every order-bearing field.  It is exported so
// renderers and command-level tests can re-canonicalize an explicit plan.
func CanonicalizePlan(plan *domain.RoutingPlan) {
	if plan == nil {
		return
	}
	plan.ObservationCutoff = plan.ObservationCutoff.UTC()
	plan.Services = domain.StableStrings(plan.Services)
	for i := range plan.Rules {
		plan.Rules[i].Labels = domain.StableStrings(plan.Rules[i].Labels)
		plan.Rules[i].ReasonCodes = domain.StableStrings(plan.Rules[i].ReasonCodes)
		plan.Rules[i].ProvenanceRefs = domain.StableStrings(plan.Rules[i].ProvenanceRefs)
		if plan.Rules[i].ExpiresAt != nil {
			expires := plan.Rules[i].ExpiresAt.UTC()
			plan.Rules[i].ExpiresAt = &expires
		}
	}
	sortRules(plan.Rules)
	for i := range plan.Excluded {
		plan.Excluded[i].ReasonCodes = domain.StableStrings(plan.Excluded[i].ReasonCodes)
		plan.Excluded[i].ProvenanceRefs = domain.StableStrings(plan.Excluded[i].ProvenanceRefs)
	}
	slices.SortFunc(plan.Excluded, func(a, b domain.Excluded) int {
		ak := a.Candidate.CandidateKey() + "\x00" + a.Outcome + "\x00" + strings.Join(a.ReasonCodes, ",")
		bk := b.Candidate.CandidateKey() + "\x00" + b.Outcome + "\x00" + strings.Join(b.ReasonCodes, ",")
		return cmp.Compare(ak, bk)
	})
	plan.Warnings = domain.StableStrings(plan.Warnings)
	slices.SortFunc(plan.Coverage, func(a, b domain.Coverage) int {
		return cmp.Or(cmp.Compare(a.ServiceID, b.ServiceID), cmp.Compare(a.ComponentID, b.ComponentID))
	})
	plan.Sightings = canonicalSightings(plan.Sightings)
	plan.Relations = canonicalRelations(plan.Relations)
}

type hashRule struct {
	Kind        string   `json:"kind"`
	Value       string   `json:"value"`
	Action      string   `json:"action"`
	ServiceID   string   `json:"service_id"`
	ComponentID string   `json:"component_id"`
	SourceClass string   `json:"source_class"`
	Labels      []string `json:"labels,omitempty"`
	Reasons     []string `json:"reason_codes"`
	Provenance  []string `json:"provenance_refs"`
}

type hashTarget struct {
	SupportsDomainExact   bool `json:"supports_domain_exact"`
	SupportsDomainSuffix  bool `json:"supports_domain_suffix"`
	SupportsDynamicDNSSet bool `json:"supports_dynamic_dns_set"`
	SupportsIPv4          bool `json:"supports_ipv4"`
	SupportsIPv6          bool `json:"supports_ipv6"`
	SupportsPrefixes      bool `json:"supports_prefixes"`
	MaxRules              int  `json:"max_rules"`
	MaxArtifactSize       int  `json:"max_artifact_size"`
}

type hashPayload struct {
	InterfaceVersion     string     `json:"interface_version"`
	TargetID             string     `json:"target_id"`
	ProfileKey           string     `json:"profile_key"`
	Target               hashTarget `json:"target"`
	PolicyVersion        string     `json:"policy_version"`
	CatalogRevision      string     `json:"catalog_revision"`
	Rules                []hashRule `json:"rules"`
	SightingFingerprints []string   `json:"sighting_fingerprints"`
	RelationFingerprints []string   `json:"relation_fingerprints"`
}

func SemanticHash(plan domain.RoutingPlan, target domain.TargetProfile) string {
	plan = cloneRoutingPlan(plan)
	CanonicalizePlan(&plan)
	payload := hashPayload{InterfaceVersion: plan.InterfaceVersion, TargetID: plan.TargetID, ProfileKey: plan.ProfileKey, PolicyVersion: plan.PolicyVersion, CatalogRevision: plan.CatalogRevision,
		Target: hashTarget{target.Constraints.SupportsDomainExact, target.Constraints.SupportsDomainSuffix, target.Constraints.SupportsDynamicDNSSet, target.Constraints.SupportsIPv4, target.Constraints.SupportsIPv6, target.Constraints.SupportsPrefixes, target.Constraints.MaxRules, target.Constraints.MaxArtifactSize}}
	for _, rule := range plan.Rules {
		payload.Rules = append(payload.Rules, hashRule{string(rule.Kind), rule.CanonicalValue(), string(rule.Action), rule.ServiceID, rule.ComponentID, string(rule.SourceClass), domain.StableStrings(rule.Labels), domain.StableStrings(rule.ReasonCodes), domain.StableStrings(rule.ProvenanceRefs)})
	}
	for _, sighting := range plan.Sightings {
		payload.SightingFingerprints = append(payload.SightingFingerprints, sighting.Fingerprint())
	}
	for _, relation := range plan.Relations {
		payload.RelationFingerprints = append(payload.RelationFingerprints, relation.Fingerprint())
	}
	slices.Sort(payload.SightingFingerprints)
	slices.Sort(payload.RelationFingerprints)
	encoded, _ := json.Marshal(payload)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func cloneRoutingPlan(plan domain.RoutingPlan) domain.RoutingPlan {
	clone := plan
	clone.Services = append([]string(nil), plan.Services...)
	clone.Rules = append([]domain.RouteRule(nil), plan.Rules...)
	for i := range clone.Rules {
		clone.Rules[i].Labels = append([]string(nil), plan.Rules[i].Labels...)
		clone.Rules[i].ReasonCodes = append([]string(nil), plan.Rules[i].ReasonCodes...)
		clone.Rules[i].ProvenanceRefs = append([]string(nil), plan.Rules[i].ProvenanceRefs...)
		if plan.Rules[i].ExpiresAt != nil {
			expires := *plan.Rules[i].ExpiresAt
			clone.Rules[i].ExpiresAt = &expires
		}
	}
	clone.Excluded = append([]domain.Excluded(nil), plan.Excluded...)
	for i := range clone.Excluded {
		clone.Excluded[i].Candidate.ReasonCodes = append([]string(nil), plan.Excluded[i].Candidate.ReasonCodes...)
		clone.Excluded[i].Candidate.ProvenanceRefs = append([]string(nil), plan.Excluded[i].Candidate.ProvenanceRefs...)
		clone.Excluded[i].ReasonCodes = append([]string(nil), plan.Excluded[i].ReasonCodes...)
		clone.Excluded[i].ProvenanceRefs = append([]string(nil), plan.Excluded[i].ProvenanceRefs...)
	}
	clone.Warnings = append([]string(nil), plan.Warnings...)
	clone.Coverage = append([]domain.Coverage(nil), plan.Coverage...)
	clone.Relations = append([]domain.Relation(nil), plan.Relations...)
	clone.Sightings = append([]domain.Sighting(nil), plan.Sightings...)
	return clone
}
