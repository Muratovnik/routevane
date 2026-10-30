package sqlite

import (
	"context"
	"fmt"

	"github.com/Muratovnik/routevane/internal/application"
)

// DeviceUses lists every live format bound to a connection with the profile
// that owns it, grouped by connection, in one query. Removing a format or
// deleting a profile clears the binding in the same transaction, so neither is
// listed; the removed and deleted conditions repeat that rule rather than rely
// on it. An archived profile keeps its bindings and is marked.
func (s *Store) DeviceUses(ctx context.Context) (map[string][]application.DeviceUse, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT o.device_id, p.id, p.name, o.id, o.target_id, p.archived_at_ns<>0
	FROM outputs AS o JOIN profiles AS p ON p.id=o.profile_id
	WHERE o.device_id IS NOT NULL AND o.removed_at_ns=0 AND p.deleted_at_ns=0
	ORDER BY o.device_id ASC, p.archived_at_ns<>0 ASC, p.name ASC, p.id ASC, o.target_id ASC, o.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list device uses: %w", err)
	}
	defer func() { _ = rows.Close() }()
	uses := make(map[string][]application.DeviceUse)
	for rows.Next() {
		var deviceID string
		var use application.DeviceUse
		if err := rows.Scan(&deviceID, &use.ProfileID, &use.ProfileName, &use.OutputID, &use.TargetID, &use.Archived); err != nil {
			return nil, fmt.Errorf("list device uses: %w", err)
		}
		uses[deviceID] = append(uses[deviceID], use)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list device uses: %w", err)
	}
	return uses, nil
}
