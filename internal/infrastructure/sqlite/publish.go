package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Muratovnik/routevane/internal/application"
)

func (s *Store) Publish(ctx context.Context, candidate application.PublicationCandidate) (application.Output, application.PlanSnapshotRecord, application.ArtifactBuildRecord, error) {
	if err := validatePublication(candidate); err != nil {
		return application.Output{}, application.PlanSnapshotRecord{}, application.ArtifactBuildRecord{}, err
	}
	if err := s.preparePublication(); err != nil {
		return application.Output{}, application.PlanSnapshotRecord{}, application.ArtifactBuildRecord{}, err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return application.Output{}, application.PlanSnapshotRecord{}, application.ArtifactBuildRecord{}, fmt.Errorf("begin publication: %w", err)
	}
	fail := func(cause error) (application.Output, application.PlanSnapshotRecord, application.ArtifactBuildRecord, error) {
		_ = tx.Rollback()
		return application.Output{}, application.PlanSnapshotRecord{}, application.ArtifactBuildRecord{}, cause
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM outputs WHERE id=?`, candidate.Snapshot.OutputID).Scan(&exists); err != nil || exists != 1 {
		return fail(application.ErrNotFound)
	}

	snapshot := candidate.Snapshot
	var existingID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM plan_snapshots WHERE output_id=? AND routing_plan_json=?`, snapshot.OutputID, snapshot.RoutingPlanJSON).Scan(&existingID)
	if err == nil {
		snapshot, err = scanPlanSnapshot(tx.QueryRowContext(ctx, snapshotSelect+` WHERE id=?`, existingID))
		if err != nil {
			return fail(err)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fail(fmt.Errorf("find plan snapshot: %w", err))
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO plan_snapshots(id,output_id,routing_plan_hash,routing_plan_json,policy_version,catalog_revision,observation_cutoff_ns,created_at_ns,status) VALUES(?,?,?,?,?,?,?,?,?)`, snapshot.ID, snapshot.OutputID, snapshot.RoutingPlanHash, snapshot.RoutingPlanJSON, snapshot.PolicyVersion, snapshot.CatalogRevision, snapshot.ObservationCutoff.UTC().UnixNano(), snapshot.CreatedAt.UTC().UnixNano(), snapshot.Status)
		if err != nil {
			return fail(fmt.Errorf("insert immutable plan snapshot: %w", err))
		}
	}

	artifact := candidate.Artifact
	artifact.PlanSnapshotID = snapshot.ID
	err = tx.QueryRowContext(ctx, `SELECT id FROM artifact_builds WHERE plan_snapshot_id=? AND renderer_id=? AND renderer_version=? AND artifact_hash=?`, artifact.PlanSnapshotID, artifact.RendererID, artifact.RendererVersion, artifact.ArtifactHash).Scan(&existingID)
	if err == nil {
		artifact, err = scanArtifact(tx.QueryRowContext(ctx, artifactSelect+` WHERE id=?`, existingID))
		if err != nil {
			return fail(err)
		}
		if artifact.ArtifactPath != candidate.Artifact.ArtifactPath || artifact.SizeBytes != candidate.Artifact.SizeBytes {
			return fail(fmt.Errorf("immutable artifact identity mismatch"))
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fail(fmt.Errorf("find artifact build: %w", err))
	} else {
		var earliest sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT min(content_created_at_ns) FROM artifact_builds WHERE renderer_id=? AND renderer_version=? AND artifact_hash=? AND artifact_path=?`, artifact.RendererID, artifact.RendererVersion, artifact.ArtifactHash, artifact.ArtifactPath).Scan(&earliest); err != nil {
			return fail(fmt.Errorf("read artifact content time: %w", err))
		}
		if earliest.Valid && earliest.Int64 < artifact.ContentCreatedAt.UnixNano() {
			artifact.ContentCreatedAt = unixNanos(earliest.Int64)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO artifact_builds(id,output_id,plan_snapshot_id,renderer_id,renderer_version,artifact_hash,artifact_path,size_bytes,content_type,content_created_at_ns,validation_status,status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, artifact.ID, artifact.OutputID, artifact.PlanSnapshotID, artifact.RendererID, artifact.RendererVersion, artifact.ArtifactHash, artifact.ArtifactPath, artifact.SizeBytes, artifact.ContentType, artifact.ContentCreatedAt.UTC().UnixNano(), artifact.ValidationStatus, artifact.Status)
		if err != nil {
			return fail(fmt.Errorf("insert immutable artifact build: %w", err))
		}
	}
	var latest sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT latest_artifact_id FROM outputs WHERE id=?`, artifact.OutputID).Scan(&latest); err != nil {
		return fail(fmt.Errorf("read current publication: %w", err))
	}
	if !latest.Valid || latest.String != artifact.ID {
		var previous any
		if latest.Valid {
			previous = latest.String
		}
		if _, err := tx.ExecContext(ctx, `UPDATE outputs SET previous_artifact_id=?,latest_artifact_id=? WHERE id=?`, previous, artifact.ID, artifact.OutputID); err != nil {
			return fail(fmt.Errorf("advance publication pointers: %w", err))
		}
	}
	attempt := candidate.Attempt
	attempt.ArtifactID = artifact.ID
	if !validOutputAttempt(attempt) {
		return fail(fmt.Errorf("invalid successful output attempt"))
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO output_attempts(output_id,status,code,projected_rules,maximum_rules,artifact_id,completed_at_ns) VALUES(?,?,?,?,?,?,?)`, attempt.OutputID, attempt.Status, attempt.Code, attempt.ProjectedRules, attempt.MaximumRules, attempt.ArtifactID, attempt.CompletedAt.UTC().UnixNano()); err != nil {
		return fail(fmt.Errorf("record successful output attempt: %w", err))
	}
	output, err := scanOutput(tx.QueryRowContext(ctx, outputSelect+` WHERE id=?`, artifact.OutputID))
	if err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return application.Output{}, application.PlanSnapshotRecord{}, application.ArtifactBuildRecord{}, fmt.Errorf("commit publication: %w", err)
	}
	return output, snapshot, artifact, nil
}

