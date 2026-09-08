package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/planjson"
	"github.com/Muratovnik/routevane/internal/planner"
)

type PublishedBuild struct {
	Output   Output
	Snapshot PlanSnapshotRecord
	Artifact ArtifactBuildRecord
	Summary  PublishedBuildSummary
}

// PublishedBuildSummary contains the bounded information a caller needs to
// report a completed publication without exposing a RoutingPlan or its raw
// observation evidence.
type PublishedBuildSummary struct {
	RuleCount            int       `json:"rule_count"`
	PartialCoverage      bool      `json:"partial_coverage"`
	PartialCoverageCount int       `json:"partial_coverage_count"`
	ContentCreatedAt     time.Time `json:"content_created_at"`
	ValidationStatus     string    `json:"validation_status"`
	Status               string    `json:"status"`
	// DegradedSources names the sources whose observations were kept alive by
	// their grace window. It is a bounded diagnostic: the identity of a source,
	// never its configuration or the addresses it contributed.
	DegradedSources []string `json:"degraded_sources,omitempty"`
}

type SafePlanSnapshot struct {
	ID                string    `json:"id"`
	OutputID          string    `json:"output_id"`
	RoutingPlanHash   string    `json:"routing_plan_hash"`
	PolicyVersion     string    `json:"policy_version"`
	CatalogRevision   string    `json:"catalog_revision"`
	ObservationCutoff time.Time `json:"observation_cutoff"`
	CreatedAt         time.Time `json:"created_at"`
	Status            string    `json:"status"`
}

type SafeArtifactBuild struct {
	ID               string    `json:"id"`
	OutputID         string    `json:"output_id"`
	PlanSnapshotID   string    `json:"plan_snapshot_id"`
	RendererID       string    `json:"renderer_id"`
	RendererVersion  string    `json:"renderer_version"`
	ArtifactHash     string    `json:"artifact_hash"`
	SizeBytes        int64     `json:"size_bytes"`
	ContentType      string    `json:"content_type"`
	ContentCreatedAt time.Time `json:"content_created_at"`
	ValidationStatus string    `json:"validation_status"`
	Status           string    `json:"status"`
}

// SafePublishedBuild is the public application result. Its shape is intended
// for local presentation boundaries and has no bearer credential or raw plan.
type SafePublishedBuild struct {
	Output   Output                `json:"output"`
	Snapshot SafePlanSnapshot      `json:"snapshot"`
	Artifact SafeArtifactBuild     `json:"artifact"`
	Summary  PublishedBuildSummary `json:"summary"`
}

func (build PublishedBuild) SafeProjection() SafePublishedBuild {
	return SafePublishedBuild{
		Output: build.Output,
		Snapshot: SafePlanSnapshot{
			ID:                build.Snapshot.ID,
			OutputID:          build.Snapshot.OutputID,
			RoutingPlanHash:   build.Snapshot.RoutingPlanHash,
			PolicyVersion:     build.Snapshot.PolicyVersion,
			CatalogRevision:   build.Snapshot.CatalogRevision,
			ObservationCutoff: build.Snapshot.ObservationCutoff,
			CreatedAt:         build.Snapshot.CreatedAt,
			Status:            build.Snapshot.Status,
		},
		Artifact: SafeArtifactBuild{
			ID:               build.Artifact.ID,
			OutputID:         build.Artifact.OutputID,
			PlanSnapshotID:   build.Artifact.PlanSnapshotID,
			RendererID:       build.Artifact.RendererID,
			RendererVersion:  build.Artifact.RendererVersion,
			ArtifactHash:     build.Artifact.ArtifactHash,
			SizeBytes:        build.Artifact.SizeBytes,
			ContentType:      build.Artifact.ContentType,
			ContentCreatedAt: build.Artifact.ContentCreatedAt,
			ValidationStatus: build.Artifact.ValidationStatus,
			Status:           build.Artifact.Status,
		},
		Summary: build.Summary,
	}
}

