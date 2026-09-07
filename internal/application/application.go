// Package application coordinates the refresh and build use cases. Its
// interfaces are declared by these consumers after their first concrete need.
package application

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/planner"
)

const SourceTimeout = 5 * time.Second

var (
	ErrSourceFailed = errors.New("one or more source cycles failed")
	// ErrSourceDegraded reports that every failed cycle is inside its grace
	// window, so the stored observations remain usable and a build can still
	// publish a reason-coded artifact.
	ErrSourceDegraded  = errors.New("one or more source cycles failed inside their grace window")
	ErrFormatMismatch  = errors.New("effective profile does not match catalog")
	ErrPreflight       = errors.New("routing plan preflight failed")
	ErrRuleLimit       = errors.New("routing plan exceeds target rule limit")
	ErrPartialCoverage = errors.New("required routing coverage is incomplete")
)

type Clock interface {
	Now() time.Time
}

type ClockFunc func() time.Time

func (f ClockFunc) Now() time.Time {
	if f == nil {
		return time.Time{}
	}
	return f()
}

type SourceRequest struct {
	ListID         string
	ComponentID    string
	SourceID       string
	SourceRevision string
	Names          []string
	URL            string
	Format         domain.FeedFormat
	SourceClass    domain.SourceClass
	ObservedAt     time.Time
}

type SourceResult struct {
	Sightings []domain.Sighting
	Relations []domain.Relation
	// Skipped counts entries a source read and refused to record. It is
	// reported, never silently dropped, so an unusable feed is visible.
	Skipped int
}

type Source interface {
	Observe(context.Context, SourceRequest) (SourceResult, error)
}

// SourceRegistry resolves a catalog source type to its implementation. The map
// replaces the single-source parameter now that a second real source exists;
// refresh refuses a type the composition did not register.
type SourceRegistry map[domain.SourceType]Source

// SourceErrorCode is the stable failure identity recorded for a source cycle.
func SourceErrorCode(sourceType domain.SourceType) string {
	switch sourceType {
	case domain.SourceDNS:
		return "dns_observation_failed"
	case domain.SourceHTTP:
		return "http_feed_failed"
	default:
		return "external_source_failed"
	}
}

// SourceGracePeriod is how long observations from a failing source stay usable
// after its last successful cycle. Inside the window the plan is published with
// a source_degraded warning instead of losing routes to a transient outage.
const SourceGracePeriod = 24 * time.Hour

// SourceRunState is the persisted health of one source cycle identity. It is
// read in the same coherent transaction as the observations it describes.
type SourceRunState struct {
	SourceID       string
	SourceRevision string
	LastSuccessAt  time.Time
	LastFailureAt  time.Time
}

// LastRunFailed reports whether the most recent cycle for this identity failed.
func (s SourceRunState) LastRunFailed() bool {
	return !s.LastFailureAt.IsZero() && s.LastFailureAt.After(s.LastSuccessAt)
}

// DegradedSources selects the sources whose latest cycle failed while their last
// success is still inside the grace window. The result is sorted by source id so
// the planner stays deterministic.
func DegradedSources(health []SourceRunState, activeRevisions map[string]string, cutoff time.Time) []planner.DegradedSource {
	// One source id can have rows for several revisions. The longest surviving
	// grace window wins so a revision change cannot shorten an active window.
	windows := make(map[string]time.Time, len(health))
	for _, state := range health {
		if state.SourceID == "" || state.LastSuccessAt.IsZero() || !state.LastRunFailed() {
			continue
		}
		if want, ok := activeRevisions[state.SourceID]; ok && want != state.SourceRevision {
			continue
		}
		graceUntil := state.LastSuccessAt.UTC().Add(SourceGracePeriod)
		if !graceUntil.After(cutoff.UTC()) {
			continue
		}
		if existing, ok := windows[state.SourceID]; !ok || existing.Before(graceUntil) {
			windows[state.SourceID] = graceUntil
		}
	}
	ids := slices.Sorted(maps.Keys(windows))
	degraded := make([]planner.DegradedSource, 0, len(ids))
	for _, id := range ids {
		degraded = append(degraded, planner.DegradedSource{SourceID: id, GraceUntil: windows[id]})
	}
	return degraded
}

