package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

// Deleting from the library (ADR 0029). A shipped object leaves a removal
// record, because the catalog file is never edited and a catalog update must
// not resurrect what the operator removed. An operator-created object leaves
// nothing: its own rows are the whole of it, so deleting them is the deletion,
// and the schema refuses a removal record for one — such a record would
// subtract an object that is already gone and would then outlive whatever
// later took the identity.
//
// Whatever the kind, the state that only made sense while the object existed
// goes in the same transaction: a category takes its membership, and a list
// takes its verdicts, its source overrides, its feeds and every membership row
// naming it. A surviving row would attach itself to the next object with that
// identity.

// operatorPrefix is the reserved identity prefix both operator-created kinds
// carry. The application exports it once, under the name of the kind whose
// grammar first enforced it.
const operatorPrefix = application.CustomCategoryIDPrefix

const (
	// catalogRemovalLimit bounds the removal read and refuses to truncate: a
	// shortened read would resurrect an object the operator deleted, which is
	// the one failure this table must not have. It is one step above what the
	// application allows to hold, so an honest store never meets it.
	catalogRemovalLimit = 2048
	// removedListsLimit bounds the lists one category deletion may take with
	// it, above the membership bound the application enforces.
	removedListsLimit = 1024
	// removedAtLayout is the stored moment's format. It is text rather than
	// the nanosecond integer the rest of the schema uses because a removal is
	// read as a record of an operator action, never compared or ordered
	// against the observation timeline.
	removedAtLayout = time.RFC3339Nano
)

// RemoveFromLibrary applies one deletion as a unit: the object, the lists a
// category takes with it, and everything those rows owned.
func (s *Store) RemoveFromLibrary(ctx context.Context, removal application.LibraryRemoval) error {
	if err := validLibraryRemoval(removal); err != nil {
		return err
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin library removal: %w", err)
	}
	fail := func(cause error) error { _ = tx.Rollback(); return cause }
	stamp := removal.RemovedAt.UTC().Format(removedAtLayout)
	services := removal.Services
	if removal.Kind == application.RemovalCategory {
		if err := removeCategoryRows(ctx, tx, removal.ID, stamp); err != nil {
			return fail(err)
		}
	} else {
		services = []string{removal.ID}
	}
	for _, serviceID := range services {
		if err := removeServiceRows(ctx, tx, serviceID, stamp); err != nil {
			return fail(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit library removal: %w", err)
	}
	return nil
}

func validLibraryRemoval(removal application.LibraryRemoval) error {
	switch removal.Kind {
	case application.RemovalCategory, application.RemovalList:
	default:
		return fmt.Errorf("invalid library removal kind")
	}
	if domain.ValidateSlug(removal.ID) != nil || removal.RemovedAt.IsZero() {
		return fmt.Errorf("invalid library removal")
	}
	// Only a category holds lists, so only a category deletion may name any.
	if removal.Kind != application.RemovalCategory && len(removal.Services) != 0 {
		return fmt.Errorf("invalid library removal")
	}
	if len(removal.Services) > removedListsLimit {
		return fmt.Errorf("invalid library removal")
	}
	seen := make(map[string]struct{}, len(removal.Services))
	for _, serviceID := range removal.Services {
		if domain.ValidateSlug(serviceID) != nil {
			return fmt.Errorf("invalid library removal")
		}
		if _, duplicate := seen[serviceID]; duplicate {
			return fmt.Errorf("invalid library removal")
		}
		seen[serviceID] = struct{}{}
	}
	return nil
}

func removeCategoryRows(ctx context.Context, tx *sql.Tx, id, stamp string) error {
	if strings.HasPrefix(id, operatorPrefix) {
		result, err := tx.ExecContext(ctx, `DELETE FROM custom_categories WHERE id=?`, id)
		if err != nil {
			return fmt.Errorf("delete custom category: %w", err)
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			return application.ErrNotFound
		}
	} else if err := recordRemoval(ctx, tx, application.RemovalCategory, id, stamp); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM category_memberships WHERE category_id=?`, id); err != nil {
		return fmt.Errorf("delete category membership: %w", err)
	}
	return nil
}

func removeServiceRows(ctx context.Context, tx *sql.Tx, id, stamp string) error {
	if strings.HasPrefix(id, operatorPrefix) {
		result, err := tx.ExecContext(ctx, `DELETE FROM custom_lists WHERE id=?`, id)
		if err != nil {
			return fmt.Errorf("delete custom list: %w", err)
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			return application.ErrNotFound
		}
	} else if err := recordRemoval(ctx, tx, application.RemovalList, id, stamp); err != nil {
		return err
	}
	for _, owned := range []struct{ statement, operation string }{
		{`DELETE FROM list_domain_verdicts WHERE list_id=?`, "delete list verdicts"},
		{`DELETE FROM list_disabled_sources WHERE list_id=?`, "delete list source overrides"},
		{`DELETE FROM custom_sources WHERE list_id=?`, "delete list feeds"},
		{`DELETE FROM category_memberships WHERE list_id=?`, "delete list membership"},
	} {
		if _, err := tx.ExecContext(ctx, owned.statement, id); err != nil {
			return fmt.Errorf("%s: %w", owned.operation, err)
		}
	}
	return nil
}

// recordRemoval keeps the first moment rather than the latest. A second
// deletion of the same object is a caller that read a stale library; the
// record already there is the one that describes what happened.
func recordRemoval(ctx context.Context, tx *sql.Tx, kind application.RemovalKind, id, stamp string) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO catalog_removals(kind,id,removed_at) VALUES(?,?,?)
ON CONFLICT(kind,id) DO NOTHING`, string(kind), id, stamp); err != nil {
		return fmt.Errorf("record catalog removal: %w", err)
	}
	return nil
}

// catalogRemovals reads the removal half of the overlay. It is called from the
// overlay read rather than on its own, so the merge subtracts removals from
// the same moment it applies membership.
func (s *Store) catalogRemovals(ctx context.Context) ([]application.CatalogRemoval, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT kind,id,removed_at FROM catalog_removals ORDER BY kind ASC, id ASC LIMIT ?`, catalogRemovalLimit+1)
	if err != nil {
		return nil, fmt.Errorf("list catalog removals: %w", err)
	}
	removals := make([]application.CatalogRemoval, 0)
	for rows.Next() {
		var kind, id, stamp string
		if err := rows.Scan(&kind, &id, &stamp); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("read catalog removal: %w", err)
		}
		moment, err := time.Parse(removedAtLayout, stamp)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("invalid stored catalog removal moment")
		}
		removal := application.CatalogRemoval{Kind: application.RemovalKind(kind), ID: id, RemovedAt: moment.UTC()}
		if !validCatalogRemoval(removal) {
			_ = rows.Close()
			return nil, fmt.Errorf("invalid stored catalog removal")
		}
		removals = append(removals, removal)
	}
	if err := closeRows(rows, "list catalog removals"); err != nil {
		return nil, err
	}
	if len(removals) > catalogRemovalLimit {
		return nil, fmt.Errorf("stored catalog removals exceed their bound")
	}
	return removals, nil
}

// validCatalogRemoval checks one stored row against the grammar the schema
// promises: a known kind, a slug identity, one outside the reserved operator
// prefix, and a moment that happened.
func validCatalogRemoval(removal application.CatalogRemoval) bool {
	switch removal.Kind {
	case application.RemovalCategory, application.RemovalList:
	default:
		return false
	}
	if domain.ValidateSlug(removal.ID) != nil || strings.HasPrefix(removal.ID, operatorPrefix) {
		return false
	}
	return !removal.RemovedAt.IsZero()
}
