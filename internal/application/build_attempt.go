package application

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Muratovnik/routevane/internal/domain"
	"github.com/Muratovnik/routevane/internal/planner"
)

const (
	BuildFailureRuleLimit           = "rule_limit"
	BuildFailurePartialCoverage     = "partial_coverage"
	BuildFailureTargetChanged       = "target_changed"
	BuildFailureProfileArchived     = "list_archived"
	BuildFailureProfileChanged      = "profile_changed"
	BuildFailureSuperseded          = "build_superseded"
	BuildFailureSourceUnavailable   = "source_unavailable"
	BuildFailureFormatMismatch      = "profile_mismatch"
	BuildFailurePreflight           = "preflight_failed"
	BuildFailureArtifactUnavailable = "artifact_unavailable"
	BuildFailureStorage             = "storage_failed"
	BuildFailureUnknown             = "build_failed"
)

// The bounds of the lists a refused build names. A profile resolves to far fewer
// lists than this and a list's title is a line, so the bounds protect the stored
// attempt and the response from a hostile catalog and never cut a real refusal.
const (
	maxUncoveredLists  = 256
	maxUncoveredTitle  = 200
	uncoveredTitleDots = "…"
)

// UncoveredList is one list whose required part a build could not cover: its
// identity, for a link to it, and the title the operator knows it by.
type UncoveredList struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// PartialCoverageError is ErrPartialCoverage with the lists that caused it. The
// planner names identities; the titles come from the definitions the build was
// given, so the refusal can be read without a second request. It unwraps to the
// sentinel, so every existing errors.Is keeps answering.
type PartialCoverageError struct {
	Lists []UncoveredList
	cause error
}

func (e *PartialCoverageError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%v: %v", ErrPartialCoverage, e.cause)
	}
	return fmt.Sprintf("%v: %d list(s)", ErrPartialCoverage, len(e.Lists))
}

func (e *PartialCoverageError) Unwrap() error { return ErrPartialCoverage }

// partialCoverage turns the planner's refusal into the application's, naming
// each uncovered list by the title its definition carries. A refusal that does
// not carry its lists is still a partial coverage, only without them.
func partialCoverage(err error, definitions []domain.ListDefinition) error {
	var missing *planner.RequiredCoverageError
	if !errors.As(err, &missing) {
		return &PartialCoverageError{cause: err}
	}
	titles := make(map[string]string, len(definitions))
	for _, definition := range definitions {
		titles[definition.ID] = definition.Title
	}
	return &PartialCoverageError{Lists: uncoveredLists(missing.Lists, titles), cause: err}
}

// uncoveredFromCoverage names the lists of a plan's incomplete components when
// only the plan is at hand, as it is for the final preflight. It has no titles,
// so a list is named by its identity.
func uncoveredFromCoverage(coverage []domain.Coverage) error {
	var ids []string
	for _, item := range coverage {
		if !item.Complete {
			ids = append(ids, item.ListID)
		}
	}
	return &PartialCoverageError{Lists: uncoveredLists(domain.StableStrings(ids), nil)}
}

func uncoveredLists(ids []string, titles map[string]string) []UncoveredList {
	if len(ids) > maxUncoveredLists {
		ids = ids[:maxUncoveredLists]
	}
	lists := make([]UncoveredList, len(ids))
	for i, id := range ids {
		title := strings.TrimSpace(titles[id])
		if title == "" {
			title = id
		}
		if runes := []rune(title); len(runes) > maxUncoveredTitle {
			title = string(runes[:maxUncoveredTitle-1]) + uncoveredTitleDots
		}
		lists[i] = UncoveredList{ID: id, Title: title}
	}
	return lists
}

// BuildFailureDetails is stable transport/persistence data derived from an
// internal error chain. Raw error strings stay in local logs.
type BuildFailureDetails struct {
	Code           string `json:"code"`
	ProjectedRules int    `json:"projected_rules,omitempty"`
	MaximumRules   int    `json:"maximum_rules,omitempty"`
	// UncoveredLists names the lists behind a partial_coverage failure, sorted
	// by identity. Every other failure leaves it empty.
	UncoveredLists []UncoveredList `json:"uncovered_lists,omitempty"`
}

func ClassifyBuildFailure(err error) BuildFailureDetails {
	var limit *RuleLimitError
	var partial *PartialCoverageError
	switch {
	case errors.As(err, &partial):
		return BuildFailureDetails{Code: BuildFailurePartialCoverage, UncoveredLists: partial.Lists}
	case errors.As(err, &limit):
		return BuildFailureDetails{Code: BuildFailureRuleLimit, ProjectedRules: limit.Projected, MaximumRules: limit.Maximum}
	case errors.Is(err, ErrRuleLimit):
		return BuildFailureDetails{Code: BuildFailureRuleLimit}
	case errors.Is(err, ErrPartialCoverage):
		return BuildFailureDetails{Code: BuildFailurePartialCoverage}
	case errors.Is(err, ErrTargetChanged):
		return BuildFailureDetails{Code: BuildFailureTargetChanged}
	case errors.Is(err, ErrProfileArchived):
		return BuildFailureDetails{Code: BuildFailureProfileArchived}
	case errors.Is(err, ErrProfileChanged):
		return BuildFailureDetails{Code: BuildFailureProfileChanged}
	case errors.Is(err, ErrBuildSuperseded):
		return BuildFailureDetails{Code: BuildFailureSuperseded}
	case errors.Is(err, ErrSourceFailed), errors.Is(err, ErrSourceDegraded):
		return BuildFailureDetails{Code: BuildFailureSourceUnavailable}
	case errors.Is(err, ErrFormatMismatch):
		return BuildFailureDetails{Code: BuildFailureFormatMismatch}
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
