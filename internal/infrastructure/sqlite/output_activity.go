package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

// maxActivityOutputs bounds one OutputActivities read to the outputs a full
// profile page can hold: profileLimit profiles of at most 64 formats each.
const maxActivityOutputs = profileLimit * 64

// OutputActivities reads the bound connection's name, the moment the current
// file became current, and the newest succeeded and failed send to a saved
// connection for every given live output, in two queries whatever their number.
//
// Successful build attempts are appended in the same transaction that moves an
// output's latest pointer and are never updated or deleted, so their row order
// is the order in which files became current. The current file became current
// at its first success after the last success that published another file.
func (s *Store) OutputActivities(ctx context.Context, outputIDs []string) (map[string]application.OutputActivity, error) {
	if len(outputIDs) > maxActivityOutputs {
		return nil, fmt.Errorf("invalid output activity query")
	}
	for _, id := range outputIDs {
		if !validID(id) {
			return nil, fmt.Errorf("invalid output activity query")
		}
	}
	activities := make(map[string]application.OutputActivity, len(outputIDs))
	if len(outputIDs) == 0 {
		return activities, nil
	}
	// A JSON array keeps the set a bound value in a fixed query.
	ids, err := json.Marshal(outputIDs)
	if err != nil {
		return nil, err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	if err := s.readOutputStates(ctx, string(ids), activities); err != nil {
		return nil, err
	}
	if err := s.readOutputSends(ctx, string(ids), activities); err != nil {
		return nil, err
	}
	return activities, nil
}

func (s *Store) readOutputStates(ctx context.Context, ids string, activities map[string]application.OutputActivity) error {
	rows, err := s.db.QueryContext(ctx, `SELECT o.id, coalesce(d.name,''), coalesce((
		SELECT current.completed_at_ns FROM output_attempts AS current
		WHERE current.output_id=o.id AND current.status='success' AND current.artifact_id=o.latest_artifact_id
		AND current.id > coalesce((
			SELECT max(other.id) FROM output_attempts AS other
			WHERE other.output_id=o.id AND other.status='success' AND other.artifact_id<>o.latest_artifact_id), 0)
		ORDER BY current.id ASC LIMIT 1), 0)
	FROM json_each(?) AS wanted
	JOIN outputs AS o ON o.id=wanted.value AND o.removed_at_ns=0
	LEFT JOIN devices AS d ON d.id=o.device_id`, ids)
	if err != nil {
		return fmt.Errorf("read output activity: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var activity application.OutputActivity
		var builtAt int64
		if err := rows.Scan(&id, &activity.DeviceName, &builtAt); err != nil {
			return fmt.Errorf("read output activity: %w", err)
		}
		if builtAt != 0 {
			activity.BuiltAt = time.Unix(0, builtAt).UTC()
		}
		activities[id] = activity
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read output activity: %w", err)
	}
	return nil
}

// readOutputSends ranks each output's finished sends to saved connections by
// outcome and keeps the newest of each. The files of an output are found
// through its builds, as LatestOutputDelivery finds them.
func (s *Store) readOutputSends(ctx context.Context, ids string, activities map[string]application.OutputActivity) error {
	rows, err := s.db.QueryContext(ctx, `SELECT output_id, device_id, artifact_id, status, completed_at_ns, error_code, result_json FROM (
		SELECT b.output_id, a.device_id, a.artifact_id, a.status, a.completed_at_ns, a.error_code,
			CASE WHEN a.status='failed' THEN a.result_json ELSE '' END AS result_json,
			row_number() OVER (PARTITION BY b.output_id, a.status ORDER BY a.completed_at_ns DESC, a.id DESC) AS position
		FROM json_each(?) AS wanted
		JOIN outputs AS o ON o.id=wanted.value AND o.removed_at_ns=0
		JOIN artifact_builds AS b ON b.output_id=o.id
		JOIN deployment_attempts AS a ON a.artifact_id=b.id
		WHERE a.status IN ('succeeded','failed') AND a.device_id<>''
	) WHERE position=1`, ids)
	if err != nil {
		return fmt.Errorf("read output sends: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var outputID, deviceID, artifactID, status, code, resultJSON string
		var completedAt int64
		if err := rows.Scan(&outputID, &deviceID, &artifactID, &status, &completedAt, &code, &resultJSON); err != nil {
			return fmt.Errorf("read output sends: %w", err)
		}
		activity := activities[outputID]
		at := time.Unix(0, completedAt).UTC()
		if status == string(application.DeploymentAttemptSucceeded) {
			activity.LatestDelivery = &application.OutputDelivery{DeviceID: deviceID, ArtifactID: artifactID, CompletedAt: at}
		} else {
			var result application.DeployResult
			if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
				return fmt.Errorf("decode deployment attempt result: %w", err)
			}
			activity.LatestFailure = &application.OutputDeliveryFailure{DeviceID: deviceID, Code: code, Step: result.FailedStep(), CompletedAt: at}
		}
		activities[outputID] = activity
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read output sends: %w", err)
	}
	// A failure is news only until a send succeeds after it.
	for id, activity := range activities {
		if activity.LatestFailure != nil && activity.LatestDelivery != nil && !activity.LatestFailure.CompletedAt.After(activity.LatestDelivery.CompletedAt) {
			activity.LatestFailure = nil
			activities[id] = activity
		}
	}
	return nil
}
