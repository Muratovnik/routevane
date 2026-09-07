package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Muratovnik/routevane/internal/application"
	"github.com/Muratovnik/routevane/internal/domain"
)

// The overlay reads are bounded and refuse to truncate. A silently shortened
// membership set would change what a route publishes without saying so, which
// is the one failure this table must not have; a shortened listing of custom
// categories would hide a category the operator created. Both bounds are one
// step above what the writes above them allow, so an honest store never meets
// them.
const (
	customCategoryLimit     = 256
	categoryMembershipLimit = 16384
	// maxCategoryTitleLength is the stored title bound, in characters, and is
	// the same number the application's grammar enforces.
	maxCategoryTitleLength = 80
)

// validCustomCategory checks one row against the grammar the schema promises:
// the reserved prefix, a slug identity, and a bounded title.
func validCustomCategory(category application.CustomCategory) bool {
	if !strings.HasPrefix(category.ID, application.CustomCategoryIDPrefix) || domain.ValidateSlug(category.ID) != nil {
		return false
	}
	return category.Title != "" && len([]rune(category.Title)) <= maxCategoryTitleLength
}

// validMembership checks one overlay row. A category the operator created has
// no catalog membership to remove, so only 'added' is meaningful for it — the
// schema refuses the other case too, and refusing here names it.
func validMembership(membership application.CategoryMembership) bool {
	if domain.ValidateSlug(membership.CategoryID) != nil || domain.ValidateSlug(membership.ListID) != nil {
		return false
	}
	if membership.UpdatedAt.IsZero() {
		return false
	}
	switch membership.State {
	case application.MembershipAdded:
		return true
	case application.MembershipRemoved:
		return !strings.HasPrefix(membership.CategoryID, application.CustomCategoryIDPrefix)
	default:
		return false
	}
}

// CreateCustomCategory writes one operator-created category and its initial
// membership in one transaction: a category that existed for a moment without
// the services it was created with would be a category no one asked for.
func (s *Store) CreateCustomCategory(ctx context.Context, category application.CustomCategory, memberships []application.CategoryMembership) error {
	if !validCustomCategory(category) || category.CreatedAt.IsZero() || category.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid custom category")
	}
	if err := validMemberships(category.ID, memberships); err != nil {
		return err
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin custom category: %w", err)
	}
	fail := func(cause error) error { _ = tx.Rollback(); return cause }
	// A taken identifier is the one failure the caller retries; everything else
	// is a real error and must not be spent on eight more attempts.
	var taken int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM custom_categories WHERE id=?`, category.ID).Scan(&taken); err != nil {
		return fail(fmt.Errorf("check custom category identity: %w", err))
	}
	if taken != 0 {
		return fail(application.ErrIdentityCollision)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO custom_categories(id,title,created_at_ns,updated_at_ns) VALUES(?,?,?,?)`,
		category.ID, category.Title, category.CreatedAt.UTC().UnixNano(), category.UpdatedAt.UTC().UnixNano()); err != nil {
		return fail(fmt.Errorf("insert custom category: %w", err))
	}
	if err := writeMemberships(ctx, tx, category.ID, memberships); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit custom category: %w", err)
	}
	return nil
}

