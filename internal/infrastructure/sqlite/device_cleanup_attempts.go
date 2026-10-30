package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/Muratovnik/routevane/internal/application"
)

// The device cleanup journal (ADR 0060) records each removal of what a retired
// format delivered, the way deployment_attempts records each send: claimed
// before the first device effect, completed once, and never changed again.

const deviceCleanupAttemptSelect = `SELECT id,output_id,device_id,endpoint,status,started_at_ns,coalesce(completed_at_ns,0),result_json,error_code FROM device_cleanup_attempts`

func (s *Store) BeginDeviceCleanupAttempt(ctx context.Context, proposed application.DeviceCleanupAttempt) error {
	if !validDeviceCleanupIdentity(proposed) || proposed.Status != application.DeploymentAttemptPending ||
		!proposed.CompletedAt.IsZero() || proposed.Error != "" {
		return fmt.Errorf("invalid device cleanup attempt")
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO device_cleanup_attempts(id,output_id,device_id,endpoint,status,started_at_ns,result_json,error_code) VALUES(?,?,?,?,?,?,'','')`,
		proposed.ID, proposed.OutputID, proposed.DeviceID, proposed.Endpoint, proposed.Status, proposed.StartedAt.UTC().UnixNano()); err != nil {
		return fmt.Errorf("begin device cleanup attempt: %w", err)
	}
	return nil
}

func (s *Store) CompleteDeviceCleanupAttempt(ctx context.Context, completed application.DeviceCleanupAttempt) error {
	if !validDeviceCleanupIdentity(completed) || completed.CompletedAt.IsZero() || completed.CompletedAt.Before(completed.StartedAt) || len(completed.Error) > 64 {
		return fmt.Errorf("invalid completed device cleanup attempt")
	}
	switch completed.Status {
	case application.DeploymentAttemptSucceeded:
		if completed.Error != "" || !completed.Result.Applied {
			return fmt.Errorf("invalid completed device cleanup attempt")
		}
	case application.DeploymentAttemptFailed:
		if completed.Error == "" || completed.Result.Applied {
			return fmt.Errorf("invalid completed device cleanup attempt")
		}
	default:
		return fmt.Errorf("invalid completed device cleanup attempt")
	}
	payload, err := json.Marshal(completed.Result)
	if err != nil {
		return fmt.Errorf("encode device cleanup attempt result: %w", err)
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `UPDATE device_cleanup_attempts SET status=?,completed_at_ns=?,result_json=?,error_code=? WHERE id=? AND output_id=? AND device_id=? AND endpoint=? AND status='pending'`,
		completed.Status, completed.CompletedAt.UTC().UnixNano(), string(payload), completed.Error,
		completed.ID, completed.OutputID, completed.DeviceID, completed.Endpoint)
	if err != nil {
		return fmt.Errorf("complete device cleanup attempt: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect device cleanup attempt completion: %w", err)
	}
	if rows != 1 {
		return application.ErrDeploymentAttemptMismatch
	}
	return nil
}

// DeviceCleanupAttempts lists one format's recorded removals, oldest first.
func (s *Store) DeviceCleanupAttempts(ctx context.Context, outputID string) ([]application.DeviceCleanupAttempt, error) {
	if !validID(outputID) {
		return nil, application.ErrNotFound
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, deviceCleanupAttemptSelect+` WHERE output_id=? ORDER BY started_at_ns,id`, outputID)
	if err != nil {
		return nil, fmt.Errorf("read device cleanup attempts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	attempts := make([]application.DeviceCleanupAttempt, 0)
	for rows.Next() {
		attempt, err := scanDeviceCleanupAttempt(rows)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read device cleanup attempts: %w", err)
	}
	return attempts, nil
}

func scanDeviceCleanupAttempt(row interface{ Scan(...any) error }) (application.DeviceCleanupAttempt, error) {
	var attempt application.DeviceCleanupAttempt
	var status, resultJSON string
	var startedAt, completedAt int64
	if err := row.Scan(&attempt.ID, &attempt.OutputID, &attempt.DeviceID, &attempt.Endpoint, &status, &startedAt, &completedAt, &resultJSON, &attempt.Error); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.DeviceCleanupAttempt{}, application.ErrNotFound
		}
		return application.DeviceCleanupAttempt{}, fmt.Errorf("read device cleanup attempt: %w", err)
	}
	attempt.Status = application.DeploymentAttemptStatus(status)
	attempt.StartedAt = time.Unix(0, startedAt).UTC()
	if completedAt != 0 {
		attempt.CompletedAt = time.Unix(0, completedAt).UTC()
	}
	if resultJSON != "" {
		if err := json.Unmarshal([]byte(resultJSON), &attempt.Result); err != nil {
			return application.DeviceCleanupAttempt{}, fmt.Errorf("decode device cleanup attempt result: %w", err)
		}
	}
	return attempt, nil
}

func validDeviceCleanupIdentity(attempt application.DeviceCleanupAttempt) bool {
	if !validID(attempt.ID) || !validID(attempt.OutputID) || !validID(attempt.DeviceID) || attempt.StartedAt.IsZero() {
		return false
	}
	if attempt.Endpoint == "" || len(attempt.Endpoint) > 512 || !utf8.ValidString(attempt.Endpoint) {
		return false
	}
	for _, r := range attempt.Endpoint {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
