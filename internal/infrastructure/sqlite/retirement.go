package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

// Retirement turns a profile or one of its formats into a tombstone (ADR 0051).
// The row stays, because published plans, artifacts, attempts and ownership
// history point at it and are never deleted. What changes is that the object
// stops answering: its tokens are revoked, its binding to a device is cleared,
// and every read filters it out. The schema refuses any later change to a
// retired row, so each step below happens once, in one transaction.

// DeleteProfile retires an archived profile together with every format it
// still has.
func (s *Store) DeleteProfile(ctx context.Context, profileID string, deletedAt time.Time) error {
	if !validID(profileID) {
		return application.ErrNotFound
	}
	if deletedAt.IsZero() {
		return fmt.Errorf("invalid profile deletion")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin profile deletion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var archivedAt int64
	err = tx.QueryRowContext(ctx, `SELECT archived_at_ns FROM profiles WHERE id=? AND deleted_at_ns=0`, profileID).Scan(&archivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read profile archival: %w", err)
	}
	if archivedAt == 0 {
		return application.ErrProfileNotArchived
	}
	moment := deletedAt.UTC().UnixNano()
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscription_revocations(token_id,revoked_at_ns)
		SELECT token_id,? FROM subscriptions
		WHERE output_id IN (SELECT id FROM outputs WHERE profile_id=? AND removed_at_ns=0)
		AND token_id NOT IN (SELECT token_id FROM subscription_revocations)`, moment, profileID); err != nil {
		return fmt.Errorf("revoke profile subscriptions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE outputs SET removed_at_ns=?, device_id=NULL WHERE profile_id=? AND removed_at_ns=0`, moment, profileID); err != nil {
		return fmt.Errorf("retire profile outputs: %w", err)
	}
	// The composition goes with the profile so a list or a category it named
	// can leave the library. Nothing else reads it once the profile is gone.
	if err := clearComposition(ctx, tx, profileID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE profiles SET deleted_at_ns=?, updated_at_ns=? WHERE id=? AND deleted_at_ns=0`, moment, moment, profileID)
	if err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return application.ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit profile deletion: %w", err)
	}
	return nil
}

// RemoveOutput retires one format of a profile that is still on the shelf. An
// archived profile is frozen, its formats included, so it refuses as every
// other change to it does.
func (s *Store) RemoveOutput(ctx context.Context, outputID string, removedAt time.Time) error {
	if !validID(outputID) {
		return application.ErrNotFound
	}
	if removedAt.IsZero() {
		return fmt.Errorf("invalid output removal")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin output removal: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := requireWritableOutput(ctx, tx, outputID); err != nil {
		return err
	}
	moment := removedAt.UTC().UnixNano()
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscription_revocations(token_id,revoked_at_ns)
		SELECT token_id,? FROM subscriptions
		WHERE output_id=? AND token_id NOT IN (SELECT token_id FROM subscription_revocations)`, moment, outputID); err != nil {
		return fmt.Errorf("revoke output subscriptions: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE outputs SET removed_at_ns=?, device_id=NULL WHERE id=? AND removed_at_ns=0`, moment, outputID)
	if err != nil {
		return fmt.Errorf("remove output: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return application.ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit output removal: %w", err)
	}
	return nil
}

// ReissueSubscription replaces an output's active token. The old token is
// revoked rather than changed, and the new one is stored only as its identity
// and hash, exactly like the first one (ADR 0004).
func (s *Store) ReissueSubscription(ctx context.Context, outputID, tokenID string, tokenHash [32]byte, issuedAt time.Time) error {
	if !validID(outputID) {
		return application.ErrNotFound
	}
	if !validID(tokenID) || tokenHash == ([32]byte{}) || issuedAt.IsZero() {
		return fmt.Errorf("invalid subscription")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin subscription reissue: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := requireWritableOutput(ctx, tx, outputID); err != nil {
		return err
	}
	active, err := activeSubscriptions(ctx, tx, outputID)
	if err != nil {
		return err
	}
	if active == 0 {
		return application.ErrSubscriptionMissing
	}
	var taken int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM subscriptions WHERE token_id=?`, tokenID).Scan(&taken); err != nil {
		return fmt.Errorf("check subscription identity: %w", err)
	}
	if taken != 0 {
		return application.ErrIdentityCollision
	}
	moment := issuedAt.UTC().UnixNano()
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscription_revocations(token_id,revoked_at_ns)
		SELECT token_id,? FROM subscriptions
		WHERE output_id=? AND token_id NOT IN (SELECT token_id FROM subscription_revocations)`, moment, outputID); err != nil {
		return fmt.Errorf("revoke previous subscription: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscriptions(token_id,output_id,token_hash,created_at_ns) VALUES(?,?,?,?)`, tokenID, outputID, tokenHash[:], moment); err != nil {
		return fmt.Errorf("insert subscription: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit subscription reissue: %w", err)
	}
	return nil
}

// requireWritableOutput is requireWritableProfile for a mutation addressed to
// an output: the output must be live and its profile neither archived nor
// deleted, read in the mutation's own transaction.
func requireWritableOutput(ctx context.Context, tx *sql.Tx, outputID string) error {
	var profileID string
	err := tx.QueryRowContext(ctx, `SELECT profile_id FROM outputs WHERE id=? AND removed_at_ns=0`, outputID).Scan(&profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read output profile: %w", err)
	}
	return requireWritableProfile(ctx, tx, profileID)
}
