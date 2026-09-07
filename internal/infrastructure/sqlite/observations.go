package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/domain"
)

func (s *Store) ApplySuccess(ctx context.Context, cycle SuccessCycle) error {
	if err := validateSuccess(cycle); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin source cycle: %w", err)
	}
	fail := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	sightings, err := canonicalCycleSightings(cycle)
	if err != nil {
		return fail(err)
	}
	relations, err := canonicalCycleRelations(cycle)
	if err != nil {
		return fail(err)
	}
	for _, sighting := range sightings {
		resourceID, err := upsertResource(ctx, tx, sighting.Resource, cycle.CompletedAt)
		if err != nil {
			return fail(err)
		}
		var ttl any
		if sighting.TTLKnown {
			ttl = sighting.TTLSeconds
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO sightings(list_id, component_id, resource_id, source_id, source_class, source_revision, first_seen_ns, last_seen_ns, valid_until_ns, ttl_seconds, observation_count, metadata_json, invalid)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,0)
ON CONFLICT(list_id, component_id, resource_id, source_id, source_revision) DO UPDATE SET
 first_seen_ns=min(sightings.first_seen_ns, excluded.first_seen_ns),
 last_seen_ns=max(sightings.last_seen_ns, excluded.last_seen_ns),
 valid_until_ns=max(sightings.valid_until_ns, excluded.valid_until_ns),
 ttl_seconds=CASE WHEN excluded.ttl_seconds IS NULL THEN sightings.ttl_seconds ELSE excluded.ttl_seconds END,
 observation_count=sightings.observation_count+1,
 metadata_json=CASE WHEN excluded.metadata_json='' THEN sightings.metadata_json ELSE excluded.metadata_json END,
 invalid=0`, cycle.ServiceID, sighting.ComponentID, resourceID, cycle.SourceID, sighting.SourceClass, cycle.SourceRevision, sighting.FirstSeen.UnixNano(), sighting.LastSeen.UnixNano(), sighting.ValidUntil.UnixNano(), ttl, 1, sighting.Metadata)
		if err != nil {
			return fail(fmt.Errorf("upsert sighting: %w", err))
		}
	}
	for _, relation := range relations {
		sourceID, err := upsertResource(ctx, tx, relation.SourceResource, cycle.CompletedAt)
		if err != nil {
			return fail(err)
		}
		targetID, err := upsertResource(ctx, tx, relation.TargetResource, cycle.CompletedAt)
		if err != nil {
			return fail(err)
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO relations(source_resource_id, relation_type, target_resource_id, list_id, component_id, first_seen_ns, last_seen_ns, valid_until_ns, source_id, source_revision, invalid)
VALUES(?,?,?,?,?,?,?,?,?,?,0)
ON CONFLICT(source_resource_id, relation_type, target_resource_id, list_id, component_id, source_id, source_revision) DO UPDATE SET
 first_seen_ns=min(relations.first_seen_ns, excluded.first_seen_ns),
 last_seen_ns=max(relations.last_seen_ns, excluded.last_seen_ns),
 valid_until_ns=max(relations.valid_until_ns, excluded.valid_until_ns),
 invalid=0`, sourceID, relation.RelationType, targetID, cycle.ServiceID, relation.ComponentID, relation.FirstSeen.UnixNano(), relation.LastSeen.UnixNano(), relation.ValidUntil.UnixNano(), cycle.SourceID, cycle.SourceRevision)
		if err != nil {
			return fail(fmt.Errorf("upsert relation: %w", err))
		}
	}
	if cycle.Profile != nil {
		if err := upsertProfile(ctx, tx, *cycle.Profile); err != nil {
			return fail(err)
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO source_runs(list_id, source_id, source_revision, started_at_ns, completed_at_ns, status, sighting_count, relation_count, error_code)
VALUES(?,?,?,?,?,'success',?,?,NULL)`, cycle.ServiceID, cycle.SourceID, cycle.SourceRevision, cycle.StartedAt.UnixNano(), cycle.CompletedAt.UnixNano(), len(sightings), len(relations))
	if err != nil {
		return fail(fmt.Errorf("record successful source run: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit source cycle: %w", err)
	}
	return s.protectFiles()
}

func (s *Store) RecordFailure(ctx context.Context, cycle FailureCycle) error {
	if !validCycleIdentity(cycle.ServiceID, cycle.SourceID, cycle.SourceRevision, cycle.StartedAt, cycle.CompletedAt) || !validErrorCode(cycle.ErrorCode) {
		return ErrInvalidCycle
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO source_runs(list_id, source_id, source_revision, started_at_ns, completed_at_ns, status, sighting_count, relation_count, error_code)
VALUES(?,?,?,?,?,'failed',0,0,?)`, cycle.ServiceID, cycle.SourceID, cycle.SourceRevision, cycle.StartedAt.UTC().UnixNano(), cycle.CompletedAt.UTC().UnixNano(), cycle.ErrorCode)
	if err != nil {
		return fmt.Errorf("record failed source run: %w", err)
	}
	return s.protectFiles()
}