type FormatRecord struct {
	FormatKey       string
	ListID          string
	TargetID        string
	RendererID      string
	CatalogRevision string
	ConfigJSON      []byte
	UpdatedAt       time.Time
}

type SuccessCycle struct {
	ListID         string
	SourceID       string
	SourceRevision string
	StartedAt      time.Time
	CompletedAt    time.Time
	Sightings      []domain.Sighting
	Relations      []domain.Relation
	Format         *FormatRecord
}

type FailureCycle struct {
	ListID         string
	SourceID       string
	SourceRevision string
	StartedAt      time.Time
	CompletedAt    time.Time
	ErrorCode      string
}

type PlanningSnapshot struct {
	Sightings []domain.Sighting
	Relations []domain.Relation
	Format    FormatRecord
	// SourceHealth describes the same list's source cycles at the same read.
	// It is part of the snapshot so grace decisions and observations cannot
	// disagree about what the database contained.
	SourceHealth []SourceRunState
}

type ObservationStore interface {
	ApplySuccess(context.Context, SuccessCycle) error
	RecordFailure(context.Context, FailureCycle) error
	ReadPlanningSnapshot(context.Context, string, map[string]string, string, time.Time) (PlanningSnapshot, error)
}

type Renderer interface {
	ID() string
	Version() string
	Descriptor() domain.RendererDescriptor
	SupportedRuleKinds() []domain.RuleKind
	ProjectedRuleCount(domain.RoutingPlan) (int, error)
	Render(domain.RoutingPlan) ([]byte, error)
	Validate([]byte) error
}

// RendererRegistry resolves a renderer id to its implementation. The target
// catalog is deliberately a separate map: a target describes a device and its
// limits, a renderer describes a format, and several targets may share one
// format.
type RendererRegistry map[string]Renderer

// Validate checks the registry against itself. Identity drift between a
// renderer and its key, or a descriptor a caller cannot serve, is a composition
// error rather than a request error.
func (r RendererRegistry) Validate() error {
	if len(r) == 0 {
		return fmt.Errorf("renderer registry is empty")
	}
	for id, renderer := range r {
		if renderer == nil || renderer.ID() != id {
			return fmt.Errorf("renderer registry key %q does not match its implementation", id)
		}
		if domain.ValidateSlug(id) != nil || domain.ValidateSlug(renderer.Version()) != nil {
			return fmt.Errorf("renderer %q has an invalid identity", id)
		}
		descriptor := renderer.Descriptor()
		if !descriptor.IsValid() || descriptor.ID != id || descriptor.Version != renderer.Version() {
			return fmt.Errorf("renderer %q has an invalid or drifting descriptor", id)
		}
		if len(renderer.SupportedRuleKinds()) == 0 {
			return fmt.Errorf("renderer %q supports no rule kind", id)
		}
	}
	return nil
}

// For resolves the renderer a target definition asks for.
func (r RendererRegistry) For(target domain.TargetDefinition) (Renderer, error) {
	renderer, ok := r[target.RendererID]
	if !ok || renderer == nil {
		return nil, fmt.Errorf("%w: no renderer registered for target %q", ErrPreflight, target.ID)
	}
	if renderer.Version() != target.FormatKey {
		return nil, fmt.Errorf("%w: renderer %q does not implement format key %q", ErrPreflight, target.RendererID, target.FormatKey)
	}
	return renderer, nil
}

type ArtifactStore interface {
	Put(context.Context, string, string, time.Time, string, []byte) (string, bool, error)
}

// BuildOutput is the non-publication file boundary. The concrete output
// directory and safe commit mechanics remain infrastructure concerns.
type BuildOutput interface {
	Put(context.Context, domain.RendererDescriptor, string, []byte) (string, bool, error)
}

