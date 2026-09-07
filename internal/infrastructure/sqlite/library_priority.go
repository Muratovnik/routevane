package sqlite

import (
	"context"
	"fmt"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

// DefaultPriority reads the sparse library-wide preference in stored order.
// The application layer owns catalog filtering and canonical append behavior;
// this boundary intentionally returns stale rows too so no destructive cleanup
// is coupled to a catalog change.
func (s *Store) DefaultPriority(ctx context.Context) ([]string, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT list_id FROM library_list_priorities ORDER BY position ASC`)
	if err != nil {
		return nil, fmt.Errorf("read library priority: %w", err)
	}
	defer rows.Close()
	priority := make([]string, 0)
	for rows.Next() {
		var listID string
		if err := rows.Scan(&listID); err != nil {
			return nil, fmt.Errorf("read library priority: %w", err)
		}
		priority = append(priority, listID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read library priority: %w", err)
	}
	return priority, nil
}

// SetDefaultPriority replaces the sparse preference as one transaction. The
// application has already checked that the values form a full permutation of
// current lists; the store still validates its own row grammar so direct
// callers cannot create duplicate positions or malformed ids.
func (s *Store) SetDefaultPriority(ctx context.Context, priority []string) error {
	seen := make(map[string]struct{}, len(priority))
	for _, listID := range priority {
		if domain.ValidateSlug(listID) != nil {
			return fmt.Errorf("invalid library priority")
		}
		if _, duplicate := seen[listID]; duplicate {
			return fmt.Errorf("invalid library priority")
		}
		seen[listID] = struct{}{}
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin library priority update: %w", err)
	}
	fail := func(cause error) error {
		_ = tx.Rollback()
		return cause
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM library_list_priorities`); err != nil {
		return fail(fmt.Errorf("clear library priority: %w", err))
	}
	for position, listID := range priority {
		if _, err := tx.ExecContext(ctx, `INSERT INTO library_list_priorities(list_id,position) VALUES(?,?)`, listID, position); err != nil {
			return fail(fmt.Errorf("write library priority: %w", err))
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit library priority update: %w", err)
	}
	return nil
}

var _ application.LibraryPriorityRepository = (*Store)(nil)
