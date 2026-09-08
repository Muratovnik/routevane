package sqlite

import (
	"context"
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
	result, err := s.db.ExecContext(ctx, `UPDATE outputs SET fqdn_group_prefix=? WHERE id=? AND target_id='keenetic-dns'`, prefix, outputID)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return application.ErrNotFound
	}
	return nil
}
