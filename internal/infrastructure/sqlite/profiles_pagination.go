package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Muratovnik/routevane/internal/application"
)

// ProfilePage returns the profiles of one shelf newest first, continuing
// strictly after afterID. The returned identity is a keyset cursor: unlike an
// offset, it cannot skip or repeat an older row if a newer profile is created
// between two requests, and it positions a page on either shelf because it names
// a creation time rather than a row of that shelf.
//
// The archived count is read in the same transaction as the rows, so a page and
// the count printed beside it describe one moment of the library.
func (s *Store) ProfilePage(ctx context.Context, shelf application.ProfileShelf, afterID string) (application.ProfilePage, error) {
	if !validShelf(shelf) {
		return application.ProfilePage{}, fmt.Errorf("unknown profile shelf %q", shelf)
	}
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

	profiles, err := profilePageRows(ctx, tx, shelf, afterID)
	if err != nil {
		return fail(err)
	}
	var archived int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM profiles WHERE deleted_at_ns=0 AND archived_at_ns<>0`).Scan(&archived); err != nil {
		return fail(fmt.Errorf("count archived profiles: %w", err))
	}
	nextCursor := ""
	if len(profiles) > profileLimit {
		nextCursor = profiles[profileLimit-1].ID
		profiles = profiles[:profileLimit]
	}
	if err := readProfileDetails(ctx, tx, profiles); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return application.ProfilePage{}, fmt.Errorf("commit profile page read: %w", err)
	}
	return application.ProfilePage{Profiles: profiles, NextCursor: nextCursor, ArchivedCount: archived}, nil
}

func validShelf(shelf application.ProfileShelf) bool {
	switch shelf {
	case application.ProfileShelfAll, application.ProfileShelfActive, application.ProfileShelfArchived:
		return true
	}
	return false
}

func profilePageRows(ctx context.Context, tx *sql.Tx, shelf application.ProfileShelf, afterID string) ([]application.Profile, error) {
	query := profileColumns + ` FROM profiles WHERE deleted_at_ns=0`
	args := make([]any, 0, 4)
	switch shelf {
	case application.ProfileShelfActive:
		query += ` AND archived_at_ns=0`
	case application.ProfileShelfArchived:
		query += ` AND archived_at_ns<>0`
	}
	if afterID != "" {
		// The cursor is a position rather than a read of that profile: one
		// deleted between two pages still marks where the next page begins.
		var createdAt int64
		if err := tx.QueryRowContext(ctx, `SELECT created_at_ns FROM profiles WHERE id=?`, afterID).Scan(&createdAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, application.ErrNotFound
			}
			return nil, fmt.Errorf("read profile page cursor: %w", err)
		}
		query += ` AND (created_at_ns < ? OR (created_at_ns = ? AND id > ?))`
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
