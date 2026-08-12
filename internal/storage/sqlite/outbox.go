package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/storage/store"
	sqlitedriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Store action kinds. They are data rather than callbacks so a committed
// database transaction can replay them after a crash.
const (
	storeActionRecordRevision = "record_revision"
	storeActionRecordOmnisave = "record_omnisave"
	storeActionRecordGame     = "record_game"
	storeActionDelete         = "delete"
)

func enqueueStoreAction(ctx context.Context, tx *sql.Tx, kind, targetID string, payload any) error {
	if payload == nil {
		return errors.New("store outbox action requires a self-contained payload")
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO store_outbox(kind, target_id, payload, created_at)
		VALUES (?, ?, ?, ?)`, kind, targetID, string(content), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func enqueueDeletion(ctx context.Context, tx *sql.Tx, marker store.Deletion) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO deletion_ledger(target_kind, target_id, deleted_at)
		VALUES (?, ?, ?) ON CONFLICT(target_kind, target_id) DO NOTHING`, marker.TargetKind,
		marker.TargetID, marker.DeletedAt.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return enqueueStoreAction(ctx, tx, storeActionDelete, marker.TargetID, marker)
}

func (r *Repository) enqueueOmnisaveProjection(ctx context.Context, tx *sql.Tx, id string) error {
	record, err := r.buildOmnisaveFrom(ctx, tx, id)
	if err != nil {
		return err
	}
	return enqueueStoreAction(ctx, tx, storeActionRecordOmnisave, id, record)
}

func (r *Repository) enqueueRevisionProjection(ctx context.Context, tx *sql.Tx, id string) error {
	manifest, err := r.buildRevisionFrom(ctx, tx, id)
	if err != nil {
		return err
	}
	return enqueueStoreAction(ctx, tx, storeActionRecordRevision, id, manifest)
}

func (r *Repository) enqueueGameProjection(ctx context.Context, tx *sql.Tx, id string) error {
	var record store.Game
	if err := tx.QueryRowContext(ctx, `SELECT id, title, sort_title, platform,
		platform_company, publisher FROM games WHERE id = ?`, id).Scan(
		&record.ID, &record.Title, &record.SortTitle, &record.Platform,
		&record.PlatformCompany, &record.Publisher,
	); err != nil {
		return err
	}
	var err error
	if record.Identifiers, err = listGameIdentifiersFrom(ctx, tx, id); err != nil {
		return err
	}
	if record.Fingerprints, err = listGameFingerprintsFrom(ctx, tx, id); err != nil {
		return err
	}
	return enqueueStoreAction(ctx, tx, storeActionRecordGame, id, record)
}

// flushStoreOutbox applies idempotent portable-store actions in database order.
func (r *Repository) flushStoreOutbox(ctx context.Context) error {
	for {
		flushed, err := r.flushNextStoreAction(ctx, r.applyStoreAction)
		if err != nil {
			return err
		}
		if !flushed {
			return nil
		}
	}
}

type storeActionApplier func(context.Context, string, string, string) error

// flushNextStoreAction holds SQLite's writer position through one durable projection.
func (r *Repository) flushNextStoreAction(
	ctx context.Context,
	apply storeActionApplier,
) (bool, error) {
	tx, err := r.beginStoreProjection(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var action struct {
		id       int64
		kind     string
		targetID string
		payload  string
	}
	err = tx.QueryRowContext(ctx, `SELECT id, kind, target_id, payload
		FROM store_outbox ORDER BY id LIMIT 1`).Scan(
		&action.id, &action.kind, &action.targetID, &action.payload)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if err := apply(ctx, action.kind, action.targetID, action.payload); err != nil {
		return false, fmt.Errorf("apply store outbox action %d (%s %s): %w",
			action.id, action.kind, action.targetID, err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM store_outbox WHERE id = ?`, action.id)
	if err != nil {
		return false, err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if deleted != 1 {
		return false, fmt.Errorf("store outbox action %d lost its database claim", action.id)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// beginStoreProjection retries until it obtains SQLite's global writer position.
func (r *Repository) beginStoreProjection(ctx context.Context) (*sql.Tx, error) {
	delay := time.Millisecond
	for {
		tx, err := r.db.BeginTx(ctx, nil)
		if err == nil {
			return tx, nil
		}
		if !sqliteLockContention(err) {
			return nil, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		if delay < 50*time.Millisecond {
			delay *= 2
		}
	}
}

func sqliteLockContention(err error) bool {
	var driverError *sqlitedriver.Error
	if !errors.As(err, &driverError) {
		return false
	}
	code := driverError.Code() & 0xff
	return code == sqlite3.SQLITE_BUSY || code == sqlite3.SQLITE_LOCKED
}

func (r *Repository) applyStoreAction(ctx context.Context, kind, targetID, payload string) error {
	switch kind {
	case storeActionRecordRevision:
		var manifest store.Revision
		if err := json.Unmarshal([]byte(payload), &manifest); err != nil {
			return err
		}
		if manifest.ID != targetID {
			return errors.New("revision projection identity does not match outbox target")
		}
		return r.store.PutRevision(manifest)
	case storeActionRecordOmnisave:
		var record store.Omnisave
		if err := json.Unmarshal([]byte(payload), &record); err != nil {
			return err
		}
		if record.ID != targetID {
			return errors.New("save projection identity does not match outbox target")
		}
		return r.store.PutOmnisave(record)
	case storeActionRecordGame:
		var record store.Game
		if err := json.Unmarshal([]byte(payload), &record); err != nil {
			return err
		}
		if record.ID != targetID {
			return errors.New("game projection identity does not match outbox target")
		}
		return r.store.PutGame(record)
	case storeActionDelete:
		var marker store.Deletion
		if err := json.Unmarshal([]byte(payload), &marker); err != nil {
			return err
		}
		if marker.TargetID != targetID {
			return errors.New("deletion projection identity does not match outbox target")
		}
		return r.store.PutDeletion(marker)
	default:
		return fmt.Errorf("unknown store action %q", kind)
	}
}

// projectStore completes the portable half of a committed mutation even when
// the request context was canceled. Failure makes later durable mutations stop
// at the repository boundary until reopen replays the queued work.
func (r *Repository) projectStore(ctx context.Context) error {
	err := r.flushStoreOutbox(context.WithoutCancel(ctx))
	if err != nil {
		r.storeErr = err
	}
	return err
}

func (r *Repository) requireStoreReady() error {
	if r.storeErr == nil {
		return nil
	}
	return fmt.Errorf("portable store projection unavailable: %w", r.storeErr)
}
