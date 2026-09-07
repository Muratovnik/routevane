package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

// finishRows drains the cursor's terminal state: an iteration cut short by a
// cancelled context reports through Err, and a close failure must not read as
// a complete result set.
func finishRows(rows *sql.Rows, operation string) error {
	if err := rows.Err(); err != nil {
		return closeRowsWith(rows, fmt.Errorf("%s: %w", operation, err))
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

// tuningLimit bounds each registry read the same way the library read is
// bounded: the transport carries no pagination, so the bound lives here.
//
// verdictLimit is the verdict read's own bound. One service may stand behind
// 2048 destination verdicts, so a bound shared with the other two reads would
// be reached by a single tuned service and would silently drop the second
// service's stored decisions on load — the one failure a registry read must
// not have.
const (
	tuningLimit  = 2048
	verdictLimit = 16384
)

func validCustomSource(source application.CustomSource) bool {
	if !strings.HasPrefix(source.ID, "feed-") || domain.ValidateSlug(source.ID) != nil {
		return false
	}
	if domain.ValidateSlug(source.ServiceID) != nil {
		return false
	}
	if len(source.URL) < 12 || len(source.URL) > 2048 {
		return false
	}
	switch source.Format {
	case domain.FeedFormatText, domain.FeedFormatJSON, domain.FeedFormatDomainList:
	default:
		return false
	}
	return true
}

func (s *Store) SetSourceDisabled(ctx context.Context, serviceID, sourceID string, disabled bool) error {
	if domain.ValidateSlug(serviceID) != nil || domain.ValidateSlug(sourceID) != nil {
		return fmt.Errorf("invalid source override")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	if disabled {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO list_disabled_sources(list_id,source_id) VALUES(?,?)
ON CONFLICT(list_id,source_id) DO NOTHING`, serviceID, sourceID); err != nil {
			return fmt.Errorf("disable source: %w", err)
		}
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM list_disabled_sources WHERE list_id=? AND source_id=?`, serviceID, sourceID); err != nil {
		return fmt.Errorf("enable source: %w", err)
	}
	return nil
}

func (s *Store) CreateCustomSource(ctx context.Context, source application.CustomSource) error {
	if !validCustomSource(source) || source.CreatedAt.IsZero() || source.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid custom source")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	var taken int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM custom_sources WHERE id=?`, source.ID).Scan(&taken); err != nil {
		return fmt.Errorf("check custom source identity: %w", err)
	}
	if taken != 0 {
		return application.ErrIdentityCollision
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO custom_sources(id,list_id,url,format,created_at_ns,updated_at_ns) VALUES(?,?,?,?,?,?)`,
		source.ID, source.ServiceID, source.URL, string(source.Format),
		source.CreatedAt.UTC().UnixNano(), source.UpdatedAt.UTC().UnixNano()); err != nil {
		return fmt.Errorf("insert custom source: %w", err)
	}
	return nil
}

func (s *Store) RemoveCustomSource(ctx context.Context, id string) error {
	if !strings.HasPrefix(id, "feed-") || domain.ValidateSlug(id) != nil {
		return application.ErrNotFound
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `DELETE FROM custom_sources WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("remove custom source: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return application.ErrNotFound
	}
	// A verdict about the source's availability must not survive the source.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM list_disabled_sources WHERE source_id=?`, id); err != nil {
		return fmt.Errorf("remove custom source override: %w", err)
	}
	return nil
}

// SetDomainVerdicts writes one operator action about a batch of destinations.
// The list_domain_verdicts.domain column keeps its name for history and now
// carries any canonical destination: a domain, an IP address, or a network
// prefix. Every value of the batch is validated before anything is written and
// the whole batch travels in one transaction, so an imported file states all of
// its destinations or none of them.
func (s *Store) SetDomainVerdicts(ctx context.Context, serviceID string, values []string, verdict application.DomainVerdict) error {
	if domain.ValidateSlug(serviceID) != nil || len(values) == 0 {
		return fmt.Errorf("invalid destination verdict")
	}
	switch verdict {
	case application.DomainVerdictInclude, application.DomainVerdictExclude, application.DomainVerdictAuto:
	default:
		return fmt.Errorf("invalid destination verdict")
	}
	for _, value := range values {
		canonical, _, err := domain.NormalizeRuleValue(value)
		if err != nil || canonical != value {
			return fmt.Errorf("invalid destination verdict")
		}
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin destination verdicts: %w", err)
	}
	failed := func() error {
		for _, value := range values {
			if verdict == application.DomainVerdictAuto {
				if _, err := tx.ExecContext(ctx,
					`DELETE FROM list_domain_verdicts WHERE list_id=? AND domain=?`, serviceID, value); err != nil {
					return fmt.Errorf("reset destination verdict: %w", err)
				}
				continue
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO list_domain_verdicts(list_id,domain,verdict) VALUES(?,?,?)
ON CONFLICT(list_id,domain) DO UPDATE SET verdict=excluded.verdict`,
				serviceID, value, string(verdict)); err != nil {
				return fmt.Errorf("write destination verdict: %w", err)
			}
		}
		return nil
	}()
	if failed != nil {
		_ = tx.Rollback()
		return failed
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit destination verdicts: %w", err)
	}
	return nil
}

// ServiceTunings reads every stored correction in one pass, keyed by service.
// The registry the planner consults is hydrated from this at startup.
func (s *Store) ServiceTunings(ctx context.Context) (map[string]application.ServiceTuning, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	tunings := make(map[string]application.ServiceTuning)
	get := func(serviceID string) application.ServiceTuning {
		return tunings[serviceID]
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT list_id,source_id FROM list_disabled_sources ORDER BY list_id,source_id LIMIT ?`, tuningLimit)
	if err != nil {
		return nil, fmt.Errorf("read disabled sources: %w", err)
	}
	for rows.Next() {
		var serviceID, sourceID string
		if err := rows.Scan(&serviceID, &sourceID); err != nil {
			return nil, closeRowsWith(rows, fmt.Errorf("read disabled sources: %w", err))
		}
		tuning := get(serviceID)
		tuning.DisabledSources = append(tuning.DisabledSources, sourceID)
		tunings[serviceID] = tuning
	}
	if err := finishRows(rows, "read disabled sources"); err != nil {
		return nil, err
	}

	rows, err = s.db.QueryContext(ctx,
		`SELECT id,list_id,url,format,created_at_ns,updated_at_ns FROM custom_sources ORDER BY id LIMIT ?`, tuningLimit)
	if err != nil {
		return nil, fmt.Errorf("read custom sources: %w", err)
	}
	for rows.Next() {
		var source application.CustomSource
		var format string
		var created, updated int64
		if err := rows.Scan(&source.ID, &source.ServiceID, &source.URL, &format, &created, &updated); err != nil {
			return nil, closeRowsWith(rows, fmt.Errorf("read custom sources: %w", err))
		}
		source.Format = domain.FeedFormat(format)
		source.CreatedAt, source.UpdatedAt = unixNanos(created), unixNanos(updated)
		if !validCustomSource(source) {
			return nil, closeRowsWith(rows, fmt.Errorf("invalid stored custom source"))
		}
		tuning := get(source.ServiceID)
		tuning.CustomSources = append(tuning.CustomSources, source)
		tunings[source.ServiceID] = tuning
	}
	if err := finishRows(rows, "read custom sources"); err != nil {
		return nil, err
	}

	// The domain column carries any canonical destination — domain, address or
	// network — so the value is read as one string and classified above this
	// layer, where the rule kinds live.
	rows, err = s.db.QueryContext(ctx,
		`SELECT list_id,domain,verdict FROM list_domain_verdicts ORDER BY list_id,domain LIMIT ?`, verdictLimit)
	if err != nil {
		return nil, fmt.Errorf("read destination verdicts: %w", err)
	}
	for rows.Next() {
		var serviceID, value, verdict string
		if err := rows.Scan(&serviceID, &value, &verdict); err != nil {
			return nil, closeRowsWith(rows, fmt.Errorf("read destination verdicts: %w", err))
		}
		tuning := get(serviceID)
		switch application.DomainVerdict(verdict) {
		case application.DomainVerdictInclude:
			tuning.Includes = append(tuning.Includes, value)
		case application.DomainVerdictExclude:
			tuning.Excludes = append(tuning.Excludes, value)
		default:
			return nil, closeRowsWith(rows, fmt.Errorf("invalid stored destination verdict"))
		}
		tunings[serviceID] = tuning
	}
	if err := finishRows(rows, "read destination verdicts"); err != nil {
		return nil, err
	}
	return tunings, nil
}
