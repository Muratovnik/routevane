package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Muratovnik/routevane/internal/application"
)

// ProfilePage returns profiles newest first, continuing strictly after afterID.
// The returned identity is a keyset cursor: unlike an offset, it cannot skip or
// repeat an older row if a newer profile is created between two requests.
func (s *Store) ProfilePage(ctx context.Context, afterID string) (application.ProfilePage, error) {
	if afterID != "" && !validID(afterID) {
		return application.ProfilePage{}, application.ErrNotFound
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ProfilePage{}, fmt.Errorf("begin profile page read: %w", err)
	}
	fail := func(cause error) (application.ProfilePage, error) {
		_ = tx.Rollback()
		return application.ProfilePage{}, cause
	}

	profiles, err := profilePageRows(ctx, tx, afterID)
	if err != nil {
		return fail(err)
	}
	nextCursor := ""
	if len(profiles) > profileLimit {
		nextCursor = profiles[profileLimit-1].ID
		profiles = profiles[:profileLimit]
	}
	for i := range profiles {
		if err := readComposition(ctx, tx, &profiles[i]); err != nil {
			return fail(err)
		}
		if err := readListDomains(ctx, tx, &profiles[i]); err != nil {
			return fail(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return application.ProfilePage{}, fmt.Errorf("commit profile page read: %w", err)
	}
	return application.ProfilePage{Profiles: profiles, NextCursor: nextCursor}, nil
}

func profilePageRows(ctx context.Context, tx *sql.Tx, afterID string) ([]application.Profile, error) {
	query := profileColumns + ` FROM profiles`
	args := make([]any, 0, 4)
	if afterID != "" {
		var createdAt int64
		if err := tx.QueryRowContext(ctx, `SELECT created_at_ns FROM profiles WHERE id=?`, afterID).Scan(&createdAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, application.ErrNotFound
			}
			return nil, fmt.Errorf("read profile page cursor: %w", err)
		}
		query += ` WHERE created_at_ns < ? OR (created_at_ns = ? AND id > ?)`
		args = append(args, createdAt, createdAt, afterID)
	}
	query += ` ORDER BY created_at_ns DESC, id ASC LIMIT ?`
	args = append(args, profileLimit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list profile page: %w", err)
	}
	defer func() { _ = rows.Close() }()
	profiles := make([]application.Profile, 0, profileLimit+1)
	for rows.Next() {
		profile, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list profile page: %w", err)
	}
	return profiles, nil
}