func (s *Store) preparePublication() error {
	if err := s.protectFiles(); err != nil {
		return fmt.Errorf("prepare publication database files: %w", err)
	}
	if s.publicationPreflight != nil {
		if err := s.publicationPreflight(); err != nil {
			return fmt.Errorf("prepare publication transaction: %w", err)
		}
	}
	return nil
}

const snapshotSelect = `SELECT id,output_id,routing_plan_hash,routing_plan_json,policy_version,catalog_revision,observation_cutoff_ns,created_at_ns,status FROM plan_snapshots`
const artifactSelect = `SELECT id,output_id,plan_snapshot_id,renderer_id,renderer_version,artifact_hash,artifact_path,size_bytes,content_type,content_created_at_ns,validation_status,status FROM artifact_builds`

func (s *Store) PlanSnapshot(ctx context.Context, id string) (application.PlanSnapshotRecord, error) {
	if !validID(id) {
		return application.PlanSnapshotRecord{}, application.ErrNotFound
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	return scanPlanSnapshot(s.db.QueryRowContext(ctx, snapshotSelect+` WHERE id=?`, id))
}
func (s *Store) ArtifactBuild(ctx context.Context, id string) (application.ArtifactBuildRecord, error) {
	if !validID(id) {
		return application.ArtifactBuildRecord{}, application.ErrNotFound
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	return scanArtifact(s.db.QueryRowContext(ctx, artifactSelect+` WHERE id=?`, id))
}

func validatePublication(c application.PublicationCandidate) error {
	s, a := c.Snapshot, c.Artifact
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(a.ArtifactPath)))
	if !validID(s.ID) || !validID(s.OutputID) || !validID(a.ID) || a.OutputID != s.OutputID || a.PlanSnapshotID != s.ID || !validHash(s.RoutingPlanHash) || !validHash(a.ArtifactHash) || len(s.RoutingPlanJSON) == 0 || len(s.RoutingPlanJSON) > 4<<20 || s.PolicyVersion == "" || s.CatalogRevision == "" || s.ObservationCutoff.IsZero() || s.CreatedAt.IsZero() || s.Status != "valid" || a.RendererID == "" || a.RendererVersion == "" || a.ArtifactPath == "" || clean != a.ArtifactPath || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || a.SizeBytes <= 0 || a.SizeBytes > 4<<20 || a.ContentType == "" || a.ContentCreatedAt.IsZero() || a.ValidationStatus != "valid" || a.Status != "published" || c.Attempt.OutputID != a.OutputID || c.Attempt.Status != "success" || c.Attempt.Code != "" || c.Attempt.ProjectedRules < 0 || c.Attempt.MaximumRules < 0 || c.Attempt.CompletedAt.IsZero() {
		return fmt.Errorf("invalid publication candidate")
	}
	return nil
}

// PublicationCounts is a narrow acceptance-test aid.
func (s *Store) PublicationCounts(ctx context.Context) (int, int, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	var snapshots, artifacts int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM plan_snapshots`).Scan(&snapshots); err != nil {
		return 0, 0, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM artifact_builds`).Scan(&artifacts); err != nil {
		return 0, 0, err
	}
	return snapshots, artifacts, nil
}
