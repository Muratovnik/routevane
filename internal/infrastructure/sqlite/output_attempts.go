package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

const outputAttemptSelect = `SELECT output_id,status,code,projected_rules,maximum_rules,coalesce(artifact_id,''),completed_at_ns,uncovered_lists_json FROM output_attempts`

func (s *Store) RecordOutputAttempt(ctx context.Context, attempt application.OutputAttempt) error {
	if !validOutputAttempt(attempt) {
		return fmt.Errorf("invalid output attempt")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	var artifact any
	if attempt.ArtifactID != "" {
		artifact = attempt.ArtifactID
	}
	uncovered, err := json.Marshal(append([]application.UncoveredList{}, attempt.UncoveredLists...))
	if err != nil {
		return fmt.Errorf("encode uncovered lists: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO output_attempts(output_id,status,code,projected_rules,maximum_rules,artifact_id,completed_at_ns,uncovered_lists_json) VALUES(?,?,?,?,?,?,?,?)`, attempt.OutputID, attempt.Status, attempt.Code, attempt.ProjectedRules, attempt.MaximumRules, artifact, attempt.CompletedAt.UTC().UnixNano(), string(uncovered))
	if err != nil {
		return fmt.Errorf("record output attempt: %w", err)
	}
	return nil
}

func (s *Store) LatestOutputAttempt(ctx context.Context, outputID string) (application.OutputAttempt, error) {
	if !validID(outputID) {
		return application.OutputAttempt{}, application.ErrNotFound
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	attempt, err := scanOutputAttempt(s.db.QueryRowContext(ctx, outputAttemptSelect+` WHERE output_id=? ORDER BY completed_at_ns DESC,id DESC LIMIT 1`, outputID))
	if errors.Is(err, sql.ErrNoRows) {
		return application.OutputAttempt{}, application.ErrNotFound
	}
	return attempt, err
}

func scanOutputAttempt(row interface{ Scan(...any) error }) (application.OutputAttempt, error) {
	var attempt application.OutputAttempt
	var completedAt int64
	var uncovered string
	if err := row.Scan(&attempt.OutputID, &attempt.Status, &attempt.Code, &attempt.ProjectedRules, &attempt.MaximumRules, &attempt.ArtifactID, &completedAt, &uncovered); err != nil {
		return application.OutputAttempt{}, err
	}
	attempt.CompletedAt = unixNanos(completedAt)
	var lists []application.UncoveredList
	if err := json.Unmarshal([]byte(uncovered), &lists); err != nil {
		return application.OutputAttempt{}, fmt.Errorf("read uncovered lists: %w", err)
	}
	if len(lists) > 0 {
		attempt.UncoveredLists = lists
	}
	return attempt, nil
}

func validOutputAttempt(attempt application.OutputAttempt) bool {
	if !validID(attempt.OutputID) || attempt.CompletedAt.IsZero() || attempt.ProjectedRules < 0 || attempt.MaximumRules < 0 {
		return false
	}
	if attempt.Status == "failed" {
		return attempt.Code != "" && len(attempt.Code) <= 64 && attempt.ArtifactID == "" && validUncoveredLists(attempt)
	}
	return attempt.Status == "success" && attempt.Code == "" && validID(attempt.ArtifactID) && len(attempt.UncoveredLists) == 0
}

// validUncoveredLists accepts the lists only on the failure that names them, and
// only as the bounded identities and titles the application produces.
func validUncoveredLists(attempt application.OutputAttempt) bool {
	if len(attempt.UncoveredLists) == 0 {
		return true
	}
	if attempt.Code != application.BuildFailurePartialCoverage || len(attempt.UncoveredLists) > maxStoredUncoveredLists {
		return false
	}
	for _, list := range attempt.UncoveredLists {
		if domain.ValidateSlug(list.ID) != nil || list.Title == "" || utf8.RuneCountInString(list.Title) > maxStoredUncoveredTitle || !utf8.ValidString(list.Title) {
			return false
		}
	}
	return true
}

// The stored bounds are the application's own, restated here because the schema
// must not trust a caller to have applied them.
const (
	maxStoredUncoveredLists = 256
	maxStoredUncoveredTitle = 200
)
