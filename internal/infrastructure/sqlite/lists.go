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

// listLimit bounds the library read. The transport rejects query parameters,
// so the bound lives here rather than in the request.
const listLimit = 200

// listColumns is written once so the row scanner and every read stay in step.
const listColumns = `SELECT id,name,refresh_interval,last_refreshed_at_ns,last_refresh_failed,archived_at_ns,created_at_ns,updated_at_ns`

// validSlugSet checks one stored part of a composition. A part may be empty --
// a list built only from categories names no service of its own -- but the
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
func normalizedComposition(list application.List) (services, categories, exclusions, priority []string, ok bool) {
	services = domain.StableStrings(list.Services)
	categories = domain.StableStrings(list.Categories)
	exclusions = domain.StableStrings(list.Exclusions)
	if len(services) != len(list.Services) || len(categories) != len(list.Categories) || len(exclusions) != len(list.Exclusions) {
		return nil, nil, nil, nil, false
	}
	if !validSlugSet(services) || !validSlugSet(categories) || !validSlugSet(exclusions) {
		return nil, nil, nil, nil, false
	}
	if len(services)+len(categories) == 0 {
		return nil, nil, nil, nil, false
	}
	named := make(map[string]struct{}, len(services))
	for _, id := range services {
		named[id] = struct{}{}
	}
	for _, id := range exclusions {
		if _, both := named[id]; both {
			return nil, nil, nil, nil, false
		}
	}
	seenPriority := make(map[string]struct{}, len(list.Priority))
	for _, id := range list.Priority {
		if domain.ValidateSlug(id) != nil {
			return nil, nil, nil, nil, false
		}
		if _, duplicate := seenPriority[id]; duplicate {
			return nil, nil, nil, nil, false
		}
		seenPriority[id] = struct{}{}
		priority = append(priority, id)
	}
	return services, categories, exclusions, priority, true
}

// writeComposition replaces every membership table of one list inside the
// caller's transaction. Exclusions go in last because the trigger that refuses
// a service which is both named and excluded fires on either insert, and the
// named set is the one the caller asked for.
func writeComposition(ctx context.Context, tx *sql.Tx, listID string, services, categories, exclusions, priority []string) error {
	writes := []struct {
		statement string
		values    []string
	}{
		{`INSERT INTO profile_lists(profile_id,list_id) VALUES(?,?)`, services},
		{`INSERT INTO profile_categories(profile_id,category_id) VALUES(?,?)`, categories},
		{`INSERT INTO profile_exclusions(profile_id,list_id) VALUES(?,?)`, exclusions},
	}
	for _, write := range writes {
		for _, value := range write.values {
			if _, err := tx.ExecContext(ctx, write.statement, listID, value); err != nil {
				return fmt.Errorf("write list composition: %w", err)
			}
		}
	}
	for position, serviceID := range priority {
		if _, err := tx.ExecContext(ctx, `INSERT INTO profile_list_priorities(profile_id,list_id,position) VALUES(?,?,?)`, listID, serviceID, position); err != nil {
			return fmt.Errorf("write list priority: %w", err)
		}
	}
	return nil
}

