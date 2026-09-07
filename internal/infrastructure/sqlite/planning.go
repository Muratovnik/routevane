package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

func (s *Store) ReadPlanningSnapshot(ctx context.Context, listID string, activeRevisions map[string]string, formatKey string, cutoff time.Time) (PlanningSnapshot, error) {
	if domain.ValidateSlug(listID) != nil || formatKey == "" || cutoff.IsZero() {
		return PlanningSnapshot{}, fmt.Errorf("invalid planning snapshot request")
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return PlanningSnapshot{}, fmt.Errorf("begin planning read: %w", err)
	}
	defer tx.Rollback()
	result := PlanningSnapshot{}
	var updatedNS int64
	if err := tx.QueryRowContext(ctx, `SELECT format_key, list_id, target_id, renderer_id, catalog_revision, config_json, updated_at_ns
FROM effective_formats WHERE format_key=? AND list_id=?`, formatKey, listID).Scan(&result.Format.FormatKey, &result.Format.ListID, &result.Format.TargetID, &result.Format.RendererID, &result.Format.CatalogRevision, &result.Format.ConfigJSON, &updatedNS); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PlanningSnapshot{}, ErrFormatNotFound
		}
		return PlanningSnapshot{}, fmt.Errorf("read profile: %w", err)
	}
	result.Format.UpdatedAt = unixNanos(updatedNS)
	rows, err := tx.QueryContext(ctx, `SELECT s.id, s.component_id, r.kind, r.normalized_value, s.source_id, s.source_class, s.source_revision,
s.first_seen_ns, s.last_seen_ns, s.valid_until_ns, s.ttl_seconds, s.observation_count, s.metadata_json, s.invalid
FROM sightings s JOIN resources r ON r.id=s.resource_id WHERE s.list_id=?`, listID)
	if err != nil {
		return PlanningSnapshot{}, fmt.Errorf("read sightings: %w", err)
	}
	for rows.Next() {
		var id int64
		var component, kind, value, sourceID, sourceClass, revision, metadata string
		var firstNS, lastNS, validNS int64
		var ttl sql.NullInt64
		var count, invalid int
		if err := rows.Scan(&id, &component, &kind, &value, &sourceID, &sourceClass, &revision, &firstNS, &lastNS, &validNS, &ttl, &count, &metadata, &invalid); err != nil {
			return PlanningSnapshot{}, closeRowsWith(rows, fmt.Errorf("scan sighting: %w", err))
		}
		if activeRevisions[sourceID] != revision {
			continue
		}
		resource, err := parseResource(domain.ResourceKind(kind), value)
		if err != nil {
			return PlanningSnapshot{}, closeRowsWith(rows, fmt.Errorf("stored sighting resource is invalid"))
		}
		validUntil := unixNanos(validNS)
		result.Sightings = append(result.Sightings, domain.Sighting{ID: strconv.FormatInt(id, 10), ListID: listID, ComponentID: component, Resource: resource, SourceID: sourceID, SourceClass: domain.SourceClass(sourceClass), SourceRevision: revision, FirstSeen: unixNanos(firstNS), LastSeen: unixNanos(lastNS), ValidUntil: validUntil, TTLSeconds: ttl.Int64, TTLKnown: ttl.Valid, ObservationCount: count, Metadata: metadata, Validity: domain.LifecycleAt(validUntil, cutoff, invalid != 0)})
	}
	// An iteration cut short by a cancelled context or an I/O fault reports
	// through Err, not Close. Without this the plan would be built from a
	// silently truncated set of observations.
	if err := rows.Err(); err != nil {
		return PlanningSnapshot{}, closeRowsWith(rows, fmt.Errorf("read sightings: %w", err))
	}
	if err := rows.Close(); err != nil {
		return PlanningSnapshot{}, fmt.Errorf("close sightings: %w", err)
	}
	rows, err = tx.QueryContext(ctx, `SELECT rs.kind, rs.normalized_value, r.relation_type, rt.kind, rt.normalized_value,
r.component_id, r.first_seen_ns, r.last_seen_ns, r.valid_until_ns, r.source_id, r.source_revision, r.invalid
FROM relations r JOIN resources rs ON rs.id=r.source_resource_id JOIN resources rt ON rt.id=r.target_resource_id
WHERE r.list_id=?`, listID)
	if err != nil {
		return PlanningSnapshot{}, fmt.Errorf("read relations: %w", err)
	}
	for rows.Next() {
		var sourceKind, sourceValue, relationType, targetKind, targetValue, component, sourceID, revision string
		var firstNS, lastNS, validNS int64
		var invalid int
		if err := rows.Scan(&sourceKind, &sourceValue, &relationType, &targetKind, &targetValue, &component, &firstNS, &lastNS, &validNS, &sourceID, &revision, &invalid); err != nil {
			return PlanningSnapshot{}, closeRowsWith(rows, fmt.Errorf("scan relation: %w", err))
		}
		if activeRevisions[sourceID] != revision {
			continue
		}
		source, sourceErr := parseResource(domain.ResourceKind(sourceKind), sourceValue)
		target, targetErr := parseResource(domain.ResourceKind(targetKind), targetValue)
		if sourceErr != nil || targetErr != nil {
			return PlanningSnapshot{}, closeRowsWith(rows, fmt.Errorf("stored relation resource is invalid"))
		}
		validUntil := unixNanos(validNS)
		result.Relations = append(result.Relations, domain.Relation{SourceResource: source, RelationType: domain.RelationType(relationType), TargetResource: target, ListID: listID, ComponentID: component, FirstSeen: unixNanos(firstNS), LastSeen: unixNanos(lastNS), ValidUntil: validUntil, SourceID: sourceID, SourceRevision: revision, Validity: domain.LifecycleAt(validUntil, cutoff, invalid != 0)})
	}
	if err := rows.Err(); err != nil {
		return PlanningSnapshot{}, closeRowsWith(rows, fmt.Errorf("read relations: %w", err))
	}
	if err := rows.Close(); err != nil {
		return PlanningSnapshot{}, fmt.Errorf("close relations: %w", err)
	}
	// Source health is read in the same transaction as the observations it
	// explains, so a grace decision can never be based on a different snapshot
	// of the database than the routes it keeps alive.
	rows, err = tx.QueryContext(ctx, `SELECT source_id, source_revision,
COALESCE(MAX(CASE WHEN status='success' THEN completed_at_ns END), 0) AS last_success_ns,
COALESCE(MAX(CASE WHEN status='failed' THEN completed_at_ns END), 0) AS last_failure_ns
FROM source_runs WHERE list_id=? GROUP BY source_id, source_revision
ORDER BY source_id, source_revision`, listID)
	if err != nil {
		return PlanningSnapshot{}, fmt.Errorf("read source health: %w", err)
	}
	for rows.Next() {
		var sourceID, revision string
		var successNS, failureNS int64
		if err := rows.Scan(&sourceID, &revision, &successNS, &failureNS); err != nil {
			return PlanningSnapshot{}, closeRowsWith(rows, fmt.Errorf("scan source health: %w", err))
		}
		state := SourceRunState{SourceID: sourceID, SourceRevision: revision}
		if successNS > 0 {
			state.LastSuccessAt = unixNanos(successNS)
		}
		if failureNS > 0 {
			state.LastFailureAt = unixNanos(failureNS)
		}
		result.SourceHealth = append(result.SourceHealth, state)
	}
	if err := rows.Err(); err != nil {
		return PlanningSnapshot{}, closeRowsWith(rows, fmt.Errorf("read source health: %w", err))
	}
	if err := rows.Close(); err != nil {
		return PlanningSnapshot{}, fmt.Errorf("close source health: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return PlanningSnapshot{}, fmt.Errorf("complete planning read: %w", err)
	}
	slices.SortFunc(result.Sightings, func(a, b domain.Sighting) int { return cmp.Compare(a.Fingerprint(), b.Fingerprint()) })
	slices.SortFunc(result.Relations, func(a, b domain.Relation) int { return cmp.Compare(a.Fingerprint(), b.Fingerprint()) })
	return result, nil
}

func closeRowsWith(rows *sql.Rows, cause error) error {
	if closeErr := rows.Close(); closeErr != nil {
		return errors.Join(cause, closeErr)
	}
	return cause
}

func parseResource(kind domain.ResourceKind, value string) (domain.Resource, error) {
	switch kind {
	case domain.ResourceDomain:
		return domain.NewDomainResource(value)
	case domain.ResourceIP:
		return domain.NewAddrResourceFromString(value)
	case domain.ResourcePrefix:
		return domain.NewPrefixResourceFromString(value)
	default:
		return domain.Resource{}, fmt.Errorf("unknown resource kind")
	}
}
