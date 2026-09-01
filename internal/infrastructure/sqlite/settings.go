package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

// Setting reads one service-wide preference. A key that was never written is
// the empty string rather than an error: an unset preference is a fact, and the
// caller owns what its default means.
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	if key == "" || len(key) > 64 {
		return "", fmt.Errorf("invalid setting key")
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read setting: %w", err)
	}
	return value, nil
}

func (s *Store) PutSetting(ctx context.Context, key, value string, updatedAt time.Time) error {
	if key == "" || len(key) > 64 || len(value) > 256 || updatedAt.IsZero() {
		return fmt.Errorf("invalid setting")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings(key,value,updated_at_ns) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at_ns=excluded.updated_at_ns`,
		key, value, updatedAt.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("write setting: %w", err)
	}
	return nil
}

// UpdateListSchedule writes only the scheduling columns, so a timer never
// rewrites a composition and a composition edit never resets when the list last
// refreshed.
func (s *Store) UpdateListSchedule(ctx context.Context, listID string, interval application.RefreshInterval, lastRefreshedAt time.Time, failed bool, updatedAt time.Time) error {
	if !validID(listID) || updatedAt.IsZero() {
		return fmt.Errorf("invalid list schedule")
	}
	switch interval {
	case application.RefreshDefault, application.RefreshOff, application.RefreshDaily, application.RefreshWeekly:
	default:
		return fmt.Errorf("invalid refresh interval")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	refreshed := int64(0)
	if !lastRefreshedAt.IsZero() {
		refreshed = lastRefreshedAt.UTC().UnixNano()
	}
	marked := 0
	if failed {
		marked = 1
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE lists SET refresh_interval=?, last_refreshed_at_ns=?, last_refresh_failed=?, updated_at_ns=? WHERE id=?`,
		string(interval), refreshed, marked, updatedAt.UTC().UnixNano(), listID)
	if err != nil {
		return fmt.Errorf("update list schedule: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return application.ErrNotFound
	}
	return nil
}
