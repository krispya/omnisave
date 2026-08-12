package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/storage"
	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

// Portable-store writes are projected from SQLite's transactional outbox.

// noteStoreLag logs best-effort physical cleanup after its immutable deletion
// marker is durable. Failure retains bytes and is retried by a complete sweep.
func (r *Repository) noteStoreLag(what string, err error) {
	if err != nil {
		log.Printf("save store: could not record %s; reconciling on the next open repairs it: %v", what, err)
	}
}

type storeQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *Repository) buildRevisionFrom(
	ctx context.Context,
	queryer storeQueryer,
	revisionID string,
) (store.Revision, error) {
	var (
		manifest  store.Revision
		parent    sql.NullString
		createdAt string
		metadata  string
		gameID    string
		gameTitle string
		platform  sql.NullString
	)
	err := queryer.QueryRowContext(ctx, `SELECT
			revisions.id, revisions.parent_id, revisions.created_at, revisions.metadata,
			COALESCE(omnisaves.id, revisions.omnisave_id), COALESCE(omnisaves.display_name, ''), revisions.game_id,
			COALESCE(games.title, ''), games.platform
		FROM revisions
		LEFT JOIN omnisaves ON omnisaves.id = revisions.omnisave_id
		LEFT JOIN games ON games.id = revisions.game_id
		WHERE revisions.id = ?`, revisionID).Scan(
		&manifest.ID, &parent, &createdAt, &metadata,
		&manifest.Omnisave.ID, &manifest.Omnisave.DisplayName, &gameID,
		&gameTitle, &platform,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Revision{}, storage.ErrNotFound
	}
	if err != nil {
		return store.Revision{}, err
	}
	if parent.Valid {
		manifest.Parent = &parent.String
	}
	if manifest.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return store.Revision{}, err
	}
	if metadata != "" {
		if err := json.Unmarshal([]byte(metadata), &manifest.Metadata); err != nil {
			return store.Revision{}, err
		}
	}

	manifest.Game = store.RevisionGame{ID: gameID, Title: gameTitle, Platform: platform.String}
	if manifest.Game.Identifiers, err = listGameIdentifiersFrom(ctx, queryer, gameID); err != nil {
		return store.Revision{}, err
	}

	files, err := listRevisionFilesFrom(ctx, queryer, revisionID)
	if err != nil {
		return store.Revision{}, err
	}
	manifest.Files = make([]store.RevisionFile, 0, len(files))
	for _, file := range files {
		manifest.Files = append(manifest.Files, store.RevisionFile{
			Path:   file.Path,
			SHA256: file.Artifact.SHA256,
			Size:   file.Artifact.Size,
			Format: file.Artifact.Format,
		})
	}
	return manifest, nil
}

func (r *Repository) buildOmnisaveFrom(
	ctx context.Context,
	queryer storeQueryer,
	id string,
) (store.Omnisave, error) {
	var (
		record     store.Omnisave
		forkedSave sql.NullString
		forkedRev  sql.NullString
		createdAt  string
		metadata   string
	)
	err := queryer.QueryRowContext(ctx, `SELECT id, game_id, display_name, current_revision_id,
			forked_from_omnisave_id, forked_from_revision_id, created_at, metadata
		FROM omnisaves WHERE id = ?`, id).Scan(
		&record.ID, &record.GameID, &record.DisplayName, &record.CurrentRevisionID,
		&forkedSave, &forkedRev, &createdAt, &metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Omnisave{}, storage.ErrNotFound
	}
	if err != nil {
		return store.Omnisave{}, err
	}
	if forkedSave.Valid && forkedRev.Valid {
		record.ForkedFrom = &omnisave.ForkOrigin{
			OmnisaveID: forkedSave.String,
			RevisionID: forkedRev.String,
		}
	}
	if record.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return store.Omnisave{}, err
	}
	if metadata != "" {
		if err := json.Unmarshal([]byte(metadata), &record.Metadata); err != nil {
			return store.Omnisave{}, err
		}
	}
	rows, err := queryer.QueryContext(ctx, `WITH RECURSIVE members(id) AS (
		SELECT id FROM revisions WHERE omnisave_id = ?
		UNION SELECT current_revision_id FROM omnisaves WHERE id = ? AND current_revision_id IS NOT NULL
		UNION SELECT forked_from_revision_id FROM omnisaves WHERE id = ? AND forked_from_revision_id IS NOT NULL
		UNION SELECT revisions.parent_id FROM revisions JOIN members ON revisions.id = members.id
			WHERE revisions.parent_id IS NOT NULL
	) SELECT id, display_name, name_source FROM revisions
		WHERE id IN (SELECT id FROM members) AND display_name <> '' ORDER BY created_at, id`, id, id, id)
	if err != nil {
		return store.Omnisave{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var revisionID, displayName, nameSource string
		if err := rows.Scan(&revisionID, &displayName, &nameSource); err != nil {
			return store.Omnisave{}, err
		}
		if record.RevisionNames == nil {
			record.RevisionNames = make(map[string]string)
			record.RevisionNameSources = make(map[string]string)
		}
		record.RevisionNames[revisionID] = displayName
		if nameSource != "" {
			record.RevisionNameSources[revisionID] = nameSource
		}
	}
	if err := rows.Err(); err != nil {
		return store.Omnisave{}, err
	}
	return record, nil
}

