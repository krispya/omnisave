package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

func (r *Repository) UpsertDevice(ctx context.Context, device catalog.Device) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO devices(id, name, platform, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name, platform = excluded.platform, last_seen_at = excluded.last_seen_at`,
		device.ID, device.Name, device.Platform,
		device.CreatedAt.Format(time.RFC3339Nano), device.LastSeenAt.Format(time.RFC3339Nano),
	)
	return err
}

// TrackGame upserts one provenance record. A repeated track refreshes the
// device's presence and clears any earlier untracking; the first tracked
// time never changes. Tracking is also a liveness signal for the device.
func (r *Repository) TrackGame(ctx context.Context, gameID string, record catalog.GameTracking) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var known bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM games WHERE id = ?) AND EXISTS(SELECT 1 FROM devices WHERE id = ?)`,
		gameID, record.DeviceID,
	).Scan(&known); err != nil {
		return err
	}
	if !known {
		return storage.ErrNotFound
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO game_tracking(
		game_id, device_id, adapter, installed, first_tracked_at, last_seen_at, untracked_at
	) VALUES (?, ?, ?, ?, ?, ?, NULL)
	ON CONFLICT(game_id, device_id) DO UPDATE SET
		adapter = excluded.adapter, installed = excluded.installed,
		last_seen_at = excluded.last_seen_at, untracked_at = NULL`,
		gameID, record.DeviceID, record.Adapter, record.Installed,
		record.FirstTrackedAt.Format(time.RFC3339Nano), record.LastSeenAt.Format(time.RFC3339Nano),
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET last_seen_at = ? WHERE id = ?`,
		record.LastSeenAt.Format(time.RFC3339Nano), record.DeviceID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) UntrackGame(ctx context.Context, gameID, deviceID string, at time.Time) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE game_tracking SET untracked_at = ? WHERE game_id = ? AND device_id = ?`,
		at.Format(time.RFC3339Nano), gameID, deviceID,
	)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (r *Repository) ListGameProvenance(ctx context.Context, gameID string) ([]catalog.GameTracking, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT
		t.device_id, d.name, t.adapter, t.installed, t.first_tracked_at, t.last_seen_at, t.untracked_at
		FROM game_tracking t JOIN devices d ON d.id = t.device_id
		WHERE t.game_id = ? ORDER BY t.first_tracked_at, t.device_id`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]catalog.GameTracking, 0)
	for rows.Next() {
		var record catalog.GameTracking
		var firstTracked, lastSeen string
		var untracked sql.NullString
		if err := rows.Scan(
			&record.DeviceID, &record.DeviceName, &record.Adapter, &record.Installed,
			&firstTracked, &lastSeen, &untracked,
		); err != nil {
			return nil, err
		}
		if record.FirstTrackedAt, err = time.Parse(time.RFC3339Nano, firstTracked); err != nil {
			return nil, err
		}
		if record.LastSeenAt, err = time.Parse(time.RFC3339Nano, lastSeen); err != nil {
			return nil, err
		}
		if untracked.Valid {
			at, err := time.Parse(time.RFC3339Nano, untracked.String)
			if err != nil {
				return nil, err
			}
			record.UntrackedAt = &at
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