func validServiceDomains(overrides map[string][]string) bool {
	if len(overrides) > 128 {
		return false
	}
	total := 0
	for serviceID, values := range overrides {
		if domain.ValidateSlug(serviceID) != nil || len(values) > 64 {
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

func writeServiceDomains(ctx context.Context, tx *sql.Tx, listID string, overrides map[string][]string) error {
	if !validServiceDomains(overrides) {
		return fmt.Errorf("invalid list service domains")
	}
	ids := slices.Sorted(maps.Keys(overrides))
	for _, serviceID := range ids {
		payload, err := json.Marshal(overrides[serviceID])
		if err != nil {
			return fmt.Errorf("encode list service domains: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO profile_list_domains(profile_id,list_id,domains_json) VALUES(?,?,?)`, listID, serviceID, string(payload)); err != nil {
			return fmt.Errorf("write list service domains: %w", err)
		}
	}
	return nil
}

func clearComposition(ctx context.Context, tx *sql.Tx, listID string) error {
	for _, statement := range []string{
		`DELETE FROM profile_lists WHERE profile_id=?`,
		`DELETE FROM profile_categories WHERE profile_id=?`,
		`DELETE FROM profile_exclusions WHERE profile_id=?`,
		`DELETE FROM profile_list_domains WHERE profile_id=?`,
		`DELETE FROM profile_list_priorities WHERE profile_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, listID); err != nil {
			return fmt.Errorf("replace list composition: %w", err)
		}
	}
	return nil
}

func (s *Store) CreateList(ctx context.Context, list application.List) error {
	services, categories, exclusions, priority, ok := normalizedComposition(list)
	if !validID(list.ID) || list.Name == "" || len([]rune(list.Name)) > 120 || list.CreatedAt.IsZero() || list.UpdatedAt.IsZero() || !ok {
		return fmt.Errorf("invalid list")
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
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM profiles WHERE id=?`, list.ID).Scan(&taken); err != nil {
		return fail(fmt.Errorf("check list identity: %w", err))
	}
	if taken != 0 {
		return fail(application.ErrIdentityCollision)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO profiles(id,name,created_at_ns,updated_at_ns) VALUES(?,?,?,?)`, list.ID, list.Name, list.CreatedAt.UTC().UnixNano(), list.UpdatedAt.UTC().UnixNano()); err != nil {
		return fail(fmt.Errorf("insert list: %w", err))
	}
	if err := writeComposition(ctx, tx, list.ID, services, categories, exclusions, priority); err != nil {
		return fail(err)
	}
	if err := writeServiceDomains(ctx, tx, list.ID, list.ServiceDomains); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit list creation: %w", err)
	}
	return nil
}

func (s *Store) List(ctx context.Context, id string) (application.List, error) {
	if !validID(id) {
		return application.List{}, application.ErrNotFound
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	list, err := scanList(s.db.QueryRowContext(ctx, listColumns+` FROM profiles WHERE id=?`, id))
	if err != nil {
		return application.List{}, err
	}
	if err := s.readComposition(ctx, &list); err != nil {
		return application.List{}, err
	}
	if err := s.readServiceDomains(ctx, &list); err != nil {
		return application.List{}, err
	}
	return list, nil
}

func (s *Store) readServiceDomains(ctx context.Context, list *application.List) error {
	rows, err := s.db.QueryContext(ctx, `SELECT list_id,domains_json FROM profile_list_domains WHERE profile_id=? ORDER BY list_id ASC LIMIT 128`, list.ID)
	if err != nil {
		return fmt.Errorf("read list service domains: %w", err)
	}
	defer func() { _ = rows.Close() }()
	overrides := make(map[string][]string)
	for rows.Next() {
		var serviceID, payload string
		if err := rows.Scan(&serviceID, &payload); err != nil {
			return fmt.Errorf("read list service domains: %w", err)
		}
		var domains []string
		if err := json.Unmarshal([]byte(payload), &domains); err != nil {
			return fmt.Errorf("decode list service domains: %w", err)
		}
		if domains == nil {
			domains = []string{}
		}
		overrides[serviceID] = domains
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read list service domains: %w", err)
	}
	if !validServiceDomains(overrides) {
		return fmt.Errorf("invalid stored list service domains")
	}
	if len(overrides) > 0 {
		list.ServiceDomains = overrides
	}
	return nil
}

// readComposition fills the three stored parts of one list. Each is a separate
// query rather than a join because a list may legitimately have none of one
// kind, and a join would have to distinguish that from a missing list.
func (s *Store) readComposition(ctx context.Context, list *application.List) error {
	reads := []struct {
		query string
		into  *[]string
	}{
		{`SELECT list_id FROM profile_lists WHERE profile_id=? ORDER BY list_id ASC LIMIT 128`, &list.Services},
		{`SELECT category_id FROM profile_categories WHERE profile_id=? ORDER BY category_id ASC LIMIT 128`, &list.Categories},
		{`SELECT list_id FROM profile_exclusions WHERE profile_id=? ORDER BY list_id ASC LIMIT 128`, &list.Exclusions},
	}
	for _, read := range reads {
		values, err := s.compositionPart(ctx, read.query, list.ID)
		if err != nil {
			return err
		}
		*read.into = values
	}
	priority, err := s.compositionPart(ctx, `SELECT list_id FROM profile_list_priorities WHERE profile_id=? ORDER BY position ASC LIMIT 128`, list.ID)
	if err != nil {
		return err
	}
	list.Priority = priority
	return nil
}

func (s *Store) compositionPart(ctx context.Context, query, listID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, listID)
	if err != nil {
		return nil, fmt.Errorf("read list composition: %w", err)
	}
	defer func() { _ = rows.Close() }()
	values := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read list composition: %w", err)
		}
		values = append(values, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read list composition: %w", err)
	}
	return values, nil
}

func (s *Store) Lists(ctx context.Context) ([]application.List, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, listColumns+` FROM profiles ORDER BY created_at_ns DESC, id ASC LIMIT ?`, listLimit)
	if err != nil {
		return nil, fmt.Errorf("list lists: %w", err)
	}
	lists := make([]application.List, 0)
	for rows.Next() {
		list, err := scanList(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		lists = append(lists, list)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("list lists: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("list lists: %w", err)
	}
	// The composition read runs after the cursor is closed: this store holds a
	// single connection, so a nested query would deadlock against it.
	for i := range lists {
		if err := s.readComposition(ctx, &lists[i]); err != nil {
			return nil, err
		}
		if err := s.readServiceDomains(ctx, &lists[i]); err != nil {
			return nil, err
		}
	}
	return lists, nil
}

func (s *Store) UpdateList(ctx context.Context, list application.List) error {
	services, categories, exclusions, priority, ok := normalizedComposition(list)
	if !validID(list.ID) || list.Name == "" || len([]rune(list.Name)) > 120 || list.UpdatedAt.IsZero() || !ok {
		return fmt.Errorf("invalid list")
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
	result, err := tx.ExecContext(ctx, `UPDATE profiles SET name=?,updated_at_ns=? WHERE id=?`, list.Name, list.UpdatedAt.UTC().UnixNano(), list.ID)
	if err != nil {
		return fail(fmt.Errorf("update list: %w", err))
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return fail(application.ErrNotFound)
	}
	if err := clearComposition(ctx, tx, list.ID); err != nil {
		return fail(err)
	}
	if err := writeComposition(ctx, tx, list.ID, services, categories, exclusions, priority); err != nil {
		return fail(err)
	}
	if err := writeServiceDomains(ctx, tx, list.ID, list.ServiceDomains); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit list update: %w", err)
	}
	return nil
}

// SetListArchived writes only the archival column and the moment of the edit.
// A zero moment is the restored state: the column carries the fact and its date
// together, so nothing can report an archived list with no date or a date with
// no archival.
func (s *Store) SetListArchived(ctx context.Context, listID string, archivedAt time.Time, updatedAt time.Time) error {
	if !validID(listID) || updatedAt.IsZero() {
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
		archived, updatedAt.UTC().UnixNano(), listID)
	if err != nil {
		return fmt.Errorf("update list archival: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return application.ErrNotFound
	}
	return nil
}