// migrateLegacyDeletionMarkers turns version-3 mutable tombstones into the
// immutable committed facts version 4 uses. It is idempotent so an interrupted
// upgrade resumes on the next open.
func (r *Repository) migrateLegacyDeletionMarkers() bool {
	ctx := context.Background()
	complete := true
	note := func(id string, err error) {
		complete = false
		log.Printf("save store: could not migrate legacy deletion state for save %s: %v", id, err)
	}
	// Migration predates the outbox, so it is the one direct mutable store
	// writer. Hold SQLite's writer position throughout to keep it ordered with
	// every current projection in another Repository.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		note("namespace", err)
		return false
	}
	defer tx.Rollback()
	if err := r.store.EachOmnisaveID(func(id string) error {
		record, err := r.store.GetOmnisave(id)
		if err != nil {
			note(id, err)
			return nil
		}
		changed := false
		if record.DeletedAt != nil {
			var exists bool
			if err := tx.QueryRowContext(ctx,
				`SELECT EXISTS(SELECT 1 FROM omnisaves WHERE id = ?)`, id,
			).Scan(&exists); err != nil {
				note(id, err)
				return nil
			}
			// Version 3 wrote tombstones before commit, so a live row makes
			// their meaning ambiguous. Absence is not proof: preserve the row.
			if !exists {
				if err := r.store.PutDeletion(store.Deletion{
					TargetKind: store.DeletionOmnisave,
					TargetID:   record.ID,
					DeletedAt:  *record.DeletedAt,
				}); err != nil {
					note(id, err)
					return nil
				}
			}
			record.DeletedAt = nil
			changed = true
		}
		for _, revisionID := range record.DeletedRevisions {
			var exists bool
			if err := tx.QueryRowContext(ctx,
				`SELECT EXISTS(SELECT 1 FROM revisions WHERE id = ?)`, revisionID,
			).Scan(&exists); err != nil {
				note(id, err)
				return nil
			}
			if !exists {
				if err := r.store.PutDeletion(store.Deletion{
					TargetKind: store.DeletionRevision,
					TargetID:   revisionID,
					DeletedAt:  time.Now().UTC(),
				}); err != nil {
					note(id, err)
					return nil
				}
			}
			changed = true
		}
		if !changed {
			return nil
		}
		record.DeletedRevisions = nil
		if err := r.store.PutOmnisave(record); err != nil {
			note(id, err)
		}
		return nil
	}); err != nil {
		note("namespace", err)
	}
	if err := tx.Commit(); err != nil {
		note("namespace", err)
	}
	return complete
}

// dropRevisions removes manifests after the rows and immutable markers commit.
// Failing here only retains data; a complete later sweep retries the cleanup.
func (r *Repository) dropRevisions(revisionIDs []string) error {
	for _, revisionID := range revisionIDs {
		if err := r.store.RemoveRevision(revisionID); err != nil {
			return err
		}
	}
	return nil
}

// reconcile queues missing portable records in normal projection order.
func (r *Repository) reconcile(ctx context.Context) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var failures int
	note := func(what string, err error) {
		failures++
		log.Printf("save store: could not queue %s during reconcile: %v", what, err)
	}
	gameIDs, err := queryIDs(ctx, tx, `SELECT id FROM games ORDER BY id`)
	if err != nil {
		return false, fmt.Errorf("reconcile games: %w", err)
	}
	for _, id := range gameIDs {
		if err := r.enqueueGameProjection(ctx, tx, id); err != nil {
			note("game "+id, err)
		}
	}

	saveIDs, err := queryIDs(ctx, tx, `SELECT id FROM omnisaves ORDER BY id`)
	if err != nil {
		return false, fmt.Errorf("reconcile saves: %w", err)
	}
	for _, id := range saveIDs {
		if err := r.enqueueOmnisaveProjection(ctx, tx, id); err != nil {
			note("save "+id, err)
		}
	}

	// Include shared revisions whose creator save was deleted.
	revisionIDs, err := queryIDs(ctx, tx, `SELECT id FROM revisions ORDER BY id`)
	if err != nil {
		return false, fmt.Errorf("reconcile revisions: %w", err)
	}
	for _, id := range revisionIDs {
		if r.store.HasRevision(id) {
			continue
		}
		if err := r.enqueueRevisionProjection(ctx, tx, id); err != nil {
			note("revision "+id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if err := r.projectStore(ctx); err != nil {
		failures++
		log.Printf("save store: could not apply reconciled projections: %v", err)
	}
	if failures > 0 {
		log.Printf("save store: %d record(s) could not be reconciled; the database still holds them", failures)
	}
	return failures == 0, nil
}

// omnisaveIDsOfGame lists a game's saves, for a deletion that has to record
// the same in the store once the rows are gone.
func omnisaveIDsOfGame(ctx context.Context, tx *sql.Tx, gameID string) ([]string, error) {
	return queryIDs(ctx, tx, `SELECT id FROM omnisaves WHERE game_id = ?`, gameID)
}

// revisionIDsOfGame lists all graph nodes, including those created by deleted forks.
func revisionIDsOfGame(ctx context.Context, tx *sql.Tx, gameID string) ([]string, error) {
	return queryIDs(ctx, tx, `SELECT id FROM revisions WHERE game_id = ?`, gameID)
}

func queryIDs(ctx context.Context, tx *sql.Tx, query string, arguments ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
