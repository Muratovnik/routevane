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

func scanList(row rowScanner) (application.List, error) {
	var list application.List
	var created, updated, refreshed, archived int64
	var interval string
	var failed int
	if err := row.Scan(&list.ID, &list.Name, &interval, &refreshed, &failed, &archived, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return list, application.ErrNotFound
		}
		return list, fmt.Errorf("read list: %w", err)
	}
	list.RefreshInterval = application.RefreshInterval(interval)
	if refreshed > 0 {
		list.LastRefreshedAt = unixNanos(refreshed)
	}
	list.LastRefreshFailed = failed == 1
	if archived > 0 {
		list.ArchivedAt = unixNanos(archived)
	}
	list.CreatedAt = unixNanos(created)
	list.UpdatedAt = unixNanos(updated)
	list.Services = make([]string, 0)
	list.Categories = make([]string, 0)
	list.Exclusions = make([]string, 0)
	return list, nil
}
func scanOutput(row rowScanner) (application.Output, error) {
	var o application.Output
	var created int64
	var listID, latest, previous, device sql.NullString
	if err := row.Scan(&o.ID, &listID, &o.TargetID, &o.ProfileKey, &o.RendererID, &o.RendererVersion, &o.TargetRevision, &created, &latest, &previous, &device); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return o, application.ErrNotFound
		}
		return o, fmt.Errorf("read output: %w", err)
	}
	o.ListID = listID.String
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
