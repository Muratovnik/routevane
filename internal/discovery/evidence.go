package discovery

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

var (
	ErrInvalidScenario = errors.New("invalid exploration scenario")
	ErrInvalidEvidence = errors.New("invalid session evidence")
)

// Activation outcomes. They are stable identities reported in diagnostics.
const (
	// ActivationAccepted means the host is configured for observation.
	ActivationAccepted = "accepted"
	// ActivationDependency means the host is recorded and never activated.
	ActivationDependency = "dependency"
)

// Activation reason codes.
const (
	// ReasonSameSiteRequiredComponent is a same-site host observed while a step
	// exercising a required component was running.
	ReasonSameSiteRequiredComponent = "same_site_required_component"
	// ReasonComponentNotExercised means the component exists in the scenario but
	// no step actually exercised it, so nothing about it was observed.
	ReasonComponentNotExercised = "component_not_exercised"
	// ReasonOptionalComponent covers telemetry and advertising: recorded, never
	// required, never activated.
	ReasonOptionalComponent = "optional_component"
	// ReasonSharedThirdParty is a host outside the registrable domain. It stays
	// a dependency and is never widened to a suffix, an ASN, or a network.
	ReasonSharedThirdParty = CandidateThirdPartyDomain
	// ReasonUnknownComponent is a host whose component could not be attributed
	// to an exercised step.
	ReasonUnknownComponent = "unknown_component"
)

// Step is one repeatable exploration action. A scenario is an ordered list of
// steps, so the same exploration can be replayed and compared.
type Step struct {
	// ID is the step identity used in reports and in relation provenance.
	ID string `json:"id" yaml:"id"`
	// Component is the taxonomy identity every host observed during this step is
	// attributed to.
	Component string `json:"component" yaml:"component"`
	// URL is navigated at the start of the step. Empty continues on the current
	// document, which is how a scenario expresses "the same page, later".
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
	// SettleSeconds is how long the step waits after navigating. It is bounded so
	// a scenario cannot become an unbounded crawl.
	SettleSeconds int `json:"settle_seconds,omitempty" yaml:"settle_seconds,omitempty"`
}

// Scenario is a repeatable exploration description.
type Scenario struct {
	Target string `json:"target" yaml:"target"`
	Steps  []Step `json:"steps" yaml:"steps"`
}

// Scenario bounds. A learning session is a guided exploration, not a crawler.
const (
	MaxScenarioSteps  = 16
	MaxStepSettle     = 30
	DefaultStepSettle = 3
)

// Validate refuses a scenario that is unbounded, unattributable, or ambiguous.
func (s Scenario) Validate() error {
	if len(s.Steps) == 0 {
		return fmt.Errorf("%w: no step", ErrInvalidScenario)
	}
	if len(s.Steps) > MaxScenarioSteps {
		return fmt.Errorf("%w: %d steps exceed the bound %d", ErrInvalidScenario, len(s.Steps), MaxScenarioSteps)
	}
	seen := make(map[string]struct{}, len(s.Steps))
	for index, step := range s.Steps {
		if domain.ValidateSlug(step.ID) != nil {
			return fmt.Errorf("%w: step %d has an invalid id", ErrInvalidScenario, index)
		}
		if _, duplicate := seen[step.ID]; duplicate {
			return fmt.Errorf("%w: duplicate step id %q", ErrInvalidScenario, step.ID)
		}
		seen[step.ID] = struct{}{}
		if !domain.KnownComponent(step.Component) {
			return fmt.Errorf("%w: step %q has an unknown component %q", ErrInvalidScenario, step.ID, step.Component)
		}
		if step.SettleSeconds < 0 || step.SettleSeconds > MaxStepSettle {
			return fmt.Errorf("%w: step %q settle is outside 0..%d", ErrInvalidScenario, step.ID, MaxStepSettle)
		}
		if index == 0 && step.URL == "" {
			return fmt.Errorf("%w: the first step must navigate", ErrInvalidScenario)
		}
		if step.URL != "" {
			if _, err := NormalizeTarget(step.URL); err != nil {
				return fmt.Errorf("%w: step %q url: %v", ErrInvalidScenario, step.ID, err)
			}
		}
	}
	return nil
}

// ExercisedComponents lists the components the scenario actually exercises.
func (s Scenario) ExercisedComponents() []string {
	seen := map[string]struct{}{}
	for _, step := range s.Steps {
		seen[step.Component] = struct{}{}
	}
	components := slices.Sorted(maps.Keys(seen))
	return components
}

