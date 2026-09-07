package sqlite

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

func (s *Store) CreateOutput(ctx context.Context, create application.NewOutput) error {
	o := create.Output
	hasToken := create.TokenID != "" || create.TokenHash != ([32]byte{})
	if !validID(o.ID) || !validID(o.ProfileID) || o.TargetID == "" || o.FormatKey == "" || o.RendererID == "" || o.RendererVersion == "" || o.TargetRevision == "" || o.CreatedAt.IsZero() || (hasToken && (!validID(create.TokenID) || create.TokenHash == ([32]byte{}))) {
		return fmt.Errorf("invalid output")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin output creation: %w", err)
	}
	fail := func(cause error) error { _ = tx.Rollback(); return cause }
	// A taken identifier is the one failure the caller retries. A second output
	// for the same format is a different answer entirely, and the application
	// layer's own check cannot see a request that arrived at the same moment.
	var taken int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM outputs WHERE id=? OR (? <> '' AND (SELECT count(*) FROM subscriptions WHERE token_id=?) > 0)`, o.ID, create.TokenID, create.TokenID).Scan(&taken); err != nil {
		return fail(fmt.Errorf("check output identity: %w", err))
	}
	if taken != 0 {
		return fail(application.ErrIdentityCollision)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM outputs WHERE profile_id=? AND target_id=?`, o.ProfileID, o.TargetID).Scan(&taken); err != nil {
		return fail(fmt.Errorf("check output target: %w", err))
	}
	if taken != 0 {
		return fail(application.ErrOutputExists)
	}
	var deviceID any
	if o.DeviceID != "" {
		deviceID = o.DeviceID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outputs(id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns,latest_artifact_id,previous_artifact_id,device_id) VALUES(?,?,?,?,?,?,?,?,NULL,NULL,?)`, o.ID, o.ProfileID, o.TargetID, o.FormatKey, o.RendererID, o.RendererVersion, o.TargetRevision, o.CreatedAt.UTC().UnixNano(), deviceID)
	if err != nil {
		return fail(fmt.Errorf("insert output: %w", err))
	}
	if hasToken {
		if _, err := tx.ExecContext(ctx, `INSERT INTO subscriptions(output_id,token_id,token_hash,created_at_ns) VALUES(?,?,?,?)`, o.ID, create.TokenID, create.TokenHash[:], o.CreatedAt.UTC().UnixNano()); err != nil {
			return fail(fmt.Errorf("insert subscription: %w", err))
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit output creation: %w", err)
	}
	return nil
}

func (s *Store) CreateSubscription(ctx context.Context, outputID, tokenID string, tokenHash [32]byte, createdAt time.Time) error {
	if !validID(outputID) || !validID(tokenID) || createdAt.IsZero() {
		return fmt.Errorf("invalid subscription")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin subscription creation: %w", err)
	}
	fail := func(cause error) error { _ = tx.Rollback(); return cause }
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM outputs WHERE id=?`, outputID).Scan(&exists); err != nil || exists != 1 {
		return fail(application.ErrNotFound)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM subscriptions WHERE output_id=?`, outputID).Scan(&exists); err != nil {
		return fail(fmt.Errorf("check output subscription: %w", err))
	}
	if exists != 0 {
		return fail(application.ErrSubscriptionExists)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM subscriptions WHERE token_id=?`, tokenID).Scan(&exists); err != nil {
		return fail(fmt.Errorf("check subscription identity: %w", err))
	}
	if exists != 0 {
		return fail(application.ErrIdentityCollision)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscriptions(output_id,token_id,token_hash,created_at_ns) VALUES(?,?,?,?)`, outputID, tokenID, tokenHash[:], createdAt.UTC().UnixNano()); err != nil {
		return fail(fmt.Errorf("insert subscription: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit subscription creation: %w", err)
	}
	return nil
}

const outputSelect = `SELECT id,profile_id,target_id,format_key,renderer_id,renderer_version,target_revision,created_at_ns,latest_artifact_id,previous_artifact_id,device_id FROM outputs`

func (s *Store) UpdateOutputDevice(ctx context.Context, outputID, deviceID string) error {
	if !validID(outputID) || (deviceID != "" && !validID(deviceID)) {
		return application.ErrNotFound
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	var value any
	if deviceID != "" {
		value = deviceID
	}
	result, err := s.db.ExecContext(ctx, `UPDATE outputs SET device_id=? WHERE id=?`, value, outputID)
	if err != nil {
		return fmt.Errorf("update output device: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return application.ErrNotFound
	}
	return nil
}

func (s *Store) Output(ctx context.Context, id string) (application.Output, error) {
	if !validID(id) {
		return application.Output{}, application.ErrNotFound
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	return scanOutput(s.db.QueryRowContext(ctx, outputSelect+` WHERE id=?`, id))
}

func (s *Store) OutputsByProfile(ctx context.Context, profileID string) ([]application.Output, error) {
	if !validID(profileID) {
		return nil, application.ErrNotFound
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	return s.scanOutputs(ctx, outputSelect+` WHERE profile_id=? ORDER BY created_at_ns ASC, id ASC LIMIT 64`, profileID)
}

// Outputs bounds the library read the same way Profiles does, and newest-first for
// the same reason: what falls off the end must be the oldest, never the output
// the operator just made.
func (s *Store) Outputs(ctx context.Context) ([]application.Output, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	return s.scanOutputs(ctx, outputSelect+` ORDER BY created_at_ns DESC, id ASC LIMIT ?`, profileLimit*4)
}

func (s *Store) scanOutputs(ctx context.Context, query string, args ...any) ([]application.Output, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list outputs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	outputs := make([]application.Output, 0)
	for rows.Next() {
		output, err := scanOutput(rows)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, output)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list outputs: %w", err)
	}
	return outputs, nil
}

func (s *Store) OutputBySubscription(ctx context.Context, tokenID string, tokenHash [32]byte) (application.Output, error) {
	if !validID(tokenID) {
		return application.Output{}, application.ErrNotFound
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	var outputID string
	var stored []byte
	if err := s.db.QueryRowContext(ctx, `SELECT output_id,token_hash FROM subscriptions WHERE token_id=?`, tokenID).Scan(&outputID, &stored); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.Output{}, application.ErrNotFound
		}
		return application.Output{}, fmt.Errorf("read subscription: %w", err)
	}
	if len(stored) != 32 || subtle.ConstantTimeCompare(stored, tokenHash[:]) != 1 {
		return application.Output{}, application.ErrNotFound
	}
	return s.Output(ctx, outputID)
}
