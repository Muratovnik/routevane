package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Muratovnik/routevane/internal/application"
)

// UpdateOutputKeepAddresses stores whether an output keeps the addresses a
// name already covers. Whether the output's format can hold the setting is the
// application's judgement: the store knows no target constraints.
func (s *Store) UpdateOutputKeepAddresses(ctx context.Context, outputID string, keep bool) error {
	if !validID(outputID) {
		return application.ErrNotFound
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin output addresses update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var profileID string
	err = tx.QueryRowContext(ctx, `SELECT profile_id FROM outputs WHERE id=? AND removed_at_ns=0`, outputID).Scan(&profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read output addresses profile: %w", err)
	}
	if err := requireWritableProfile(ctx, tx, profileID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE outputs SET keep_addresses=? WHERE id=? AND removed_at_ns=0`, keep, outputID)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return application.ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit output addresses update: %w", err)
	}
	return nil
}