// PreparedPlan is the immutable handoff between coherent planning/preflight
// and rendering. Callers may encode Plan before invoking an untrusted renderer.
type PreparedPlan struct {
	Plan                domain.RoutingPlan
	Target              domain.TargetDefinition
	compositionOverlaps CompositionOverlaps
}

// PrepareLists reads all selected lists at one cutoff and completes
// planner and renderer preflight without invoking Render.
func PrepareLists(ctx context.Context, definitions []domain.ListDefinition, activeRevisions map[string]map[string]string, target domain.TargetDefinition, store ObservationStore, renderer Renderer, cutoff time.Time) (PreparedPlan, error) {
	if ctx == nil || store == nil || renderer == nil || len(definitions) == 0 || target.ID == "" || target.FormatKey == "" || target.RendererID == "" || len(target.RendererOptions) != 0 || cutoff.IsZero() {
		return PreparedPlan{}, fmt.Errorf("invalid prepare composition")
	}
	ordered := append([]domain.ListDefinition(nil), definitions...)
	slices.SortFunc(ordered, func(a, b domain.ListDefinition) int { return cmp.Compare(a.ID, b.ID) })
	for i, definition := range ordered {
		if domain.ValidateSlug(definition.ID) != nil || definition.CatalogRevision == "" || (i > 0 && ordered[i-1].ID == definition.ID) {
			return PreparedPlan{}, fmt.Errorf("invalid list definition")
		}
	}
	cutoff = cutoff.UTC()
	rawFormat := domain.RawJSONTargetDefinition()
	inputs := make([]planner.ListInput, 0, len(ordered))
	for _, definition := range ordered {
		snapshot, err := store.ReadPlanningSnapshot(ctx, definition.ID, activeRevisions[definition.ID], rawFormat.FormatKey, cutoff)
		if err != nil {
			return PreparedPlan{}, fmt.Errorf("read planning snapshot for %q: %w", definition.ID, err)
		}
		if snapshot.Format.CatalogRevision != definition.CatalogRevision || snapshot.Format.ListID != definition.ID || snapshot.Format.FormatKey != rawFormat.FormatKey || snapshot.Format.TargetID != rawFormat.ID || snapshot.Format.RendererID != rawFormat.RendererID {
			return PreparedPlan{}, ErrFormatMismatch
		}
		inputs = append(inputs, planner.ListInput{Definition: definition, Sightings: snapshot.Sightings, Relations: snapshot.Relations, Degraded: DegradedSources(snapshot.SourceHealth, activeRevisions[definition.ID], cutoff)})
	}
	planningTarget := target
	planningTarget.Constraints.MaxRules = 0
	plan, err := planner.BuildPlanSet(inputs, planningTarget, cutoff)
	if err != nil {
		switch {
		case errors.Is(err, planner.ErrRuleLimitExceeded):
			return PreparedPlan{}, fmt.Errorf("%w: %v", ErrRuleLimit, err)
		case errors.Is(err, planner.ErrRequiredCoverage):
			return PreparedPlan{}, fmt.Errorf("%w: %v", ErrPartialCoverage, err)
		default:
			return PreparedPlan{}, fmt.Errorf("build routing plan set: %w", err)
		}
	}
	plan.SemanticHash = planner.SemanticHash(plan, target)
	wantLists := make([]string, len(ordered))
	for i := range ordered {
		wantLists[i] = ordered[i].ID
	}
	if !equalStrings(plan.Lists, wantLists) {
		return PreparedPlan{}, fmt.Errorf("%w: list set mismatch", ErrPreflight)
	}
	if err := PreflightPlan(plan, target, renderer, cutoff); err != nil {
		return PreparedPlan{}, err
	}
	return PreparedPlan{Plan: plan, Target: target}, nil
}