// Build publishes one output from its profile's current composition. A profile edit
// is therefore visible in the next build without touching the output row.
func (s *PublicationService) Build(ctx context.Context, id string) (result PublishedBuild, resultErr error) {
	output, err := s.Output(ctx, id)
	if err != nil {
		return PublishedBuild{}, err
	}
	defer func() {
		if resultErr == nil {
			return
		}
		details := ClassifyBuildFailure(resultErr)
		completedAt := s.config.Clock.Now().UTC()
		if completedAt.IsZero() {
			return
		}
		_ = s.config.Store.RecordOutputAttempt(context.WithoutCancel(ctx), OutputAttempt{
			OutputID: output.ID, Status: "failed", Code: details.Code,
			ProjectedRules: details.ProjectedRules, MaximumRules: details.MaximumRules,
			CompletedAt: completedAt,
		})
	}()
	profile, err := s.config.Store.Profile(ctx, output.ProfileID)
	if err != nil {
		return PublishedBuild{}, err
	}
	// The guard is here and not only on the profile routes: a build addresses an
	// output, and an archived profile reached through one of its outputs would
	// republish just as effectively as one reached through itself.
	if err := profile.writable(); err != nil {
		return PublishedBuild{}, err
	}
	target, renderer, err := s.verifyTarget(output)
	if err != nil {
		return PublishedBuild{}, err
	}
	prepared, cutoff, err := s.prepareProfile(ctx, profile, target, renderer)
	if err != nil {
		return PublishedBuild{}, err
	}
	// Snapshot bytes are finalized before renderer invocation.
	snapshotJSON, err := planjson.Encode(prepared.Plan)
	if err != nil {
		return PublishedBuild{}, fmt.Errorf("encode plan snapshot: %w", err)
	}
	if err := planjson.Validate(snapshotJSON); err != nil {
		return PublishedBuild{}, fmt.Errorf("validate plan snapshot: %w", err)
	}
	payload, err := RenderPreparedOutput(prepared, renderer, output.ID, output.FQDNGroupPrefix)
	if err != nil {
		return PublishedBuild{}, err
	}
	file, err := s.config.Files.PutPublished(ctx, renderer.Descriptor(), payload)
	if err != nil {
		return PublishedBuild{}, fmt.Errorf("%w: store published candidate: %w", ErrPublicationStorage, err)
	}
	snapshotID, err := randomHex(s.config.Entropy, 16)
	if err != nil {
		return PublishedBuild{}, err
	}
	artifactID, err := randomHex(s.config.Entropy, 16)
	if err != nil {
		return PublishedBuild{}, err
	}
	now := s.config.Clock.Now().UTC()
	if now.IsZero() {
		return PublishedBuild{}, fmt.Errorf("clock returned zero time")
	}
	projectedRules, err := renderer.ProjectedRuleCount(cloneRoutingPlan(prepared.Plan))
	if err != nil || projectedRules < 0 {
		return PublishedBuild{}, fmt.Errorf("project published rule count: %w", err)
	}
	candidate := PublicationCandidate{
		Snapshot: PlanSnapshotRecord{ID: snapshotID, OutputID: id, RoutingPlanHash: prepared.Plan.SemanticHash, RoutingPlanJSON: snapshotJSON, PolicyVersion: prepared.Plan.PolicyVersion, CatalogRevision: prepared.Plan.CatalogRevision, ObservationCutoff: cutoff, CreatedAt: now, Status: "valid"},
		Artifact: ArtifactBuildRecord{ID: artifactID, OutputID: id, PlanSnapshotID: snapshotID, RendererID: renderer.ID(), RendererVersion: renderer.Version(), ArtifactHash: file.Hash, ArtifactPath: file.RelativePath, SizeBytes: file.Size, ContentType: renderer.Descriptor().ContentType, ContentCreatedAt: now, ValidationStatus: "valid", Status: "published"},
		Attempt:  OutputAttempt{OutputID: id, Status: "success", ProjectedRules: projectedRules, MaximumRules: target.Constraints.MaxRules, ArtifactID: artifactID, CompletedAt: now},
	}
	output, snapshot, artifact, err := s.config.Store.Publish(ctx, candidate)
	if err != nil {
		return PublishedBuild{}, fmt.Errorf("%w: commit publication: %w", ErrPublicationStorage, err)
	}
	partialCoverageCount := 0
	for _, diagnostic := range planDiagnostics(prepared.Plan) {
		if diagnostic.Code == "partial_coverage" {
			partialCoverageCount += diagnostic.Count
		}
	}
	degradedSources := degradedSourceNames(prepared.Plan)
	return PublishedBuild{
		Output:   output,
		Snapshot: snapshot,
		Artifact: artifact,
		Summary: PublishedBuildSummary{
			RuleCount:            len(prepared.Plan.Rules),
			PartialCoverage:      partialCoverageCount > 0,
			PartialCoverageCount: partialCoverageCount,
			ContentCreatedAt:     artifact.ContentCreatedAt,
			ValidationStatus:     artifact.ValidationStatus,
			Status:               artifact.Status,
			DegradedSources:      degradedSources,
		},
	}, nil
}

