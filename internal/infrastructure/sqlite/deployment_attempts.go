package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

const deploymentAttemptSelect = `SELECT id,artifact_id,request_hash,status,started_at_ns,coalesce(completed_at_ns,0),result_json,error_code FROM deployment_attempts`

func (s *Store) BeginDeploymentAttempt(ctx context.Context, proposed application.DeploymentAttempt) (application.DeploymentAttempt, bool, error) {
	if !validPendingDeploymentAttempt(proposed) {
		return application.DeploymentAttempt{}, false, fmt.Errorf("invalid deployment attempt")
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `INSERT INTO deployment_attempts(id,artifact_id,request_hash,status,started_at_ns,result_json,error_code) VALUES(?,?,?,?,?,'','') ON CONFLICT(id) DO NOTHING`,
		proposed.ID, proposed.ArtifactID, proposed.RequestHash, proposed.Status, proposed.StartedAt.UTC().UnixNano())
	if err != nil {
		return application.DeploymentAttempt{}, false, fmt.Errorf("begin deployment attempt: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return application.DeploymentAttempt{}, false, fmt.Errorf("inspect deployment attempt claim: %w", err)
	}
	attempt, err := scanDeploymentAttempt(s.db.QueryRowContext(ctx, deploymentAttemptSelect+` WHERE id=?`, proposed.ID))
	if err != nil {
		return application.DeploymentAttempt{}, false, err
	}
	return attempt, rows == 1, nil
}

func (s *Store) CompleteDeploymentAttempt(ctx context.Context, completed application.DeploymentAttempt) error {
	if !validCompletedDeploymentAttempt(completed) {
		return fmt.Errorf("invalid completed deployment attempt")
	}
	payload, err := json.Marshal(completed.Result)
	if err != nil {
		return fmt.Errorf("encode deployment attempt result: %w", err)
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `UPDATE deployment_attempts SET status=?,completed_at_ns=?,result_json=?,error_code=? WHERE id=? AND artifact_id=? AND request_hash=? AND status='pending'`,
		completed.Status, completed.CompletedAt.UTC().UnixNano(), string(payload), completed.Error,
		completed.ID, completed.ArtifactID, completed.RequestHash)
	if err != nil {
		return fmt.Errorf("complete deployment attempt: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect deployment attempt completion: %w", err)
	}
	if rows != 1 {
		return application.ErrDeploymentAttemptMismatch
	}
	return nil
}

func (s *Store) DeploymentAttempt(ctx context.Context, id string) (application.DeploymentAttempt, error) {
	if !validID(id) {
		return application.DeploymentAttempt{}, application.ErrNotFound
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	return scanDeploymentAttempt(s.db.QueryRowContext(ctx, deploymentAttemptSelect+` WHERE id=?`, id))
}

func scanDeploymentAttempt(row interface{ Scan(...any) error }) (application.DeploymentAttempt, error) {
	var attempt application.DeploymentAttempt
	var status string
	var startedAt, completedAt int64
	var resultJSON string
	if err := row.Scan(&attempt.ID, &attempt.ArtifactID, &attempt.RequestHash, &status, &startedAt, &completedAt, &resultJSON, &attempt.Error); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.DeploymentAttempt{}, application.ErrNotFound
		}
		return application.DeploymentAttempt{}, fmt.Errorf("read deployment attempt: %w", err)
	}
	attempt.Status = application.DeploymentAttemptStatus(status)
	attempt.StartedAt = time.Unix(0, startedAt).UTC()
	if completedAt != 0 {
		attempt.CompletedAt = time.Unix(0, completedAt).UTC()
	}
	if resultJSON != "" {
		if err := json.Unmarshal([]byte(resultJSON), &attempt.Result); err != nil {
			return application.DeploymentAttempt{}, fmt.Errorf("decode deployment attempt result: %w", err)
		}
	}
	return attempt, nil
}

func validPendingDeploymentAttempt(attempt application.DeploymentAttempt) bool {
	return validID(attempt.ID) && validID(attempt.ArtifactID) && validHash(attempt.RequestHash) &&
		attempt.Status == application.DeploymentAttemptPending && !attempt.StartedAt.IsZero() &&
		attempt.CompletedAt.IsZero() && attempt.Error == ""
}

func validCompletedDeploymentAttempt(attempt application.DeploymentAttempt) bool {
	if !validID(attempt.ID) || !validID(attempt.ArtifactID) || !validHash(attempt.RequestHash) ||
		attempt.StartedAt.IsZero() || attempt.CompletedAt.IsZero() || attempt.CompletedAt.Before(attempt.StartedAt) || len(attempt.Error) > 64 {
		return false
	}
	if attempt.Status == application.DeploymentAttemptSucceeded {
		return attempt.Error == "" && attempt.Result.Applied
	}
	return attempt.Status == application.DeploymentAttemptFailed && attempt.Error != "" && !attempt.Result.Applied
}
