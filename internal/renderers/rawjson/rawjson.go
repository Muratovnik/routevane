// Package rawjson renders a canonical RoutingPlan as diagnostic JSON.  It
// deliberately contains no planner policy: all values are copied from the
// already-decided plan and canonicalized typed values are formatted here.
package rawjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/planjson"
)

const (
	ID              = "raw-json"
	Version         = "raw-v1"
	MaxArtifactSize = planjson.MaxBytes
	ContentType     = "application/json"
	FileExtension   = "json"
)

type Renderer struct{}

func (Renderer) ID() string      { return ID }
func (Renderer) Version() string { return Version }
func (Renderer) Descriptor() domain.RendererDescriptor {
	return domain.RendererDescriptor{ID: ID, Version: Version, ContentType: ContentType, FileExtension: FileExtension}
}
func (Renderer) SupportedRuleKinds() []domain.RuleKind {
	return []domain.RuleKind{
		domain.RuleDomainExact,
		domain.RuleDomainSuffix,
		domain.RuleIPv4,
		domain.RuleIPv6,
		domain.RulePrefix4,
		domain.RulePrefix6,
	}
}
func (Renderer) ProjectedRuleCount(plan domain.RoutingPlan) (int, error) {
	return len(plan.Rules), nil
}
func (Renderer) Render(plan domain.RoutingPlan) ([]byte, error) { return Render(plan) }
func (Renderer) Validate(payload []byte) error                  { return Validate(payload) }

type Rule = planjson.Rule
type Excluded = planjson.Excluded
type Coverage = planjson.Coverage
type Relation = planjson.Relation
type Sighting = planjson.Sighting
type Resource = planjson.Resource
type Plan = planjson.Plan

func Render(plan domain.RoutingPlan) ([]byte, error) { return planjson.Encode(plan) }

// Parse decodes the document independently of Render and returns the typed
// plan. planjson.Validate proves the size bound, the single-document rule,
// the absence of unknown fields, and a complete identity; this function adds
// what that check does not: re-marshaling the decoded value and requiring
// byte equality with the input, so a document that is merely equivalent but
// reordered, reindented, or otherwise non-canonical is refused.
func Parse(payload []byte) (Plan, error) {
	if err := planjson.Validate(payload); err != nil {
		return Plan{}, err
	}
	var plan Plan
	if err := json.Unmarshal(payload, &plan); err != nil {
		return Plan{}, fmt.Errorf("decode routing plan JSON: %w", err)
	}
	rendered, err := json.Marshal(plan)
	if err != nil {
		return Plan{}, fmt.Errorf("marshal routing plan JSON: %w", err)
	}
	rendered = append(rendered, '\n')
	if !bytes.Equal(rendered, payload) {
		return Plan{}, fmt.Errorf("routing plan JSON is not in canonical order or form")
	}
	return plan, nil
}

// Validate fully parses a bounded artifact in memory and requires it to be
// byte-identical to what re-rendering the decoded value would produce. It is
// intentionally a renderer concern and performs no publication or policy
// decisions.
func Validate(encoded []byte) error {
	_, err := Parse(encoded)
	return err
}

func Write(w io.Writer, plan domain.RoutingPlan) error {
	if w == nil {
		return fmt.Errorf("raw JSON writer is nil")
	}
	encoded, err := Render(plan)
	if err != nil {
		return err
	}
	_, err = w.Write(encoded)
	if err != nil {
		return fmt.Errorf("write raw JSON: %w", err)
	}
	return nil
}

func Convert(plan domain.RoutingPlan) (Plan, error) { return planjson.Convert(plan) }