// RenderPrepared renders, bounds, and validates one preflighted plan.
func RenderPrepared(prepared PreparedPlan, renderer Renderer) ([]byte, error) {
	if renderer == nil || renderer.ID() != prepared.Target.RendererID || renderer.Version() != prepared.Target.FormatKey {
		return nil, fmt.Errorf("%w: renderer identity", ErrPreflight)
	}
	payload, err := renderer.Render(cloneRoutingPlan(prepared.Plan))
	if err != nil {
		return nil, fmt.Errorf("render target artifact: %w", err)
	}
	if prepared.Target.Constraints.MaxArtifactSize > 0 && len(payload) > prepared.Target.Constraints.MaxArtifactSize {
		return nil, fmt.Errorf("rendered artifact exceeds profile size")
	}
	if err := renderer.Validate(payload); err != nil {
		return nil, fmt.Errorf("validate target artifact: %w", err)
	}
	return payload, nil
}

type RefreshSummary struct {
	ListID         string `json:"list"`
	SourceRuns     int    `json:"source_runs"`
	SuccessfulRuns int    `json:"successful_runs"`
	FailedRuns     int    `json:"failed_runs"`
	Sightings      int    `json:"sightings"`
	Relations      int    `json:"relations"`
	// SkippedEntries counts feed entries a source refused to record.
	SkippedEntries int `json:"skipped_entries,omitempty"`
	// DegradedSources lists the sources that failed this cycle while their
	// previous success is still inside the grace window.
	DegradedSources []string `json:"degraded_sources,omitempty"`
}

func RefreshList(ctx context.Context, definition domain.ListDefinition, target domain.TargetDefinition, sources SourceRegistry, store ObservationStore, clock Clock) (RefreshSummary, error) {
	summary := RefreshSummary{ListID: definition.ID}
	if ctx == nil || len(sources) == 0 || store == nil || clock == nil || definition.ID == "" || definition.CatalogRevision == "" {
		return summary, fmt.Errorf("invalid refresh composition")
	}
	observedAt := clock.Now().UTC()
	if observedAt.IsZero() {
		return summary, fmt.Errorf("clock returned zero time")
	}
	profileJSON, err := json.Marshal(target)
	if err != nil {
		return summary, fmt.Errorf("encode effective profile: %w", err)
	}
	format := FormatRecord{FormatKey: target.FormatKey, ListID: definition.ID, TargetID: target.ID, RendererID: target.RendererID, CatalogRevision: definition.CatalogRevision, ConfigJSON: profileJSON, UpdatedAt: observedAt}
	manual := SuccessCycle{ListID: definition.ID, SourceID: "manual", SourceRevision: definition.CatalogRevision, StartedAt: observedAt, CompletedAt: observedAt, Format: &format}
	if err := store.ApplySuccess(ctx, manual); err != nil {
		return summary, fmt.Errorf("persist manual source cycle: %w", err)
	}
	summary.SourceRuns++
	summary.SuccessfulRuns++

	for _, definitionSource := range definition.Sources {
		source, registered := sources[definitionSource.Type]
		if !registered || source == nil {
			return summary, fmt.Errorf("unsupported source type %q", definitionSource.Type)
		}
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		summary.SourceRuns++
		sourceCtx, cancel := context.WithTimeout(ctx, SourceTimeout)
		result, observeErr := source.Observe(sourceCtx, SourceRequest{ListID: definition.ID, ComponentID: definitionSource.ComponentID, SourceID: definitionSource.ID, SourceRevision: definitionSource.Revision, Names: append([]string(nil), definitionSource.Names...), URL: definitionSource.URL, Format: definitionSource.Format, SourceClass: definitionSource.Class, ObservedAt: observedAt})
		cancel()
		if observeErr != nil {
			summary.FailedRuns++
			if err := store.RecordFailure(ctx, FailureCycle{ListID: definition.ID, SourceID: definitionSource.ID, SourceRevision: definitionSource.Revision, StartedAt: observedAt, CompletedAt: observedAt, ErrorCode: SourceErrorCode(definitionSource.Type)}); err != nil {
				return summary, fmt.Errorf("record failed source cycle: %w", err)
			}
			continue
		}
		if err := store.ApplySuccess(ctx, SuccessCycle{ListID: definition.ID, SourceID: definitionSource.ID, SourceRevision: definitionSource.Revision, StartedAt: observedAt, CompletedAt: observedAt, Sightings: result.Sightings, Relations: result.Relations}); err != nil {
			return summary, fmt.Errorf("persist %s source cycle: %w", definitionSource.Type, err)
		}
		summary.SuccessfulRuns++
		summary.Sightings += len(result.Sightings)
		summary.Relations += len(result.Relations)
		summary.SkippedEntries += result.Skipped
	}
	if summary.FailedRuns > 0 {
		// A failed cycle is always reported. Whether it is fatal depends on the
		// persisted source health at this cutoff: every failure inside its
		// grace window leaves the stored observations usable.
		revisions := activeSourceRevisions(definition)
		snapshot, readErr := store.ReadPlanningSnapshot(ctx, definition.ID, revisions, target.FormatKey, observedAt)
		if readErr != nil {
			return summary, ErrSourceFailed
		}
		for _, degraded := range DegradedSources(snapshot.SourceHealth, revisions, observedAt) {
			summary.DegradedSources = append(summary.DegradedSources, degraded.SourceID)
		}
		if len(summary.DegradedSources) == summary.FailedRuns {
			return summary, ErrSourceDegraded
		}
		return summary, ErrSourceFailed
	}
	return summary, nil
}

