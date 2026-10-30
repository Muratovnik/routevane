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

	"github.com/Muratovnik/routevane/internal/application"
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
	health, err := readSourceHealth(ctx, tx, listSourceHealthQuery, listID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	result.SourceHealth = health[listID]
	if err := tx.Commit(); err != nil {
		return PlanningSnapshot{}, fmt.Errorf("complete planning read: %w", err)
	}
	slices.SortFunc(result.Sightings, func(a, b domain.Sighting) int { return cmp.Compare(a.Fingerprint(), b.Fingerprint()) })
	slices.SortFunc(result.Relations, func(a, b domain.Relation) int { return cmp.Compare(a.Fingerprint(), b.Fingerprint()) })
	return result, nil
}

// Source health per (list, source, revision): the latest success and failure,
// the success before the latest one, and the code of the latest failure. The
// correlated reads walk source_runs_list_source_idx within one identity.
const sourceHealthColumns = `SELECT g.list_id, g.source_id, g.source_revision, g.last_success_ns, g.last_failure_ns,
COALESCE((SELECT MAX(p.completed_at_ns) FROM source_runs p
 WHERE p.list_id=g.list_id AND p.source_id=g.source_id AND p.source_revision=g.source_revision
 AND p.status='success' AND p.completed_at_ns < g.last_success_ns), 0) AS previous_success_ns,
COALESCE((SELECT f.error_code FROM source_runs f
 WHERE f.list_id=g.list_id AND f.source_id=g.source_id AND f.source_revision=g.source_revision AND f.status='failed'
 ORDER BY f.completed_at_ns DESC, f.id DESC LIMIT 1), '') AS last_error_code
FROM (SELECT list_id, source_id, source_revision,
 COALESCE(MAX(CASE WHEN status='success' THEN completed_at_ns END), 0) AS last_success_ns,
 COALESCE(MAX(CASE WHEN status='failed' THEN completed_at_ns END), 0) AS last_failure_ns
 FROM source_runs`

const listSourceHealthQuery = sourceHealthColumns + ` WHERE list_id=? GROUP BY list_id, source_id, source_revision) g
ORDER BY g.source_id, g.source_revision`

const allSourceHealthQuery = sourceHealthColumns + ` GROUP BY list_id, source_id, source_revision) g
ORDER BY g.list_id, g.source_id, g.source_revision`

func readSourceHealth(ctx context.Context, tx *sql.Tx, query string, args ...any) (map[string][]SourceRunState, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read source health: %w", err)
	}
	health := make(map[string][]SourceRunState)
	for rows.Next() {
		var listID, errorCode string
		var successNS, failureNS, previousNS int64
		state := SourceRunState{}
		if err := rows.Scan(&listID, &state.SourceID, &state.SourceRevision, &successNS, &failureNS, &previousNS, &errorCode); err != nil {
			return nil, closeRowsWith(rows, fmt.Errorf("scan source health: %w", err))
		}
		if successNS > 0 {
			state.LastSuccessAt = unixNanos(successNS)
		}
		if failureNS > 0 {
			state.LastFailureAt = unixNanos(failureNS)
			state.LastErrorCode = errorCode
		}
		if previousNS > 0 {
			state.PreviousSuccessAt = unixNanos(previousNS)
		}
		health[listID] = append(health[listID], state)
	}
	if err := rows.Err(); err != nil {
		return nil, closeRowsWith(rows, fmt.Errorf("read source health: %w", err))
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close source health: %w", err)
	}
	return health, nil
}

// ReadListObservations reads every list's source history in one read-only
// transaction: three queries whatever the number of lists. Stamps carry only
// what freshness compares — identities and times — not metadata or relations,
// and no resource is re-parsed: the stored value is already canonical.
func (s *Store) ReadListObservations(ctx context.Context, formatKey string) (map[string]application.ListObservations, error) {
	if formatKey == "" {
		return nil, fmt.Errorf("invalid list observations request")
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin list observations read: %w", err)
	}
	defer tx.Rollback()
	result := make(map[string]application.ListObservations)
	formats, err := tx.QueryContext(ctx, `SELECT list_id, catalog_revision FROM effective_formats WHERE format_key=?`, formatKey)
	if err != nil {
		return nil, fmt.Errorf("read list formats: %w", err)
	}
	for formats.Next() {
		var listID, revision string
		if err := formats.Scan(&listID, &revision); err != nil {
			return nil, closeRowsWith(formats, fmt.Errorf("scan list format: %w", err))
		}
		observations := result[listID]
		observations.CatalogRevision = revision
		result[listID] = observations
	}
	if err := formats.Err(); err != nil {
		return nil, closeRowsWith(formats, fmt.Errorf("read list formats: %w", err))
	}
	if err := formats.Close(); err != nil {
		return nil, fmt.Errorf("close list formats: %w", err)
	}
	stamps, err := tx.QueryContext(ctx, `SELECT s.list_id, s.source_id, s.source_revision, r.kind, r.normalized_value,
s.first_seen_ns, s.last_seen_ns, s.valid_until_ns, s.invalid
FROM sightings s JOIN resources r ON r.id=s.resource_id`)
	if err != nil {
		return nil, fmt.Errorf("read observation stamps: %w", err)
	}
	for stamps.Next() {
		var listID, kind, value string
		var firstNS, lastNS, validNS int64
		var invalid int
		stamp := application.ObservationStamp{}
		if err := stamps.Scan(&listID, &stamp.SourceID, &stamp.SourceRevision, &kind, &value, &firstNS, &lastNS, &validNS, &invalid); err != nil {
			return nil, closeRowsWith(stamps, fmt.Errorf("scan observation stamp: %w", err))
		}
		stamp.Value = application.ObservationValue(domain.ResourceKind(kind), value)
		stamp.FirstSeen, stamp.LastSeen, stamp.ValidUntil = unixNanos(firstNS), unixNanos(lastNS), unixNanos(validNS)
		stamp.Invalid = invalid != 0
		observations := result[listID]
		observations.Stamps = append(observations.Stamps, stamp)
		result[listID] = observations
	}
	if err := stamps.Err(); err != nil {
		return nil, closeRowsWith(stamps, fmt.Errorf("read observation stamps: %w", err))
	}
	if err := stamps.Close(); err != nil {
		return nil, fmt.Errorf("close observation stamps: %w", err)
	}
	health, err := readSourceHealth(ctx, tx, allSourceHealthQuery)
	if err != nil {
		return nil, err
	}
	for listID, states := range health {
		observations := result[listID]
		observations.SourceHealth = states
		result[listID] = observations
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("complete list observations read: %w", err)
	}
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
