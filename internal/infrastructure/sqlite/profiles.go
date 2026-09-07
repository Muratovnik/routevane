package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

// profileLimit bounds the library read. The transport rejects query parameters,
// so the bound lives here rather than in the request.
const profileLimit = 200

// profileColumns is written once so the row scanner and every read stay in step.
const profileColumns = `SELECT id,name,refresh_interval,last_refreshed_at_ns,last_refresh_failed,archived_at_ns,created_at_ns,updated_at_ns`

// validSlugSet checks one stored part of a composition. A part may be empty --
// a profile built only from categories names no list of its own -- but the
// whole composition may not be, which the caller checks.
func validSlugSet(values []string) bool {
	if len(values) > 128 {
		return false
	}
	for _, id := range values {
		if domain.ValidateSlug(id) != nil {
			return false
		}
	}
	return true
}

// normalizedComposition returns the unordered references in canonical order
// and the priority in operator order. It refuses malformed, duplicate or
// contradictory values and a composition that names nothing at all.
func normalizedComposition(profile application.Profile) (lists, categories, exclusions, priority []string, ok bool) {
	lists = domain.StableStrings(profile.Lists)
	categories = domain.StableStrings(profile.Categories)
	exclusions = domain.StableStrings(profile.Exclusions)
	if len(lists) != len(profile.Lists) || len(categories) != len(profile.Categories) || len(exclusions) != len(profile.Exclusions) {
		return nil, nil, nil, nil, false
	}
	if !validSlugSet(lists) || !validSlugSet(categories) || !validSlugSet(exclusions) {
		return nil, nil, nil, nil, false
	}
	if len(lists)+len(categories) == 0 {
		return nil, nil, nil, nil, false
	}
	named := make(map[string]struct{}, len(lists))
	for _, id := range lists {
		named[id] = struct{}{}
	}
	for _, id := range exclusions {
		if _, both := named[id]; both {
			return nil, nil, nil, nil, false
		}
	}
	seenPriority := make(map[string]struct{}, len(profile.Priority))
	for _, id := range profile.Priority {
		if domain.ValidateSlug(id) != nil {
			return nil, nil, nil, nil, false
		}
		if _, duplicate := seenPriority[id]; duplicate {
			return nil, nil, nil, nil, false
		}
		seenPriority[id] = struct{}{}
		priority = append(priority, id)
	}
	return lists, categories, exclusions, priority, true
}

// writeComposition replaces every membership table of one profile inside the
// caller's transaction. Exclusions go in last because the trigger that refuses
// a list which is both named and excluded fires on either insert, and the
// named set is the one the caller asked for.
func writeComposition(ctx context.Context, tx *sql.Tx, profileID string, lists, categories, exclusions, priority []string) error {
	writes := []struct {
		statement string
		values    []string
	}{
		{`INSERT INTO profile_lists(profile_id,list_id) VALUES(?,?)`, lists},
		{`INSERT INTO profile_categories(profile_id,category_id) VALUES(?,?)`, categories},
		{`INSERT INTO profile_exclusions(profile_id,list_id) VALUES(?,?)`, exclusions},
	}
	for _, write := range writes {
		for _, value := range write.values {
			if _, err := tx.ExecContext(ctx, write.statement, profileID, value); err != nil {
				return fmt.Errorf("write profile composition: %w", err)
			}
		}
	}
	for position, listID := range priority {
		if _, err := tx.ExecContext(ctx, `INSERT INTO profile_list_priorities(profile_id,list_id,position) VALUES(?,?,?)`, profileID, listID, position); err != nil {
			return fmt.Errorf("write profile priority: %w", err)
		}
	}
	return nil
}

func validListDomains(overrides map[string][]string) bool {
	if len(overrides) > 128 {
		return false
	}
	total := 0
	for listID, values := range overrides {
		if domain.ValidateSlug(listID) != nil || len(values) > 64 {
			return false
		}
		total += len(values)
		if total > 512 || len(domain.StableStrings(values)) != len(values) {
			return false
		}
		for _, value := range values {
			normalized, err := domain.NormalizeDomain(value)
			if err != nil || normalized != value {
				return false
			}
		}
	}
	return true
}

