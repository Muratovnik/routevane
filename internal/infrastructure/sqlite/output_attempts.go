package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Muratovnik/routevane/internal/application"
)

const outputAttemptSelect = `SELECT output_id,status,code,projected_rules,maximum_rules,coalesce(artifact_id,''),completed_at_ns FROM output_attempts`

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
	_, err := s.db.ExecContext(ctx, `INSERT INTO output_attempts(output_id,status,code,projected_rules,maximum_rules,artifact_id,completed_at_ns) VALUES(?,?,?,?,?,?,?)`, attempt.OutputID, attempt.Status, attempt.Code, attempt.ProjectedRules, attempt.MaximumRules, artifact, attempt.CompletedAt.UTC().UnixNano())
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
	if err := row.Scan(&attempt.OutputID, &attempt.Status, &attempt.Code, &attempt.ProjectedRules, &attempt.MaximumRules, &attempt.ArtifactID, &completedAt); err != nil {
		return application.OutputAttempt{}, err
	}
	attempt.CompletedAt = unixNanos(completedAt)
	return attempt, nil
}

func validOutputAttempt(attempt application.OutputAttempt) bool {
	if !validID(attempt.OutputID) || attempt.CompletedAt.IsZero() || attempt.ProjectedRules < 0 || attempt.MaximumRules < 0 {
		return false
	}
	if attempt.Status == "failed" {
		return attempt.Code != "" && len(attempt.Code) <= 64 && attempt.ArtifactID == ""
	}
	return attempt.Status == "success" && attempt.Code == "" && validID(attempt.ArtifactID)
}
