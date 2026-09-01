// Package planjson owns the canonical, renderer-neutral RoutingPlan JSON
// representation. The raw-json renderer delegates to this package so the
// artifact bytes and the snapshot bytes cannot drift.
package planjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

const MaxBytes = 4 << 20

type Rule struct {
	Kind           string   `json:"kind"`
	Value          string   `json:"value"`
	Action         string   `json:"action"`
	ServiceID      string   `json:"service_id"`
	ComponentID    string   `json:"component_id"`
	ExpiresAt      *string  `json:"expires_at"`
	SourceClass    string   `json:"source_class"`
	ReasonCodes    []string `json:"reason_codes"`
	ProvenanceRefs []string `json:"provenance_refs"`
}
type Excluded struct {
	Candidate      Rule     `json:"candidate"`
	Outcome        string   `json:"outcome"`
	ReasonCodes    []string `json:"reason_codes"`
	ProvenanceRefs []string `json:"provenance_refs"`
}
type Coverage struct {
	ServiceID   string `json:"service_id"`
	ComponentID string `json:"component_id"`
	Complete    bool   `json:"complete"`
	RuleCount   int    `json:"rule_count"`
}
type Resource struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
type Relation struct {
	Source         Resource `json:"source"`
	RelationType   string   `json:"relation_type"`
	Target         Resource `json:"target"`
	ServiceID      string   `json:"service_id"`
	ComponentID    string   `json:"component_id"`
	FirstSeen      string   `json:"first_seen"`
	LastSeen       string   `json:"last_seen"`
	ValidUntil     string   `json:"valid_until"`
	SourceID       string   `json:"source_id"`
	SourceRevision string   `json:"source_revision"`
	Validity       string   `json:"validity"`
}
type Sighting struct {
	ID                    string   `json:"id"`
	ServiceID             string   `json:"service_id"`
	ComponentID           string   `json:"component_id"`
	Resource              Resource `json:"resource"`
	SourceID              string   `json:"source_id"`
	SourceClass           string   `json:"source_class"`
	SourceRevision        string   `json:"source_revision"`
	FirstSeen             string   `json:"first_seen"`
	LastSeen              string   `json:"last_seen"`
	ValidUntil            string   `json:"valid_until"`
	TTLSeconds            int64    `json:"ttl_seconds"`
	TTLKnown              bool     `json:"ttl_known"`
	ObservationCount      int      `json:"observation_count"`
	Validity              string   `json:"validity"`
	SharedNetworkEvidence string   `json:"shared_network_evidence"`
}
type Plan struct {
	InterfaceVersion  string     `json:"interface_version"`
	TargetID          string     `json:"target_id"`
	ProfileKey        string     `json:"profile_key"`
	Services          []string   `json:"services"`
	Rules             []Rule     `json:"rules"`
	Excluded          []Excluded `json:"excluded"`
	Warnings          []string   `json:"warnings"`
	Coverage          []Coverage `json:"coverage"`
	Relations         []Relation `json:"relations"`
	Sightings         []Sighting `json:"sightings"`
	PolicyVersion     string     `json:"policy_version"`
	CatalogRevision   string     `json:"catalog_revision"`
	ObservationCutoff string     `json:"observation_cutoff"`
	SemanticHash      string     `json:"semantic_hash"`
}

func Encode(plan domain.RoutingPlan) ([]byte, error) {
	dto, err := Convert(plan)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(dto)
	if err != nil {
		return nil, fmt.Errorf("marshal routing plan JSON: %w", err)
	}
	return append(encoded, '\n'), nil
}

