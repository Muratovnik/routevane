package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Muratovnik/routevane/internal/application"
)

func (s *Store) UpdateOutputFQDNPrefix(ctx context.Context, outputID, prefix string) error {
	if !validID(outputID) {
		return application.ErrNotFound
	}
	if err := application.ValidateFQDNPrefix("keenetic-dns", prefix); err != nil {
		return err
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin output prefix update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var profileID string
	err = tx.QueryRowContext(ctx, `SELECT profile_id FROM outputs WHERE id=? AND target_id='keenetic-dns'`, outputID).Scan(&profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read output prefix profile: %w", err)
	}
	if err := requireWritableProfile(ctx, tx, profileID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE outputs SET fqdn_group_prefix=? WHERE id=? AND target_id='keenetic-dns'`, prefix, outputID)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return application.ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit output prefix update: %w", err)
	}
	return nil
}
