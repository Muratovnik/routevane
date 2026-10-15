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

const deploymentAttemptSelect = `SELECT id,artifact_id,device_id,request_hash,status,started_at_ns,coalesce(completed_at_ns,0),result_json,error_code FROM deployment_attempts`

func (s *Store) BeginDeploymentAttempt(ctx context.Context, proposed application.DeploymentAttempt) (application.DeploymentAttempt, bool, error) {
	if !validPendingDeploymentAttempt(proposed) {
		return application.DeploymentAttempt{}, false, fmt.Errorf("invalid deployment attempt")
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `INSERT INTO deployment_attempts(id,artifact_id,device_id,request_hash,status,started_at_ns,result_json,error_code) VALUES(?,?,?,?,?,?,'','') ON CONFLICT(id) DO NOTHING`,
		proposed.ID, proposed.ArtifactID, proposed.DeviceID, proposed.RequestHash, proposed.Status, proposed.StartedAt.UTC().UnixNano())
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
	result, err := s.db.ExecContext(ctx, `UPDATE deployment_attempts SET status=?,completed_at_ns=?,result_json=?,error_code=? WHERE id=? AND artifact_id=? AND device_id=? AND request_hash=? AND status='pending'`,
		completed.Status, completed.CompletedAt.UTC().UnixNano(), string(payload), completed.Error,
		completed.ID, completed.ArtifactID, completed.DeviceID, completed.RequestHash)
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

// LatestOutputDelivery is the newest succeeded send of any file of the output
// to the saved connection the output is bound to now. The files of an output are
// found through its builds, so another output's attempts, a send to any other
// connection (earlier binding or one-off), a failed or pending attempt and a
// send to an address typed by hand (which records no device) are never
// candidates. An output bound to no connection has no delivery.
func (s *Store) LatestOutputDelivery(ctx context.Context, outputID string) (application.OutputDelivery, error) {
	if !validID(outputID) {
		return application.OutputDelivery{}, application.ErrNotFound
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	var delivery application.OutputDelivery
	var completedAt int64
	err := s.db.QueryRowContext(ctx, `SELECT a.device_id,a.artifact_id,a.completed_at_ns FROM artifact_builds b
JOIN outputs o ON o.id=b.output_id
JOIN deployment_attempts a ON a.artifact_id=b.id AND a.status='succeeded' AND a.device_id<>'' AND a.device_id=o.device_id
WHERE b.output_id=? ORDER BY a.completed_at_ns DESC,a.id DESC LIMIT 1`, outputID).Scan(&delivery.DeviceID, &delivery.ArtifactID, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return application.OutputDelivery{}, application.ErrNotFound
	}
	if err != nil {
		return application.OutputDelivery{}, fmt.Errorf("read output delivery: %w", err)
	}
	delivery.CompletedAt = time.Unix(0, completedAt).UTC()
	return delivery, nil
}

func scanDeploymentAttempt(row interface{ Scan(...any) error }) (application.DeploymentAttempt, error) {
	var attempt application.DeploymentAttempt
	var status string
	var startedAt, completedAt int64
	var resultJSON string
	if err := row.Scan(&attempt.ID, &attempt.ArtifactID, &attempt.DeviceID, &attempt.RequestHash, &status, &startedAt, &completedAt, &resultJSON, &attempt.Error); err != nil {
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

// validAttemptDevice admits the empty string of a send to a typed address.
func validAttemptDevice(id string) bool { return id == "" || validID(id) }

func validPendingDeploymentAttempt(attempt application.DeploymentAttempt) bool {
	return validID(attempt.ID) && validID(attempt.ArtifactID) && validAttemptDevice(attempt.DeviceID) && validHash(attempt.RequestHash) &&
		attempt.Status == application.DeploymentAttemptPending && !attempt.StartedAt.IsZero() &&
		attempt.CompletedAt.IsZero() && attempt.Error == ""
}

func validCompletedDeploymentAttempt(attempt application.DeploymentAttempt) bool {
	if !validID(attempt.ID) || !validID(attempt.ArtifactID) || !validAttemptDevice(attempt.DeviceID) || !validHash(attempt.RequestHash) ||
		attempt.StartedAt.IsZero() || attempt.CompletedAt.IsZero() || attempt.CompletedAt.Before(attempt.StartedAt) || len(attempt.Error) > 64 {
		return false
	}
	if attempt.Status == application.DeploymentAttemptSucceeded {
		return attempt.Error == "" && attempt.Result.Applied
	}
	return attempt.Status == application.DeploymentAttemptFailed && attempt.Error != "" && !attempt.Result.Applied
}