func upsertResource(ctx context.Context, tx *sql.Tx, resource domain.Resource, createdAt time.Time) (int64, error) {
	if !resource.IsValid() {
		return 0, fmt.Errorf("invalid resource")
	}
	var ipVersion any
	switch resource.Kind {
	case domain.ResourceIP:
		if resource.Addr.Is4() {
			ipVersion = 4
		} else {
			ipVersion = 6
		}
	case domain.ResourcePrefix:
		if resource.Prefix.Addr().Is4() {
			ipVersion = 4
		} else {
			ipVersion = 6
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO resources(kind, normalized_value, ip_version, created_at_ns) VALUES(?,?,?,?)
ON CONFLICT(kind, normalized_value) DO NOTHING`, resource.Kind, resource.CanonicalValue(), ipVersion, createdAt.UTC().UnixNano())
	if err != nil {
		return 0, fmt.Errorf("upsert resource: %w", err)
	}
	var id int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM resources WHERE kind=? AND normalized_value=?", resource.Kind, resource.CanonicalValue()).Scan(&id); err != nil {
		return 0, fmt.Errorf("read resource id: %w", err)
	}
	return id, nil
}

func upsertProfile(ctx context.Context, tx *sql.Tx, profile ProfileRecord) error {
	if profile.ProfileKey == "" || domain.ValidateSlug(profile.ServiceID) != nil || profile.TargetID == "" || profile.RendererID == "" || len(profile.CatalogRevision) != 64 || profile.UpdatedAt.IsZero() || len(profile.ConfigJSON) == 0 || !json.Valid(profile.ConfigJSON) {
		return fmt.Errorf("invalid effective profile")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO effective_formats(format_key, list_id, target_id, renderer_id, catalog_revision, config_json, updated_at_ns)
VALUES(?,?,?,?,?,?,?) ON CONFLICT(format_key, list_id) DO UPDATE SET target_id=excluded.target_id, renderer_id=excluded.renderer_id,
catalog_revision=excluded.catalog_revision, config_json=excluded.config_json, updated_at_ns=max(effective_formats.updated_at_ns, excluded.updated_at_ns)`, profile.ProfileKey, profile.ServiceID, profile.TargetID, profile.RendererID, profile.CatalogRevision, string(profile.ConfigJSON), profile.UpdatedAt.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("upsert profile: %w", err)
	}
	return nil
}

func validateSuccess(cycle SuccessCycle) error {
	if !validCycleIdentity(cycle.ServiceID, cycle.SourceID, cycle.SourceRevision, cycle.StartedAt, cycle.CompletedAt) {
		return ErrInvalidCycle
	}
	if cycle.Profile != nil && cycle.Profile.ServiceID != cycle.ServiceID {
		return ErrInvalidCycle
	}
	return nil
}

func validCycleIdentity(serviceID, sourceID, revision string, startedAt, completedAt time.Time) bool {
	return domain.ValidateSlug(serviceID) == nil && domain.ValidateSlug(sourceID) == nil && validRevision(revision) && !startedAt.IsZero() && !completedAt.IsZero() && !completedAt.Before(startedAt)
}

func canonicalCycleSightings(cycle SuccessCycle) ([]domain.Sighting, error) {
	byKey := make(map[string]domain.Sighting, len(cycle.Sightings))
	for _, value := range cycle.Sightings {
		if value.ServiceID != cycle.ServiceID || value.SourceID != cycle.SourceID || value.SourceRevision != cycle.SourceRevision || domain.ValidateSlug(value.ComponentID) != nil || !value.Resource.IsValid() || value.FirstSeen.IsZero() || value.LastSeen.IsZero() || value.ValidUntil.IsZero() || value.LastSeen.Before(value.FirstSeen) || value.ValidUntil.Before(value.LastSeen) || len(value.Metadata) > maxMetadataBytes || value.TTLSeconds < 0 {
			return nil, ErrInvalidCycle
		}
		key := strings.Join([]string{value.ComponentID, value.Resource.Kind.String(), value.Resource.CanonicalValue()}, "\x00")
		if prior, ok := byKey[key]; ok {
			if value.FirstSeen.Before(prior.FirstSeen) {
				prior.FirstSeen = value.FirstSeen
			}
			if value.LastSeen.After(prior.LastSeen) {
				prior.LastSeen = value.LastSeen
			}
			if value.ValidUntil.After(prior.ValidUntil) {
				prior.ValidUntil = value.ValidUntil
			}
			byKey[key] = prior
			continue
		}
		value.FirstSeen = value.FirstSeen.UTC()
		value.LastSeen = value.LastSeen.UTC()
		value.ValidUntil = value.ValidUntil.UTC()
		byKey[key] = value
	}
	out := make([]domain.Sighting, 0, len(byKey))
	for _, value := range byKey {
		out = append(out, value)
	}
	slices.SortFunc(out, func(a, b domain.Sighting) int { return cmp.Compare(a.Fingerprint(), b.Fingerprint()) })
	return out, nil
}

func canonicalCycleRelations(cycle SuccessCycle) ([]domain.Relation, error) {
	byKey := make(map[string]domain.Relation, len(cycle.Relations))
	for _, value := range cycle.Relations {
		if value.ServiceID != cycle.ServiceID || value.SourceID != cycle.SourceID || value.SourceRevision != cycle.SourceRevision || domain.ValidateSlug(value.ComponentID) != nil || !domain.KnownRelationType(value.RelationType) || !value.SourceResource.IsValid() || !value.TargetResource.IsValid() || value.FirstSeen.IsZero() || value.LastSeen.IsZero() || value.ValidUntil.IsZero() || value.LastSeen.Before(value.FirstSeen) || value.ValidUntil.Before(value.LastSeen) {
			return nil, ErrInvalidCycle
		}
		key := value.Fingerprint()
		if prior, ok := byKey[key]; ok {
			if value.FirstSeen.Before(prior.FirstSeen) {
				prior.FirstSeen = value.FirstSeen
			}
			if value.LastSeen.After(prior.LastSeen) {
				prior.LastSeen = value.LastSeen
			}
			if value.ValidUntil.After(prior.ValidUntil) {
				prior.ValidUntil = value.ValidUntil
			}
			byKey[key] = prior
			continue
		}
		byKey[key] = value
	}
	out := make([]domain.Relation, 0, len(byKey))
	for _, value := range byKey {
		out = append(out, value)
	}
	slices.SortFunc(out, func(a, b domain.Relation) int { return cmp.Compare(a.Fingerprint(), b.Fingerprint()) })
	return out, nil
}

func validErrorCode(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, c := range value {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

func validRevision(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
