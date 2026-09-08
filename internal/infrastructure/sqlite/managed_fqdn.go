package sqlite

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Muratovnik/routevane/internal/application"
)

func (s *Store) ManagedFQDNOwnership(ctx context.Context, endpoint, outputID string) (application.ManagedFQDNOwnership, error) {
	state := application.ManagedFQDNOwnership{Endpoint: endpoint, OutputID: outputID}
	if err := state.Validate(); err != nil {
		return state, err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT state_json FROM managed_fqdn_groups WHERE endpoint=? AND output_id=? ORDER BY name`, endpoint, outputID)
	if err != nil {
		return state, fmt.Errorf("read FQDN ownership: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var raw string
		var group application.ManagedFQDNGroup
		if err := rows.Scan(&raw); err != nil {
			return state, err
		}
		if err := json.Unmarshal([]byte(raw), &group); err != nil {
			return state, err
		}
		state.Groups = append(state.Groups, group)
	}
	if err := rows.Err(); err != nil {
		return state, err
	}
	return state, state.Validate()
}

func (s *Store) ReplaceManagedFQDNOwnership(ctx context.Context, state application.ManagedFQDNOwnership) error {
	if err := state.Validate(); err != nil {
		return err
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM managed_fqdn_groups WHERE endpoint=? AND output_id=?`, state.Endpoint, state.OutputID); err != nil {
		return err
	}
	for _, group := range state.Groups {
		encoded, err := json.Marshal(group)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO managed_fqdn_groups(endpoint,output_id,name,interface,state_json) VALUES(?,?,?,?,?)`, state.Endpoint, state.OutputID, group.Name, group.Interface, string(encoded)); err != nil {
			return fmt.Errorf("persist FQDN ownership: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) RetireManagedFQDNOwnership(ctx context.Context, endpoint, deviceInterface string) error {
	if endpoint == "" || deviceInterface == "" {
		return fmt.Errorf("invalid FQDN retirement scope")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `DELETE FROM managed_fqdn_groups WHERE endpoint=? AND interface=?`, endpoint, deviceInterface)
	return err
}