func Validate(encoded []byte) error {
	if len(encoded) == 0 || len(encoded) > MaxBytes {
		return fmt.Errorf("routing plan JSON size is outside the supported bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var plan Plan
	if err := decoder.Decode(&plan); err != nil {
		return fmt.Errorf("decode routing plan JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("routing plan JSON must contain one document")
	}
	if plan.InterfaceVersion != domain.RoutingPlanInterfaceVersion || plan.TargetID == "" || plan.ProfileKey == "" || plan.PolicyVersion == "" || plan.CatalogRevision == "" || plan.ObservationCutoff == "" || plan.SemanticHash == "" {
		return fmt.Errorf("routing plan JSON identity is incomplete")
	}
	return nil
}

func Convert(plan domain.RoutingPlan) (Plan, error) {
	if plan.InterfaceVersion != domain.RoutingPlanInterfaceVersion {
		return Plan{}, fmt.Errorf("unsupported routing plan interface version %q", plan.InterfaceVersion)
	}
	if plan.TargetID == "" || plan.ProfileKey == "" {
		return Plan{}, fmt.Errorf("routing plan target identity is incomplete")
	}
	dto := Plan{InterfaceVersion: plan.InterfaceVersion, TargetID: plan.TargetID, ProfileKey: plan.ProfileKey, Services: stringsOrEmpty(plan.Services), Rules: make([]Rule, 0, len(plan.Rules)), Excluded: make([]Excluded, 0, len(plan.Excluded)), Warnings: stringsOrEmpty(plan.Warnings), Coverage: make([]Coverage, 0, len(plan.Coverage)), Relations: make([]Relation, 0, len(plan.Relations)), Sightings: make([]Sighting, 0, len(plan.Sightings)), PolicyVersion: plan.PolicyVersion, CatalogRevision: plan.CatalogRevision, ObservationCutoff: formatTime(plan.ObservationCutoff), SemanticHash: plan.SemanticHash}
	for _, rule := range plan.Rules {
		if !rule.IsValid() {
			return Plan{}, fmt.Errorf("routing plan contains invalid rule %q", rule.CanonicalValue())
		}
		dto.Rules = append(dto.Rules, convertRule(rule))
	}
	for _, excluded := range plan.Excluded {
		dto.Excluded = append(dto.Excluded, Excluded{Candidate: convertRule(excluded.Candidate), Outcome: excluded.Outcome, ReasonCodes: stringsOrEmpty(excluded.ReasonCodes), ProvenanceRefs: stringsOrEmpty(excluded.ProvenanceRefs)})
	}
	for _, coverage := range plan.Coverage {
		dto.Coverage = append(dto.Coverage, Coverage{ServiceID: coverage.ServiceID, ComponentID: coverage.ComponentID, Complete: coverage.Complete, RuleCount: coverage.RuleCount})
	}
	for _, relation := range plan.Relations {
		if !relation.SourceResource.IsValid() || !relation.TargetResource.IsValid() {
			return Plan{}, fmt.Errorf("routing plan contains invalid relation")
		}
		dto.Relations = append(dto.Relations, Relation{Source: convertResource(relation.SourceResource), RelationType: string(relation.RelationType), Target: convertResource(relation.TargetResource), ServiceID: relation.ServiceID, ComponentID: relation.ComponentID, FirstSeen: formatTime(relation.FirstSeen), LastSeen: formatTime(relation.LastSeen), ValidUntil: formatTime(relation.ValidUntil), SourceID: relation.SourceID, SourceRevision: relation.SourceRevision, Validity: string(relation.Validity)})
	}
	for _, sighting := range plan.Sightings {
		if !sighting.Resource.IsValid() {
			return Plan{}, fmt.Errorf("routing plan contains invalid sighting resource")
		}
		dto.Sightings = append(dto.Sightings, Sighting{ID: sighting.ID, ServiceID: sighting.ServiceID, ComponentID: sighting.ComponentID, Resource: convertResource(sighting.Resource), SourceID: sighting.SourceID, SourceClass: string(sighting.SourceClass), SourceRevision: sighting.SourceRevision, FirstSeen: formatTime(sighting.FirstSeen), LastSeen: formatTime(sighting.LastSeen), ValidUntil: formatTime(sighting.ValidUntil), TTLSeconds: sighting.TTLSeconds, TTLKnown: sighting.TTLKnown, ObservationCount: sighting.ObservationCount, Validity: string(sighting.Validity), SharedNetworkEvidence: string(sighting.SharedNetworkEvidence)})
	}
	return dto, nil
}

func convertRule(rule domain.RouteRule) Rule {
	var expires *string
	if rule.ExpiresAt != nil {
		value := formatTime(*rule.ExpiresAt)
		expires = &value
	}
	return Rule{Kind: string(rule.Kind), Value: rule.CanonicalValue(), Action: string(rule.Action), ServiceID: rule.ServiceID, ComponentID: rule.ComponentID, ExpiresAt: expires, SourceClass: string(rule.SourceClass), ReasonCodes: stringsOrEmpty(rule.ReasonCodes), ProvenanceRefs: stringsOrEmpty(rule.ProvenanceRefs)}
}
func convertResource(resource domain.Resource) Resource {
	return Resource{Kind: string(resource.Kind), Value: resource.CanonicalValue()}
}
func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
func stringsOrEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return append([]string{}, values...)
}
