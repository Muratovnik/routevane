package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Muratovnik/routevane/internal/application"
)

// deviceLimit bounds the registry read. The transport rejects query
// parameters, so the bound lives here rather than in the request.
const deviceLimit = 200

const deviceColumns = `SELECT id,target_id,name,address,account,interface,auto_deliver,created_at_ns,updated_at_ns`

func validDevice(device application.Device) bool {
	return validID(device.ID) &&
		device.TargetID != "" && len(device.TargetID) <= 64 &&
		device.Name != "" && len([]rune(device.Name)) <= 120 &&
		device.Address != "" && len(device.Address) <= 512 &&
		len([]rune(device.Account)) <= 120 &&
		len([]rune(device.Interface)) <= 120 &&
		!device.CreatedAt.IsZero() && !device.UpdatedAt.IsZero()
}

func (s *Store) CreateDevice(ctx context.Context, device application.Device) error {
	if !validDevice(device) {
		return fmt.Errorf("invalid device")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	var taken int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM devices WHERE id=?`, device.ID).Scan(&taken); err != nil {
		return fmt.Errorf("check device identity: %w", err)
	}
	if taken != 0 {
		return application.ErrIdentityCollision
	}
	autoDeliver := 0
	if device.AutoDeliver {
		autoDeliver = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO devices(id,target_id,name,address,account,interface,auto_deliver,created_at_ns,updated_at_ns) VALUES(?,?,?,?,?,?,?,?,?)`,
		device.ID, device.TargetID, device.Name, device.Address, device.Account, device.Interface, autoDeliver,
		device.CreatedAt.UTC().UnixNano(), device.UpdatedAt.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("insert device: %w", err)
	}
	return nil
}

func (s *Store) Device(ctx context.Context, id string) (application.Device, error) {
	if !validID(id) {
		return application.Device{}, application.ErrNotFound
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	return scanDevice(s.db.QueryRowContext(ctx, deviceColumns+` FROM devices WHERE id=?`, id))
}

func (s *Store) Devices(ctx context.Context) ([]application.Device, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, deviceColumns+` FROM devices ORDER BY created_at_ns ASC, id ASC LIMIT ?`, deviceLimit)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer func() { _ = rows.Close() }()
	devices := make([]application.Device, 0)
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	return devices, nil
}

// UpdateDevice replaces the editable parts and the delivery flag. The target is
// not editable: a device that became a different kind of device is a different
// device.
func (s *Store) UpdateDevice(ctx context.Context, device application.Device) error {
	if !validDevice(device) {
		return fmt.Errorf("invalid device")
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	autoDeliver := 0
	if device.AutoDeliver {
		autoDeliver = 1
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE devices SET name=?,address=?,account=?,interface=?,auto_deliver=?,updated_at_ns=? WHERE id=? AND target_id=?`,
		device.Name, device.Address, device.Account, device.Interface, autoDeliver, device.UpdatedAt.UTC().UnixNano(), device.ID, device.TargetID)
	if err != nil {
		return fmt.Errorf("update device: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return application.ErrNotFound
	}
	return nil
}

// DeleteDevice removes the entry. A device promises nothing to anyone -- unlike
// a published artifact, no subscription resolves to it -- so the append-only
// rule that protects publication does not apply here.
func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	if !validID(id) {
		return application.ErrNotFound
	}
	if err := s.preparePublication(); err != nil {
		return err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return application.ErrNotFound
	}
	return nil
}

func scanDevice(row rowScanner) (application.Device, error) {
	var device application.Device
	var autoDeliver int
	var created, updated int64
	if err := row.Scan(&device.ID, &device.TargetID, &device.Name, &device.Address, &device.Account, &device.Interface, &autoDeliver, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.Device{}, application.ErrNotFound
		}
		return application.Device{}, fmt.Errorf("read device: %w", err)
	}
	device.AutoDeliver = autoDeliver == 1
	device.CreatedAt = unixNanos(created)
	device.UpdatedAt = unixNanos(updated)
	return device, nil
}