func activeSourceRevisions(definition domain.ListDefinition) map[string]string {
	revisions := make(map[string]string, len(definition.Sources))
	for _, source := range definition.Sources {
		revisions[source.ID] = source.Revision
	}
	return revisions
}

type BuildResult struct {
	Path         string
	Reused       bool
	SemanticHash string
	RuleCount    int
	Diagnostics  []Diagnostic
}

type Diagnostic struct {
	Code  string
	Count int
}

func BuildList(ctx context.Context, definition domain.ListDefinition, activeRevisions map[string]string, store ObservationStore, renderer Renderer, artifacts ArtifactStore, clock Clock) (BuildResult, error) {
	if ctx == nil || store == nil || renderer == nil || artifacts == nil || clock == nil || definition.ID == "" || definition.CatalogRevision == "" {
		return BuildResult{}, fmt.Errorf("invalid build composition")
	}
	cutoff := clock.Now().UTC()
	if cutoff.IsZero() {
		return BuildResult{}, fmt.Errorf("clock returned zero time")
	}
	formatKey := domain.RawJSONTargetDefinition().FormatKey
	snapshot, err := store.ReadPlanningSnapshot(ctx, definition.ID, activeRevisions, formatKey, cutoff)
	if err != nil {
		return BuildResult{}, fmt.Errorf("read planning snapshot: %w", err)
	}
	if snapshot.Format.CatalogRevision != definition.CatalogRevision || snapshot.Format.ListID != definition.ID || snapshot.Format.RendererID != "raw-json" {
		return BuildResult{}, ErrFormatMismatch
	}
	var target domain.TargetDefinition
	if err := json.Unmarshal(snapshot.Format.ConfigJSON, &target); err != nil {
		return BuildResult{}, fmt.Errorf("decode effective profile: %w", err)
	}
	if target.ID != snapshot.Format.TargetID || target.FormatKey != snapshot.Format.FormatKey || target.RendererID != snapshot.Format.RendererID || target.ID != "raw-json" {
		return BuildResult{}, ErrFormatMismatch
	}
	plan, err := planner.BuildPlanWithRelations(definition, snapshot.Sightings, snapshot.Relations, target, cutoff)
	if err != nil {
		return BuildResult{}, fmt.Errorf("build routing plan: %w", err)
	}
	if err := PreflightPlan(plan, target, renderer, cutoff); err != nil {
		return BuildResult{}, err
	}
	payload, err := renderer.Render(cloneRoutingPlan(plan))
	if err != nil {
		return BuildResult{}, fmt.Errorf("render raw JSON: %w", err)
	}
	if target.Constraints.MaxArtifactSize > 0 && len(payload) > target.Constraints.MaxArtifactSize {
		return BuildResult{}, fmt.Errorf("rendered artifact exceeds profile size")
	}
	if err := renderer.Validate(payload); err != nil {
		return BuildResult{}, fmt.Errorf("validate raw JSON: %w", err)
	}
	path, reused, err := artifacts.Put(ctx, renderer.ID(), definition.ID, cutoff, plan.SemanticHash, payload)
	if err != nil {
		return BuildResult{}, fmt.Errorf("store unpublished artifact: %w", err)
	}
	return BuildResult{Path: path, Reused: reused, SemanticHash: plan.SemanticHash, RuleCount: len(plan.Rules)}, nil
}

