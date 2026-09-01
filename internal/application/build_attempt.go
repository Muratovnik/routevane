package application

import "errors"

const (
	BuildFailureRuleLimit           = "rule_limit"
	BuildFailurePartialCoverage     = "partial_coverage"
	BuildFailureTargetChanged       = "target_changed"
	BuildFailureListArchived        = "list_archived"
	BuildFailureSourceUnavailable   = "source_unavailable"
	BuildFailureProfileMismatch     = "profile_mismatch"
	BuildFailurePreflight           = "preflight_failed"
	BuildFailureArtifactUnavailable = "artifact_unavailable"
	BuildFailureStorage             = "storage_failed"
	BuildFailureUnknown             = "build_failed"
)

// BuildFailureDetails is stable transport/persistence data derived from an
// internal error chain. Raw error strings stay in local logs.
type BuildFailureDetails struct {
	Code           string `json:"code"`
	ProjectedRules int    `json:"projected_rules,omitempty"`
	MaximumRules   int    `json:"maximum_rules,omitempty"`
}

func ClassifyBuildFailure(err error) BuildFailureDetails {
	var limit *RuleLimitError
	switch {
	case errors.As(err, &limit):
		return BuildFailureDetails{Code: BuildFailureRuleLimit, ProjectedRules: limit.Projected, MaximumRules: limit.Maximum}
	case errors.Is(err, ErrRuleLimit):
		return BuildFailureDetails{Code: BuildFailureRuleLimit}
	case errors.Is(err, ErrPartialCoverage):
		return BuildFailureDetails{Code: BuildFailurePartialCoverage}
	case errors.Is(err, ErrTargetChanged):
		return BuildFailureDetails{Code: BuildFailureTargetChanged}
	case errors.Is(err, ErrListArchived):
		return BuildFailureDetails{Code: BuildFailureListArchived}
	case errors.Is(err, ErrSourceFailed), errors.Is(err, ErrSourceDegraded):
		return BuildFailureDetails{Code: BuildFailureSourceUnavailable}
	case errors.Is(err, ErrProfileMismatch):
		return BuildFailureDetails{Code: BuildFailureProfileMismatch}
	case errors.Is(err, ErrPreflight):
		return BuildFailureDetails{Code: BuildFailurePreflight}
	case errors.Is(err, ErrArtifactUnavailable):
		return BuildFailureDetails{Code: BuildFailureArtifactUnavailable}
	case errors.Is(err, ErrPublicationStorage):
		return BuildFailureDetails{Code: BuildFailureStorage}
	default:
		return BuildFailureDetails{Code: BuildFailureUnknown}
	}
}
