package sqlite

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/Muratovnik/routevane/internal/application"
)

type rowScanner interface{ Scan(...any) error }

func scanProfile(row rowScanner) (application.Profile, error) {
	var profile application.Profile
	var created, updated, refreshed, archived int64
	var interval string
	var failed int
	if err := row.Scan(&profile.ID, &profile.Name, &interval, &refreshed, &failed, &archived, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return profile, application.ErrNotFound
		}
		return profile, fmt.Errorf("read profile: %w", err)
	}
	profile.RefreshInterval = application.RefreshInterval(interval)
	if refreshed > 0 {
		profile.LastRefreshedAt = unixNanos(refreshed)
	}
	profile.LastRefreshFailed = failed == 1
	if archived > 0 {
		profile.ArchivedAt = unixNanos(archived)
	}
	profile.CreatedAt = unixNanos(created)
	profile.UpdatedAt = unixNanos(updated)
	profile.Lists = make([]string, 0)
	profile.Categories = make([]string, 0)
	profile.Exclusions = make([]string, 0)
	return profile, nil
}
func scanOutput(row rowScanner) (application.Output, error) {
	var o application.Output
	var created int64
	var profileID, latest, previous, device sql.NullString
	if err := row.Scan(&o.ID, &profileID, &o.TargetID, &o.FormatKey, &o.RendererID, &o.RendererVersion, &o.TargetRevision, &created, &latest, &previous, &device, &o.FQDNGroupPrefix); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return o, application.ErrNotFound
		}
		return o, fmt.Errorf("read output: %w", err)
	}
	o.ProfileID = profileID.String
	o.CreatedAt = unixNanos(created)
	o.LatestArtifactID = latest.String
	o.PreviousArtifactID = previous.String
	o.DeviceID = device.String
	return o, nil
}
func scanPlanSnapshot(row rowScanner) (application.PlanSnapshotRecord, error) {
	var v application.PlanSnapshotRecord
	var cutoff, created int64
	if err := row.Scan(&v.ID, &v.OutputID, &v.RoutingPlanHash, &v.RoutingPlanJSON, &v.PolicyVersion, &v.CatalogRevision, &cutoff, &created, &v.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, application.ErrNotFound
		}
		return v, fmt.Errorf("read plan snapshot: %w", err)
	}
	v.ObservationCutoff = unixNanos(cutoff)
	v.CreatedAt = unixNanos(created)
	return v, nil
}
func scanArtifact(row rowScanner) (application.ArtifactBuildRecord, error) {
	var v application.ArtifactBuildRecord
	var created int64
	if err := row.Scan(&v.ID, &v.OutputID, &v.PlanSnapshotID, &v.RendererID, &v.RendererVersion, &v.ArtifactHash, &v.ArtifactPath, &v.SizeBytes, &v.ContentType, &created, &v.ValidationStatus, &v.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, application.ErrNotFound
		}
		return v, fmt.Errorf("read artifact build: %w", err)
	}
	v.ContentCreatedAt = unixNanos(created)
	return v, nil
}

func validID(value string) bool {
	if len(value) != 32 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
func validHash(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
