package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

// customServiceLimit bounds the registry read the same way the library read is
// bounded: the transport carries no pagination, so the bound lives here.
const customServiceLimit = 200

// validCustomService checks one row against the grammar the schema promises:
// the reserved prefix, a slug identity, a bounded title, and a bounded set of
// normalized domains.
func validCustomService(service application.CustomService) bool {
	if !strings.HasPrefix(service.ID, "custom-") || domain.ValidateSlug(service.ID) != nil {
		return false
	}
	if service.Title == "" || len([]rune(service.Title)) > 120 {
		return false
	}
	if len(service.Domains) == 0 || len(service.Domains) > 64 || len(domain.StableStrings(service.Domains)) != len(service.Domains) {
		return false
	}
	for _, value := range service.Domains {
		normalized, err := domain.NormalizeDomain(value)
		if err != nil || normalized != value {
			return false
		}
	}
	return true
}

func (s *Store) CreateCustomService(ctx context.Context, service application.CustomService) error {
	if !validCustomService(service) || service.CreatedAt.IsZero() || service.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid custom service")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	payload, err := json.Marshal(service.Domains)
	if err != nil {
		return fmt.Errorf("encode custom service domains: %w", err)
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	// A taken identifier is the one failure the caller retries; everything
	// else is a real error and must not be spent on eight more attempts.
	var taken int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM custom_lists WHERE id=?`, service.ID).Scan(&taken); err != nil {
		return fmt.Errorf("check custom service identity: %w", err)
	}
	if taken != 0 {
		return application.ErrIdentityCollision
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO custom_lists(id,title,domains_json,created_at_ns,updated_at_ns) VALUES(?,?,?,?,?)`,
		service.ID, service.Title, string(payload), service.CreatedAt.UTC().UnixNano(), service.UpdatedAt.UTC().UnixNano()); err != nil {
		return fmt.Errorf("insert custom service: %w", err)
	}
	return nil
}

func (s *Store) UpdateCustomService(ctx context.Context, service application.CustomService) error {
	if !validCustomService(service) || service.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid custom service")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	payload, err := json.Marshal(service.Domains)
	if err != nil {
		return fmt.Errorf("encode custom service domains: %w", err)
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	result, err := s.db.ExecContext(ctx,
		`UPDATE custom_lists SET title=?,domains_json=?,updated_at_ns=? WHERE id=?`,
		service.Title, string(payload), service.UpdatedAt.UTC().UnixNano(), service.ID)
	if err != nil {
		return fmt.Errorf("update custom service: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return application.ErrNotFound
	}
	return nil
}

func (s *Store) CustomServices(ctx context.Context) ([]application.CustomService, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,title,domains_json,created_at_ns,updated_at_ns FROM custom_lists ORDER BY id ASC LIMIT ?`, customServiceLimit)
	if err != nil {
		return nil, fmt.Errorf("list custom services: %w", err)
	}
	defer func() { _ = rows.Close() }()
	services := make([]application.CustomService, 0)
	for rows.Next() {
		service, err := scanCustomService(rows)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list custom services: %w", err)
	}
	return services, nil
}

func scanCustomService(rows *sql.Rows) (application.CustomService, error) {
	var service application.CustomService
	var payload string
	var created, updated int64
	if err := rows.Scan(&service.ID, &service.Title, &payload, &created, &updated); err != nil {
		return application.CustomService{}, fmt.Errorf("read custom service: %w", err)
	}
	if err := json.Unmarshal([]byte(payload), &service.Domains); err != nil {
		return application.CustomService{}, fmt.Errorf("decode custom service domains: %w", err)
	}
	service.CreatedAt = unixNanos(created)
	service.UpdatedAt = unixNanos(updated)
	if !validCustomService(service) {
		return application.CustomService{}, fmt.Errorf("invalid stored custom service")
	}
	return service, nil
}