// UpdateCategory applies one category edit as a unit: the title, when the
// identity carries the reserved operator prefix, and the complete membership
// overlay when the request restated it. One request states both, so a store
// that applied half of it would leave a category no one asked for.
func (s *Store) UpdateCategory(ctx context.Context, write application.CategoryWrite) error {
	custom := strings.HasPrefix(write.CategoryID, application.CustomCategoryIDPrefix)
	if domain.ValidateSlug(write.CategoryID) != nil || write.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid category write")
	}
	// A catalog category owns its own title; there is no row to carry another
	// one, so carrying one here is a caller error rather than a silent no-op.
	if custom != (write.Title != "") {
		return fmt.Errorf("invalid category write")
	}
	if custom && len([]rune(write.Title)) > maxCategoryTitleLength {
		return fmt.Errorf("invalid category write")
	}
	if err := validMemberships(write.CategoryID, write.Memberships); err != nil {
		return err
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin category update: %w", err)
	}
	fail := func(cause error) error { _ = tx.Rollback(); return cause }
	if custom {
		result, err := tx.ExecContext(ctx,
			`UPDATE custom_categories SET title=?,updated_at_ns=? WHERE id=?`,
			write.Title, write.UpdatedAt.UTC().UnixNano(), write.CategoryID)
		if err != nil {
			return fail(fmt.Errorf("update custom category: %w", err))
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			return fail(application.ErrNotFound)
		}
	}
	if write.ReplaceMemberships {
		if _, err := tx.ExecContext(ctx, `DELETE FROM category_memberships WHERE category_id=?`, write.CategoryID); err != nil {
			return fail(fmt.Errorf("replace category membership: %w", err))
		}
		if err := writeMemberships(ctx, tx, write.CategoryID, write.Memberships); err != nil {
			return fail(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit category update: %w", err)
	}
	return nil
}

// CategoryOverlay reads everything the operator stored about the library in
// one pass, so the registry that merges it with the shipped catalog is
// hydrated from one coherent read.
func (s *Store) CategoryOverlay(ctx context.Context) (application.CategoryOverlay, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	overlay := application.CategoryOverlay{
		Categories:  make([]application.CustomCategory, 0),
		Memberships: make([]application.CategoryMembership, 0),
		Removals:    make([]application.CatalogRemoval, 0),
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,title,created_at_ns,updated_at_ns FROM custom_categories ORDER BY id ASC LIMIT ?`, customCategoryLimit+1)
	if err != nil {
		return application.CategoryOverlay{}, fmt.Errorf("list custom categories: %w", err)
	}
	for rows.Next() {
		var category application.CustomCategory
		var created, updated int64
		if err := rows.Scan(&category.ID, &category.Title, &created, &updated); err != nil {
			_ = rows.Close()
			return application.CategoryOverlay{}, fmt.Errorf("read custom category: %w", err)
		}
		category.CreatedAt, category.UpdatedAt = unixNanos(created), unixNanos(updated)
		if !validCustomCategory(category) {
			_ = rows.Close()
			return application.CategoryOverlay{}, fmt.Errorf("invalid stored custom category")
		}
		overlay.Categories = append(overlay.Categories, category)
	}
	if err := closeRows(rows, "list custom categories"); err != nil {
		return application.CategoryOverlay{}, err
	}
	if len(overlay.Categories) > customCategoryLimit {
		return application.CategoryOverlay{}, fmt.Errorf("stored custom categories exceed their bound")
	}
	memberships, err := s.db.QueryContext(ctx,
		`SELECT category_id,list_id,state,updated_at_ns FROM category_memberships ORDER BY category_id ASC, list_id ASC LIMIT ?`, categoryMembershipLimit+1)
	if err != nil {
		return application.CategoryOverlay{}, fmt.Errorf("list category membership: %w", err)
	}
	for memberships.Next() {
		var membership application.CategoryMembership
		var state string
		var updated int64
		if err := memberships.Scan(&membership.CategoryID, &membership.ListID, &state, &updated); err != nil {
			_ = memberships.Close()
			return application.CategoryOverlay{}, fmt.Errorf("read category membership: %w", err)
		}
		membership.State, membership.UpdatedAt = application.MembershipState(state), unixNanos(updated)
		if !validMembership(membership) {
			_ = memberships.Close()
			return application.CategoryOverlay{}, fmt.Errorf("invalid stored category membership")
		}
		overlay.Memberships = append(overlay.Memberships, membership)
	}
	if err := closeRows(memberships, "list category membership"); err != nil {
		return application.CategoryOverlay{}, err
	}
	if len(overlay.Memberships) > categoryMembershipLimit {
		return application.CategoryOverlay{}, fmt.Errorf("stored category membership exceeds its bound")
	}
	// Removals travel with membership because the merge subtracts them before
	// it applies membership; reading them a moment later could promise a
	// category a list the operator had already deleted.
	removals, err := s.catalogRemovals(ctx)
	if err != nil {
		return application.CategoryOverlay{}, err
	}
	overlay.Removals = removals
	return overlay, nil
}

func validMemberships(categoryID string, memberships []application.CategoryMembership) error {
	if len(memberships) > categoryMembershipLimit {
		return fmt.Errorf("invalid category membership")
	}
	seen := make(map[string]struct{}, len(memberships))
	for _, membership := range memberships {
		if membership.CategoryID != categoryID || !validMembership(membership) {
			return fmt.Errorf("invalid category membership")
		}
		if _, duplicate := seen[membership.ListID]; duplicate {
			return fmt.Errorf("invalid category membership")
		}
		seen[membership.ListID] = struct{}{}
	}
	return nil
}

func writeMemberships(ctx context.Context, tx *sql.Tx, categoryID string, memberships []application.CategoryMembership) error {
	for _, membership := range memberships {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO category_memberships(category_id,list_id,state,updated_at_ns) VALUES(?,?,?,?)`,
			categoryID, membership.ListID, string(membership.State), membership.UpdatedAt.UTC().UnixNano()); err != nil {
			return fmt.Errorf("write category membership: %w", err)
		}
	}
	return nil
}

// closeRows reports the first of the two failures a cursor can hold, so a read
// that stopped early is never mistaken for one that finished.
func closeRows(rows *sql.Rows, operation string) error {
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("%s: %w", operation, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}