// BuildLists reads every selected list at one cutoff and only invokes
// the renderer after the complete set has passed planning and preflight. The
// stored Raw JSON profile is used solely as proof that refresh state belongs to
// the current list catalog; the requested target comes directly from the
// catalog argument.
func BuildLists(ctx context.Context, definitions []domain.ListDefinition, activeRevisions map[string]map[string]string, target domain.TargetDefinition, store ObservationStore, renderer Renderer, output BuildOutput, clock Clock) (BuildResult, error) {
	if ctx == nil || store == nil || renderer == nil || output == nil || clock == nil || len(definitions) == 0 || target.ID == "" || target.FormatKey == "" || target.RendererID == "" || len(target.RendererOptions) != 0 {
		return BuildResult{}, fmt.Errorf("invalid multi-list build composition")
	}
	ordered := append([]domain.ListDefinition(nil), definitions...)
	slices.SortFunc(ordered, func(a, b domain.ListDefinition) int { return cmp.Compare(a.ID, b.ID) })
	for i, definition := range ordered {
		if domain.ValidateSlug(definition.ID) != nil || definition.CatalogRevision == "" {
			return BuildResult{}, fmt.Errorf("invalid list definition")
		}
		if i > 0 && ordered[i-1].ID == definition.ID {
			return BuildResult{}, fmt.Errorf("duplicate list definition")
		}
	}
	cutoff := clock.Now().UTC()
	if cutoff.IsZero() {
		return BuildResult{}, fmt.Errorf("clock returned zero time")
	}
	rawFormat := domain.RawJSONTargetDefinition()
	inputs := make([]planner.ListInput, 0, len(ordered))
	for _, definition := range ordered {
		snapshot, err := store.ReadPlanningSnapshot(ctx, definition.ID, activeRevisions[definition.ID], rawFormat.FormatKey, cutoff)
		if err != nil {
			return BuildResult{}, fmt.Errorf("read planning snapshot for %q: %w", definition.ID, err)
		}
		if snapshot.Format.CatalogRevision != definition.CatalogRevision || snapshot.Format.ListID != definition.ID || snapshot.Format.FormatKey != rawFormat.FormatKey || snapshot.Format.TargetID != rawFormat.ID || snapshot.Format.RendererID != rawFormat.RendererID {
			return BuildResult{}, ErrFormatMismatch
		}
		inputs = append(inputs, planner.ListInput{Definition: definition, Sightings: snapshot.Sightings, Relations: snapshot.Relations, Degraded: DegradedSources(snapshot.SourceHealth, activeRevisions[definition.ID], cutoff)})
	}
	planningTarget := target
	planningTarget.Constraints.MaxRules = 0
	plan, err := planner.BuildPlanSet(inputs, planningTarget, cutoff)
	if err != nil {
		switch {
		case errors.Is(err, planner.ErrRuleLimitExceeded):
			return BuildResult{}, fmt.Errorf("%w: %v", ErrRuleLimit, err)
		case errors.Is(err, planner.ErrRequiredCoverage):
			return BuildResult{}, fmt.Errorf("%w: %v", ErrPartialCoverage, err)
		default:
			return BuildResult{}, fmt.Errorf("build routing plan set: %w", err)
		}
	}
	plan.SemanticHash = planner.SemanticHash(plan, target)
	wantLists := make([]string, len(ordered))
	for i := range ordered {
		wantLists[i] = ordered[i].ID
	}
	if !equalStrings(plan.Lists, wantLists) {
		return BuildResult{}, fmt.Errorf("%w: list set mismatch", ErrPreflight)
	}
	if err := PreflightPlan(plan, target, renderer, cutoff); err != nil {
		return BuildResult{}, err
	}
	payload, err := renderer.Render(cloneRoutingPlan(plan))
	if err != nil {
		return BuildResult{}, fmt.Errorf("render target artifact: %w", err)
	}
	if target.Constraints.MaxArtifactSize > 0 && len(payload) > target.Constraints.MaxArtifactSize {
		return BuildResult{}, fmt.Errorf("rendered artifact exceeds profile size")
	}
	if err := renderer.Validate(payload); err != nil {
		return BuildResult{}, fmt.Errorf("validate target artifact: %w", err)
	}
	path, reused, err := output.Put(ctx, renderer.Descriptor(), plan.SemanticHash, payload)
	if err != nil {
		return BuildResult{}, fmt.Errorf("store target output: %w", err)
	}
	return BuildResult{Path: path, Reused: reused, SemanticHash: plan.SemanticHash, RuleCount: len(plan.Rules), Diagnostics: planDiagnostics(plan)}, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func planDiagnostics(plan domain.RoutingPlan) []Diagnostic {
	partial := 0
	for _, warning := range plan.Warnings {
		if strings.HasPrefix(warning, "partial_coverage:") {
			partial++
		}
	}
	if partial == 0 {
		return nil
	}
	return []Diagnostic{{Code: "partial_coverage", Count: partial}}
}

// cloneRoutingPlan protects planner/application ownership across renderer
// callbacks. Several plan fields contain slices or pointers even though the
// renderer interface accepts the top-level value by value.
func cloneRoutingPlan(plan domain.RoutingPlan) domain.RoutingPlan {
	cloned := plan
	cloned.Lists = append([]string(nil), plan.Lists...)
	cloned.Rules = make([]domain.RouteRule, len(plan.Rules))
	for i := range plan.Rules {
		cloned.Rules[i] = cloneRouteRule(plan.Rules[i])
	}
	cloned.Excluded = make([]domain.Excluded, len(plan.Excluded))
	for i := range plan.Excluded {
		cloned.Excluded[i] = plan.Excluded[i]
		cloned.Excluded[i].Candidate = cloneRouteRule(plan.Excluded[i].Candidate)
		cloned.Excluded[i].ReasonCodes = append([]string(nil), plan.Excluded[i].ReasonCodes...)
		cloned.Excluded[i].ProvenanceRefs = append([]string(nil), plan.Excluded[i].ProvenanceRefs...)
	}
	cloned.Warnings = append([]string(nil), plan.Warnings...)
	cloned.Coverage = append([]domain.Coverage(nil), plan.Coverage...)
	cloned.Relations = append([]domain.Relation(nil), plan.Relations...)
	cloned.Sightings = append([]domain.Sighting(nil), plan.Sightings...)
	return cloned
}

func cloneRouteRule(rule domain.RouteRule) domain.RouteRule {
	cloned := rule
	cloned.Labels = append([]string(nil), rule.Labels...)
	cloned.ReasonCodes = append([]string(nil), rule.ReasonCodes...)
	cloned.ProvenanceRefs = append([]string(nil), rule.ProvenanceRefs...)
	if rule.ExpiresAt != nil {
		value := *rule.ExpiresAt
		cloned.ExpiresAt = &value
	}
	return cloned
}