func writeListDomains(ctx context.Context, tx *sql.Tx, profileID string, overrides map[string][]string) error {
	if !validListDomains(overrides) {
		return fmt.Errorf("invalid profile list domains")
	}
	ids := slices.Sorted(maps.Keys(overrides))
	for _, listID := range ids {
		payload, err := json.Marshal(overrides[listID])
		if err != nil {
			return fmt.Errorf("encode profile list domains: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO profile_list_domains(profile_id,list_id,domains_json) VALUES(?,?,?)`, profileID, listID, string(payload)); err != nil {
			return fmt.Errorf("write profile list domains: %w", err)
		}
	}
	return nil
}

func clearComposition(ctx context.Context, tx *sql.Tx, profileID string) error {
	for _, statement := range []string{
		`DELETE FROM profile_lists WHERE profile_id=?`,
		`DELETE FROM profile_categories WHERE profile_id=?`,
		`DELETE FROM profile_exclusions WHERE profile_id=?`,
		`DELETE FROM profile_list_domains WHERE profile_id=?`,
		`DELETE FROM profile_list_priorities WHERE profile_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, profileID); err != nil {
			return fmt.Errorf("replace profile composition: %w", err)
		}
	}
	return nil
}

func (s *Store) CreateProfile(ctx context.Context, profile application.Profile) error {
	lists, categories, exclusions, priority, ok := normalizedComposition(profile)
	if !validID(profile.ID) || profile.Name == "" || len([]rune(profile.Name)) > 120 || profile.CreatedAt.IsZero() || profile.UpdatedAt.IsZero() || !ok {
		return fmt.Errorf("invalid profile")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin list creation: %w", err)
	}
	fail := func(cause error) error { _ = tx.Rollback(); return cause }
	// A taken identifier is the one failure the caller retries. Everything else
	// is a real error and must not be spent on eight more attempts.
	var taken int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM profiles WHERE id=?`, profile.ID).Scan(&taken); err != nil {
		return fail(fmt.Errorf("check list identity: %w", err))
	}
	if taken != 0 {
		return fail(application.ErrIdentityCollision)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO profiles(id,name,created_at_ns,updated_at_ns) VALUES(?,?,?,?)`, profile.ID, profile.Name, profile.CreatedAt.UTC().UnixNano(), profile.UpdatedAt.UTC().UnixNano()); err != nil {
		return fail(fmt.Errorf("insert list: %w", err))
	}
	if err := writeComposition(ctx, tx, profile.ID, lists, categories, exclusions, priority); err != nil {
		return fail(err)
	}
	if err := writeListDomains(ctx, tx, profile.ID, profile.ListDomains); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit list creation: %w", err)
	}
	return nil
}

func (s *Store) Profile(ctx context.Context, id string) (application.Profile, error) {
	if !validID(id) {
		return application.Profile{}, application.ErrNotFound
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	profile, err := scanProfile(s.db.QueryRowContext(ctx, profileColumns+` FROM profiles WHERE id=?`, id))
	if err != nil {
		return application.Profile{}, err
	}
	if err := s.readComposition(ctx, &profile); err != nil {
		return application.Profile{}, err
	}
	if err := s.readListDomains(ctx, &profile); err != nil {
		return application.Profile{}, err
	}
	return profile, nil
}

func (s *Store) readListDomains(ctx context.Context, profile *application.Profile) error {
	rows, err := s.db.QueryContext(ctx, `SELECT list_id,domains_json FROM profile_list_domains WHERE profile_id=? ORDER BY list_id ASC LIMIT 128`, profile.ID)
	if err != nil {
		return fmt.Errorf("read profile list domains: %w", err)
	}
	defer func() { _ = rows.Close() }()
	overrides := make(map[string][]string)
	for rows.Next() {
		var listID, payload string
		if err := rows.Scan(&listID, &payload); err != nil {
			return fmt.Errorf("read profile list domains: %w", err)
		}
		var domains []string
		if err := json.Unmarshal([]byte(payload), &domains); err != nil {
			return fmt.Errorf("decode profile list domains: %w", err)
		}
		if domains == nil {
			domains = []string{}
		}
		overrides[listID] = domains
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read profile list domains: %w", err)
	}
	if !validListDomains(overrides) {
		return fmt.Errorf("invalid stored profile list domains")
	}
	if len(overrides) > 0 {
		profile.ListDomains = overrides
	}
	return nil
}

// readComposition fills the three stored parts of one profile. Each is a separate
// query rather than a join because a profile may legitimately have none of one
// kind, and a join would have to distinguish that from a missing profile.
func (s *Store) readComposition(ctx context.Context, profile *application.Profile) error {
	reads := []struct {
		query string
		into  *[]string
	}{
		{`SELECT list_id FROM profile_lists WHERE profile_id=? ORDER BY list_id ASC LIMIT 128`, &profile.Lists},
		{`SELECT category_id FROM profile_categories WHERE profile_id=? ORDER BY category_id ASC LIMIT 128`, &profile.Categories},
		{`SELECT list_id FROM profile_exclusions WHERE profile_id=? ORDER BY list_id ASC LIMIT 128`, &profile.Exclusions},
	}
	for _, read := range reads {
		values, err := s.compositionPart(ctx, read.query, profile.ID)
		if err != nil {
			return err
		}
		*read.into = values
	}
	priority, err := s.compositionPart(ctx, `SELECT list_id FROM profile_list_priorities WHERE profile_id=? ORDER BY position ASC LIMIT 128`, profile.ID)
	if err != nil {
		return err
	}
	profile.Priority = priority
	return nil
}

func (s *Store) compositionPart(ctx context.Context, query, profileID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, profileID)
	if err != nil {
		return nil, fmt.Errorf("read profile composition: %w", err)
	}
	defer func() { _ = rows.Close() }()
	values := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read profile composition: %w", err)
		}
		values = append(values, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read profile composition: %w", err)
	}
	return values, nil
}

func (s *Store) Profiles(ctx context.Context) ([]application.Profile, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, profileColumns+` FROM profiles ORDER BY created_at_ns DESC, id ASC LIMIT ?`, profileLimit)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	profiles := make([]application.Profile, 0)
	for rows.Next() {
		profile, err := scanProfile(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	// The composition read runs after the cursor is closed: this store holds a
	// single connection, so a nested query would deadlock against it.
	for i := range profiles {
		if err := s.readComposition(ctx, &profiles[i]); err != nil {
			return nil, err
		}
		if err := s.readListDomains(ctx, &profiles[i]); err != nil {
			return nil, err
		}
	}
	return profiles, nil
}

func (s *Store) UpdateProfile(ctx context.Context, profile application.Profile) error {
	lists, categories, exclusions, priority, ok := normalizedComposition(profile)
	if !validID(profile.ID) || profile.Name == "" || len([]rune(profile.Name)) > 120 || profile.UpdatedAt.IsZero() || !ok {
		return fmt.Errorf("invalid profile")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin list update: %w", err)
	}
	fail := func(cause error) error { _ = tx.Rollback(); return cause }
	result, err := tx.ExecContext(ctx, `UPDATE profiles SET name=?,updated_at_ns=? WHERE id=?`, profile.Name, profile.UpdatedAt.UTC().UnixNano(), profile.ID)
	if err != nil {
		return fail(fmt.Errorf("update list: %w", err))
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return fail(application.ErrNotFound)
	}
	if err := clearComposition(ctx, tx, profile.ID); err != nil {
		return fail(err)
	}
	if err := writeComposition(ctx, tx, profile.ID, lists, categories, exclusions, priority); err != nil {
		return fail(err)
	}
	if err := writeListDomains(ctx, tx, profile.ID, profile.ListDomains); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit list update: %w", err)
	}
	return nil
}

// SetProfileArchived writes only the archival column and the moment of the edit.
// A zero moment is the restored state: the column carries the fact and its date
// together, so nothing can report an archived profile with no date or a date with
// no archival.
func (s *Store) SetProfileArchived(ctx context.Context, profileID string, archivedAt time.Time, updatedAt time.Time) error {
	if !validID(profileID) || updatedAt.IsZero() {
		return fmt.Errorf("invalid list archival")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	archived := int64(0)
	if !archivedAt.IsZero() {
		archived = archivedAt.UTC().UnixNano()
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE profiles SET archived_at_ns=?, updated_at_ns=? WHERE id=?`,
		archived, updatedAt.UTC().UnixNano(), profileID)
	if err != nil {
		return fmt.Errorf("update list archival: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return application.ErrNotFound
	}
	return nil
}
