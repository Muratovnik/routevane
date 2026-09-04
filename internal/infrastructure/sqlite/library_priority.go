package sqlite

import (
	"context"
	"fmt"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

const libraryPriorityLimit = 128

// DefaultPriority reads the sparse library-wide preference in stored order.
// The application layer owns catalog filtering and canonical append behavior;
// this boundary intentionally returns stale rows too so no destructive cleanup
// is coupled to a catalog change.
func (s *Store) DefaultPriority(ctx context.Context) ([]string, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT service_id FROM library_service_priorities ORDER BY position ASC LIMIT ?`, libraryPriorityLimit)
	if err != nil {
		return nil, fmt.Errorf("read library priority: %w", err)
	}
	defer rows.Close()
	priority := make([]string, 0)
	for rows.Next() {
		var serviceID string
		if err := rows.Scan(&serviceID); err != nil {
			return nil, fmt.Errorf("read library priority: %w", err)
		}
		priority = append(priority, serviceID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read library priority: %w", err)
	}
	return priority, nil
}

// SetDefaultPriority replaces the sparse preference as one transaction. The
// application has already checked that the values form a full permutation of
// current services; the store still validates its own row grammar so direct
// callers cannot create duplicate positions or malformed ids.
func (s *Store) SetDefaultPriority(ctx context.Context, priority []string) error {
	if len(priority) > libraryPriorityLimit {
		return fmt.Errorf("invalid library priority")
	}
	seen := make(map[string]struct{}, len(priority))
	for _, serviceID := range priority {
		if domain.ValidateSlug(serviceID) != nil {
			return fmt.Errorf("invalid library priority")
		}
		if _, duplicate := seen[serviceID]; duplicate {
			return fmt.Errorf("invalid library priority")
		}
		seen[serviceID] = struct{}{}
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM library_service_priorities`); err != nil {
		return fail(fmt.Errorf("clear library priority: %w", err))
	}
	for position, serviceID := range priority {
		if _, err := tx.ExecContext(ctx, `INSERT INTO library_service_priorities(service_id,position) VALUES(?,?)`, serviceID, position); err != nil {
			return fail(fmt.Errorf("write library priority: %w", err))
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit library priority update: %w", err)
	}
	return nil
}

var _ application.LibraryPriorityRepository = (*Store)(nil)