// prepareProfile is the shared profile-to-plan boundary used by both durable
// publication and an on-demand file export. A manual export must not create an
// output, subscription or artifact record just to select another renderer,
// but it must make exactly the same composition and preflight decisions.
func (s *PublicationService) prepareProfile(ctx context.Context, profile Profile, target domain.TargetDefinition, renderer Renderer) (PreparedPlan, time.Time, error) {
	return s.prepareProfileAt(ctx, profile, target, renderer, s.config.Clock.Now().UTC())
}

func (s *PublicationService) prepareProfileAt(ctx context.Context, profile Profile, target domain.TargetDefinition, renderer Renderer, cutoff time.Time) (PreparedPlan, time.Time, error) {
	// References are expanded, exclusions applied and duplicates dropped once
	// here, so every renderer sees the same current list set.
	lists := s.ResolvedLists(profile)
	if len(lists) == 0 {
		return PreparedPlan{}, time.Time{}, fmt.Errorf("profile resolves to no lists")
	}
	definitions := make([]domain.ListDefinition, 0, len(lists))
	revisions := make(map[string]map[string]string, len(lists))
	for _, listID := range lists {
		definition, ok := s.definition(listID)
		if !ok {
			return PreparedPlan{}, time.Time{}, fmt.Errorf("profile list unavailable")
		}
		if domains, overridden := profile.ListDomains[listID]; overridden {
			definition = withProfileListDomains(definition, profile.ID, domains)
		}
		definitions = append(definitions, definition)
		// The observation filter derives from the effective definition, so a
		// disabled source's stored sightings stop reaching plans and an added
		// feed's observations start, without touching what is stored.
		revisions[listID] = sourceRevisions(definition)
	}
	if cutoff.IsZero() {
		return PreparedPlan{}, time.Time{}, fmt.Errorf("clock returned zero time")
	}
	prepared, err := prepareLists(ctx, definitions, revisions, target, s.config.Store, renderer, cutoff, false)
	if err != nil {
		return PreparedPlan{}, time.Time{}, err
	}
	// Keep the source-profile relationship visible to forecasts even though the
	// finished plan below has already assigned every overlap to its winner.
	prepared.compositionOverlaps = forecastOverlaps(prepared.Plan)
	planner.ApplyListPriority(&prepared.Plan, lists)
	applyRouteLabels(&prepared.Plan, routeLabelsByList(definitions, s.mergedCategories()))
	planner.CanonicalizePlan(&prepared.Plan)
	prepared.Plan.SemanticHash = planner.SemanticHash(prepared.Plan, target)
	if err := PreflightPlan(prepared.Plan, target, renderer, cutoff); err != nil {
		return PreparedPlan{}, time.Time{}, err
	}
	return prepared, cutoff, nil
}

// withProfileListDomains replaces only catalog domain seeds. IP seeds and every
// observed/source rule keep their original lifecycle; the operator's override
// is a bounded profile-local correction, not a fork of the list catalog.
func withProfileListDomains(definition domain.ListDefinition, profileID string, domains []string) domain.ListDefinition {
	seeds := make([]domain.Seed, 0, len(definition.Seeds)+len(domains))
	componentID := ""
	for _, seed := range definition.Seeds {
		if seed.Kind.IsDomain() {
			if componentID == "" {
				componentID = seed.ComponentID
			}
			continue
		}
		seeds = append(seeds, seed)
	}
	if componentID == "" && len(definition.Components) > 0 {
		componentID = definition.Components[0].ID
	}
	for _, value := range domains {
		seeds = append(seeds, domain.Seed{
			Kind: domain.RuleDomainSuffix, Value: value, ComponentID: componentID,
			SourceID: "manual:list:" + profileID + ":" + value, SourceClass: domain.SourceManual,
		})
	}
	definition.Seeds = seeds
	return definition
}

// degradedSourceNames reads the source identities out of the plan's own
// warnings, so the reported set cannot disagree with the published plan.
func degradedSourceNames(plan domain.RoutingPlan) []string {
	prefix := planner.ReasonSourceDegraded + ":"
	names := make([]string, 0)
	for _, warning := range plan.Warnings {
		if !strings.HasPrefix(warning, prefix) {
			continue
		}
		parts := strings.Split(warning, ":")
		if len(parts) != 3 || parts[2] == "" {
			continue
		}
		names = append(names, parts[2])
	}
	if len(names) == 0 {
		return nil
	}
	return domain.StableStrings(names)
}