// HostEvidence is what one session observed about one host.
type HostEvidence struct {
	Host string `json:"host"`
	// Component is the taxonomy identity attributed from the step that was
	// running. An empty value means it could not be attributed.
	Component string `json:"component,omitempty"`
	// StepIDs are the exploration steps during which the host appeared.
	StepIDs []string `json:"step_ids,omitempty"`
	// LoadedBy are the document hosts that were loading when this host appeared.
	LoadedBy []string `json:"loaded_by,omitempty"`
	// RedirectsTo are observed redirect targets of this host.
	RedirectsTo []string `json:"redirects_to,omitempty"`
	Requests    int      `json:"requests,omitempty"`
	// Refused reports the policy reason when the host was never contacted.
	Refused string `json:"refused,omitempty"`
}

// SessionEvidence is the complete, ordered outcome of one learning session or
// one imported HAR file. It is the single input shape the activation policy
// consumes, so a browser session and an import cannot diverge in meaning.
type SessionEvidence struct {
	Target Target         `json:"-"`
	Steps  []string       `json:"steps,omitempty"`
	Hosts  []HostEvidence `json:"hosts"`
	// Requests and Bytes are the session totals.
	Requests int   `json:"requests,omitempty"`
	Bytes    int64 `json:"bytes,omitempty"`
	// CleanupError reports that the session's temporary profile could not be
	// removed even after the bounded retry. It never replaces the session's
	// own error: a run failure is still reported through the returned error,
	// and a cleanup failure is only ever visible here.
	CleanupError string `json:"cleanup_error,omitempty"`
}

// Decision is the deterministic activation outcome for one host.
type Decision struct {
	Host      string   `json:"host"`
	Component string   `json:"component,omitempty"`
	Outcome   string   `json:"outcome"`
	Reasons   []string `json:"reasons"`
	StepIDs   []string `json:"step_ids,omitempty"`
}

// Classify applies the activation policy. Every rule is structural: the
// registrable-domain relationship, the component taxonomy, and whether a step
// exercising that component actually ran. No score, model, or vendor list takes
// part, so the same evidence always produces the same decisions.
//
//   - same-site host attributed to a required component that was exercised:
//     accepted;
//   - same-site host attributed to telemetry or advertising: dependency;
//   - same-site host whose component was never exercised: dependency;
//   - any host outside the registrable domain: dependency, never widened;
//   - anything unattributed: dependency.
func Classify(evidence SessionEvidence, exercised []string) []Decision {
	exercisedSet := make(map[string]struct{}, len(exercised))
	for _, component := range exercised {
		exercisedSet[component] = struct{}{}
	}
	registrable := evidence.Target.RegistrableDomain
	decisions := make([]Decision, 0, len(evidence.Hosts))
	for _, host := range evidence.Hosts {
		decision := Decision{Host: host.Host, Component: host.Component, StepIDs: host.StepIDs, Outcome: ActivationDependency}
		switch {
		case host.Refused != "":
			decision.Reasons = domain.StableStrings([]string{CandidateRefusedByPolicy, host.Refused})
		case registrable == "" || !SameSite(registrable, host.Host):
			decision.Reasons = []string{ReasonSharedThirdParty}
		case host.Component == "" || !domain.KnownComponent(host.Component):
			decision.Reasons = []string{ReasonUnknownComponent}
		case !domain.RequiredComponent(host.Component):
			decision.Reasons = []string{ReasonOptionalComponent}
		default:
			if _, ran := exercisedSet[host.Component]; !ran {
				decision.Reasons = []string{ReasonComponentNotExercised}
				break
			}
			decision.Outcome = ActivationAccepted
			decision.Reasons = []string{ReasonSameSiteRequiredComponent}
		}
		decisions = append(decisions, decision)
	}
	slices.SortFunc(decisions, func(a, b Decision) int {
		return cmp.Or(cmp.Compare(a.Component, b.Component), cmp.Compare(a.Host, b.Host))
	})
	return decisions
}

