package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/device"
)

func (r *Repository) UpsertDevice(ctx context.Context, registered device.Device) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO devices(id, name, platform, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name, platform = excluded.platform, last_seen_at = excluded.last_seen_at`,
		registered.ID, registered.Name, registered.Platform,
		registered.CreatedAt.Format(time.RFC3339Nano), registered.LastSeenAt.Format(time.RFC3339Nano),
	)
	return err
}

func (r *Repository) GetDevice(ctx context.Context, id string) (*device.Device, error) {
	var found device.Device
	var createdAt, lastSeenAt string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, platform, created_at, last_seen_at FROM devices WHERE id = ?`, id,
	).Scan(&found.ID, &found.Name, &found.Platform, &createdAt, &lastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, device.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if found.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return nil, err
	}
	if found.LastSeenAt, err = time.Parse(time.RFC3339Nano, lastSeenAt); err != nil {
		return nil, err
	}
	return &found, nil
}
