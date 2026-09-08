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

// UpdateProfileRefreshInterval writes only the operator-owned scheduling
// preference and its edit time. A scheduler completion cannot call this path.
func (s *Store) UpdateProfileRefreshInterval(ctx context.Context, profileID string, interval application.RefreshInterval, updatedAt time.Time) error {
	if !validID(profileID) || updatedAt.IsZero() {
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
	result, err := s.db.ExecContext(ctx,
		`UPDATE profiles SET refresh_interval=?, updated_at_ns=? WHERE id=?`,
		string(interval), updatedAt.UTC().UnixNano(), profileID)
	if err != nil {
		return fmt.Errorf("update profile refresh interval: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return application.ErrNotFound
	}
	return nil
}

// RecordProfileRefreshResult writes only scheduler-owned runtime metadata. It
// deliberately leaves refresh_interval and updated_at_ns untouched because an
// operator may have changed either while source I/O was in flight.
func (s *Store) RecordProfileRefreshResult(ctx context.Context, profileID string, lastRefreshedAt time.Time, failed bool) error {
	if !validID(profileID) || lastRefreshedAt.IsZero() {
		return fmt.Errorf("invalid profile refresh result")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	marked := 0
	if failed {
		marked = 1
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE profiles SET last_refreshed_at_ns=?, last_refresh_failed=? WHERE id=?`,
		lastRefreshedAt.UTC().UnixNano(), marked, profileID)
	if err != nil {
		return fmt.Errorf("record profile refresh result: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return application.ErrNotFound
	}
	return nil
}