// Relations turns the session's provenance into domain relations for one
// service. A relation records how a dependency appeared; it never asserts that
// either side owns the other, so it can never widen routing on its own.
func Relations(evidence SessionEvidence, listID, sourceID, sourceRevision string, observedAt time.Time, validity time.Duration) ([]domain.Relation, error) {
	if domain.ValidateSlug(listID) != nil || domain.ValidateSlug(sourceID) != nil || sourceRevision == "" || observedAt.IsZero() || validity <= 0 {
		return nil, ErrInvalidEvidence
	}
	observedAt = observedAt.UTC()
	validUntil := observedAt.Add(validity)
	relations := make([]domain.Relation, 0, len(evidence.Hosts))
	appendRelation := func(from, to string, relationType domain.RelationType, componentID string) {
		source, sourceErr := domain.NewDomainResource(from)
		target, targetErr := domain.NewDomainResource(to)
		if sourceErr != nil || targetErr != nil || source.CanonicalValue() == target.CanonicalValue() {
			return
		}
		if !domain.KnownComponent(componentID) {
			componentID = domain.ComponentThirdParty
		}
		relations = append(relations, domain.Relation{
			SourceResource: source, RelationType: relationType, TargetResource: target,
			ListID: listID, ComponentID: componentID,
			FirstSeen: observedAt, LastSeen: observedAt, ValidUntil: validUntil,
			SourceID: sourceID, SourceRevision: sourceRevision, Validity: domain.ValidityValid,
		})
	}
	for _, host := range evidence.Hosts {
		for _, loader := range host.LoadedBy {
			appendRelation(host.Host, loader, domain.RelationLoadedBy, host.Component)
		}
		for _, redirect := range host.RedirectsTo {
			appendRelation(host.Host, redirect, domain.RelationRedirectsTo, host.Component)
		}
		for _, step := range host.StepIDs {
			// The step identity is recorded as a synthetic name under the target
			// so a session attribution is inspectable without a new table.
			appendRelation(host.Host, stepResourceName(step), domain.RelationObservedInSession, host.Component)
		}
	}
	slices.SortFunc(relations, func(a, b domain.Relation) int { return cmp.Compare(a.Fingerprint(), b.Fingerprint()) })
	return relations, nil
}

// stepResourceName encodes a step identity as a domain-shaped value inside a
// reserved zone, so it can never collide with a real host.
func stepResourceName(stepID string) string {
	return strings.ToLower(stepID) + ".step.routevane.invalid"
}

// MergeEvidence combines evidence from several sources — for example a browser
// session and an imported HAR file — into one deterministic set.
func MergeEvidence(target Target, parts ...SessionEvidence) SessionEvidence {
	merged := SessionEvidence{Target: target}
	byHost := map[string]HostEvidence{}
	steps := map[string]struct{}{}
	for _, part := range parts {
		merged.Requests += part.Requests
		merged.Bytes += part.Bytes
		for _, step := range part.Steps {
			steps[step] = struct{}{}
		}
		for _, host := range part.Hosts {
			existing, known := byHost[host.Host]
			if !known {
				existing = HostEvidence{Host: host.Host}
			}
			existing.Requests += host.Requests
			existing.StepIDs = domain.StableStrings(append(existing.StepIDs, host.StepIDs...))
			existing.LoadedBy = domain.StableStrings(append(existing.LoadedBy, host.LoadedBy...))
			existing.RedirectsTo = domain.StableStrings(append(existing.RedirectsTo, host.RedirectsTo...))
			existing.Component = strongerComponent(existing.Component, host.Component)
			if existing.Refused == "" {
				existing.Refused = host.Refused
			}
			if host.Refused == "" {
				// A host contacted in any part is not a refused host.
				existing.Refused = ""
			}
			byHost[host.Host] = existing
		}
	}
	merged.Steps = slices.Sorted(maps.Keys(steps))
	merged.Hosts = make([]HostEvidence, 0, len(byHost))
	for _, host := range byHost {
		merged.Hosts = append(merged.Hosts, host)
	}
	slices.SortFunc(merged.Hosts, func(a, b HostEvidence) int { return cmp.Compare(a.Host, b.Host) })
	return merged
}

// strongerComponent resolves two attributions for the same host. The taxonomy
// order is the precedence: an earlier component wins, so a host seen during both
// core and telemetry steps stays core.
func strongerComponent(left, right string) string {
	if left == right {
		return left
	}
	for _, component := range domain.KnownComponents() {
		if left == component {
			return left
		}
		if right == component {
			return right
		}
	}
	if left != "" {
		return left
	}
	return right
}
