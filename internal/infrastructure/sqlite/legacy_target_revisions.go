package sqlite

import (
	"context"
	"fmt"

	"github.com/Muratovnik/routevane/internal/application"
)

// RebaseLegacyTargetRevisions moves the outputs schema version 19 marked to
// their target's own revision (ADR 0054). A marked output is moved only when
// its target, format key, renderer and renderer version all equal a format the
// running process serves; its marker is then removed, which makes the
// revision immutable again. A retired output never builds again, so its marker
// is removed without a change. Any other marked output is left as it is: it
// keeps failing with a changed target, and a later start may still move it if
// its format returns unchanged.
//
// The whole pass is one transaction, so a failure leaves every output and
// marker exactly as it was and the next start repeats it.
func (s *Store) RebaseLegacyTargetRevisions(ctx context.Context, formats []application.TargetFormat) (int, error) {
	byTarget := make(map[string]application.TargetFormat, len(formats))
	for _, format := range formats {
		if format.TargetID == "" || format.FormatKey == "" || format.RendererID == "" || format.RendererVersion == "" || format.Revision == "" {
			return 0, fmt.Errorf("invalid target format")
		}
		if _, duplicate := byTarget[format.TargetID]; duplicate {
			return 0, fmt.Errorf("duplicate target format %q", format.TargetID)
		}
		byTarget[format.TargetID] = format
	}
	moved := 0
	err := schemaStep(ctx, func(ctx context.Context) error {
		moved = 0
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin target revision rebase: %w", err)
		}
		defer func() { _ = tx.Rollback() }()
		type marked struct {
			id, targetID, formatKey, rendererID, rendererVersion string
			removed                                              bool
		}
		rows, err := tx.QueryContext(ctx, `SELECT o.id,o.target_id,o.format_key,o.renderer_id,o.renderer_version,o.removed_at_ns<>0
			FROM legacy_target_revisions l JOIN outputs o ON o.id=l.output_id ORDER BY o.id`)
		if err != nil {
			return fmt.Errorf("read legacy target revisions: %w", err)
		}
		var pending []marked
		for rows.Next() {
			var row marked
			if err := rows.Scan(&row.id, &row.targetID, &row.formatKey, &row.rendererID, &row.rendererVersion, &row.removed); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan legacy target revision: %w", err)
			}
			pending = append(pending, row)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("read legacy target revisions: %w", err)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read legacy target revisions: %w", err)
		}
		for _, row := range pending {
			if !row.removed {
				format, ok := byTarget[row.targetID]
				if !ok || format.FormatKey != row.formatKey || format.RendererID != row.rendererID || format.RendererVersion != row.rendererVersion {
					continue
				}
				if _, err := tx.ExecContext(ctx, `UPDATE outputs SET target_revision=? WHERE id=?`, format.Revision, row.id); err != nil {
					return fmt.Errorf("rebase output %s target revision: %w", row.id, err)
				}
				moved++
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM legacy_target_revisions WHERE output_id=?`, row.id); err != nil {
				return fmt.Errorf("close output %s legacy target revision: %w", row.id, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit target revision rebase: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return moved, nil
}
