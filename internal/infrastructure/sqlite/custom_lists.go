package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

// customListLimit bounds the registry read the same way the library read is
// bounded: the transport carries no pagination, so the bound lives here.
const customListLimit = 200

// validCustomList checks one row against the grammar the schema promises:
// the reserved prefix, a slug identity, a bounded title, and a bounded set of
// normalized domains.
func validCustomList(list application.CustomList) bool {
	if !strings.HasPrefix(list.ID, "custom-") || domain.ValidateSlug(list.ID) != nil {
		return false
	}
	if list.Title == "" || len([]rune(list.Title)) > 120 {
		return false
	}
	if len(list.Domains) == 0 || len(list.Domains) > 64 || len(domain.StableStrings(list.Domains)) != len(list.Domains) {
		return false
	}
	for _, value := range list.Domains {
		normalized, err := domain.NormalizeDomain(value)
		if err != nil || normalized != value {
			return false
		}
	}
	return true
}

func (s *Store) CreateCustomList(ctx context.Context, list application.CustomList) error {
	if !validCustomList(list) || list.CreatedAt.IsZero() || list.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid custom list")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	payload, err := json.Marshal(list.Domains)
	if err != nil {
		return fmt.Errorf("encode custom list domains: %w", err)
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	// A taken identifier is the one failure the caller retries; everything
	// else is a real error and must not be spent on eight more attempts.
	var taken int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM custom_lists WHERE id=?`, list.ID).Scan(&taken); err != nil {
		return fmt.Errorf("check custom list identity: %w", err)
	}
	if taken != 0 {
		return application.ErrIdentityCollision
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO custom_lists(id,title,domains_json,created_at_ns,updated_at_ns) VALUES(?,?,?,?,?)`,
		list.ID, list.Title, string(payload), list.CreatedAt.UTC().UnixNano(), list.UpdatedAt.UTC().UnixNano()); err != nil {
		return fmt.Errorf("insert custom list: %w", err)
	}
	return nil
}

func (s *Store) UpdateCustomList(ctx context.Context, list application.CustomList) error {
	if !validCustomList(list) || list.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid custom list")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	payload, err := json.Marshal(list.Domains)
	if err != nil {
		return fmt.Errorf("encode custom list domains: %w", err)
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	result, err := s.db.ExecContext(ctx,
		`UPDATE custom_lists SET title=?,domains_json=?,updated_at_ns=? WHERE id=?`,
		list.Title, string(payload), list.UpdatedAt.UTC().UnixNano(), list.ID)
	if err != nil {
		return fmt.Errorf("update custom list: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return application.ErrNotFound
	}
	return nil
}

func (s *Store) CustomLists(ctx context.Context) ([]application.CustomList, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,title,domains_json,created_at_ns,updated_at_ns FROM custom_lists ORDER BY id ASC LIMIT ?`, customListLimit)
	if err != nil {
		return nil, fmt.Errorf("list custom lists: %w", err)
	}
	defer func() { _ = rows.Close() }()
	lists := make([]application.CustomList, 0)
	for rows.Next() {
		list, err := scanCustomList(rows)
		if err != nil {
			return nil, err
		}
		lists = append(lists, list)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list custom lists: %w", err)
	}
	return lists, nil
}

func scanCustomList(rows *sql.Rows) (application.CustomList, error) {
	var list application.CustomList
	var payload string
	var created, updated int64
	if err := rows.Scan(&list.ID, &list.Title, &payload, &created, &updated); err != nil {
		return application.CustomList{}, fmt.Errorf("read custom list: %w", err)
	}
	if err := json.Unmarshal([]byte(payload), &list.Domains); err != nil {
		return application.CustomList{}, fmt.Errorf("decode custom list domains: %w", err)
	}
	list.CreatedAt = unixNanos(created)
	list.UpdatedAt = unixNanos(updated)
	if !validCustomList(list) {
		return application.CustomList{}, fmt.Errorf("invalid stored custom list")
	}
	return list, nil
}
