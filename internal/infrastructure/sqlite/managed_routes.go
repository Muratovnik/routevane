package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
)

func validManagedScope(scope application.ManagedRouteScope) bool {
	return (application.ManagedRouteOwnership{Scope: scope}).Validate() == nil
}

func (s *Store) ManagedRouteOwnership(ctx context.Context, scope application.ManagedRouteScope) (application.ManagedRouteOwnership, error) {
	state := application.ManagedRouteOwnership{Scope: scope}
	if !validManagedScope(scope) {
		return state, fmt.Errorf("invalid managed route scope")
	}
	ctx, cancel := bounded(ctx)
	defer cancel()

	var scopeID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM managed_route_scopes WHERE endpoint=? AND target_id=? AND interface=? AND retired_at_ns=0`, scope.Endpoint, scope.TargetID, scope.Interface).Scan(&scopeID)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("read managed route scope: %w", err)
	}

	routes, err := s.db.QueryContext(ctx, `SELECT prefix,created_by_routevane FROM managed_routes WHERE scope_id=? ORDER BY prefix`, scopeID)
	if err != nil {
		return state, fmt.Errorf("read managed routes: %w", err)
	}
	for routes.Next() {
		var raw string
		var created int
		if err := routes.Scan(&raw, &created); err != nil {
			_ = routes.Close()
			return state, fmt.Errorf("scan managed route: %w", err)
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() || (created != 0 && created != 1) {
			_ = routes.Close()
			return state, fmt.Errorf("stored managed route is invalid")
		}
		state.Routes = append(state.Routes, application.ManagedRoute{Prefix: prefix, CreatedByRoutevane: created == 1})
	}
	if err := routes.Err(); err != nil {
		_ = routes.Close()
		return state, fmt.Errorf("read managed routes: %w", err)
	}
	if err := routes.Close(); err != nil {
		return state, fmt.Errorf("close managed routes: %w", err)
	}

	claims, err := s.db.QueryContext(ctx, `SELECT output_id,prefix FROM managed_route_claims WHERE scope_id=? ORDER BY output_id,prefix`, scopeID)
	if err != nil {
		return state, fmt.Errorf("read managed route claims: %w", err)
	}
	for claims.Next() {
		var outputID, raw string
		if err := claims.Scan(&outputID, &raw); err != nil {
			_ = claims.Close()
			return state, fmt.Errorf("scan managed route claim: %w", err)
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() {
			_ = claims.Close()
			return state, fmt.Errorf("stored managed route claim is invalid")
		}
		state.Claims = append(state.Claims, application.ManagedRouteClaim{OutputID: outputID, Prefix: prefix})
	}
	if err := claims.Err(); err != nil {
		_ = claims.Close()
		return state, fmt.Errorf("read managed route claims: %w", err)
	}
	if err := claims.Close(); err != nil {
		return state, fmt.Errorf("close managed route claims: %w", err)
	}
	if err := state.Validate(); err != nil {
		return application.ManagedRouteOwnership{Scope: scope}, fmt.Errorf("stored managed route ownership is invalid: %w", err)
	}
	return state, nil
}

func (s *Store) ReplaceManagedRouteOwnership(ctx context.Context, state application.ManagedRouteOwnership) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("invalid managed route ownership: %w", err)
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin managed route ownership: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var scopeID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM managed_route_scopes WHERE endpoint=? AND target_id=? AND interface=? AND retired_at_ns=0`, state.Scope.Endpoint, state.Scope.TargetID, state.Scope.Interface).Scan(&scopeID)
	if errors.Is(err, sql.ErrNoRows) {
		now := time.Now().UTC().UnixNano()
		result, insertErr := tx.ExecContext(ctx, `INSERT INTO managed_route_scopes(endpoint,target_id,interface,created_at_ns,updated_at_ns) VALUES(?,?,?,?,?)`, state.Scope.Endpoint, state.Scope.TargetID, state.Scope.Interface, now, now)
		if insertErr != nil {
			return fmt.Errorf("insert managed route scope: %w", insertErr)
		}
		scopeID, err = result.LastInsertId()
	} else if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE managed_route_scopes SET updated_at_ns=? WHERE id=?`, time.Now().UTC().UnixNano(), scopeID)
	}
	if err != nil {
		return fmt.Errorf("write managed route scope: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM managed_route_claims WHERE scope_id=?`, scopeID); err != nil {
		return fmt.Errorf("replace managed route claims: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM managed_routes WHERE scope_id=?`, scopeID); err != nil {
		return fmt.Errorf("replace managed routes: %w", err)
	}
	for _, route := range state.Routes {
		created := 0
		if route.CreatedByRoutevane {
			created = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO managed_routes(scope_id,prefix,created_by_routevane) VALUES(?,?,?)`, scopeID, route.Prefix.String(), created); err != nil {
			return fmt.Errorf("insert managed route: %w", err)
		}
	}
	for _, claim := range state.Claims {
		if _, err := tx.ExecContext(ctx, `INSERT INTO managed_route_claims(scope_id,output_id,prefix) VALUES(?,?,?)`, scopeID, claim.OutputID, claim.Prefix.String()); err != nil {
			return fmt.Errorf("insert managed route claim: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit managed route ownership: %w", err)
	}
	return nil
}

func (s *Store) RetireManagedRouteOwnership(ctx context.Context, scope application.ManagedRouteScope) error {
	if !validManagedScope(scope) {
		return fmt.Errorf("invalid managed route scope")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `UPDATE managed_route_scopes SET retired_at_ns=?,updated_at_ns=? WHERE endpoint=? AND target_id=? AND interface=? AND retired_at_ns=0`, time.Now().UTC().UnixNano(), time.Now().UTC().UnixNano(), scope.Endpoint, scope.TargetID, scope.Interface)
	if err != nil {
		return fmt.Errorf("retire managed route ownership: %w", err)
	}
	return nil
}
