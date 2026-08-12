// Package sqlite persists Omnisave metadata in SQLite and artifacts on disk.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/storage"
	"github.com/krisbaumgartner/omnisave/internal/storage/store"
	sqlitedriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type Repository struct {
	db    *sql.DB
	store *store.Store
	// storeErr stops later portable mutations after an outbox projection
	// failure. Open replays the queue before exposing the repository again.
	storeErr error
	// mutate serializes in-process mutations and their portable-store side effects.
	mutate sync.Mutex
}

// Open migrates SQLite, replays portable writes, and repairs database/store drift.
func Open(databasePath, storeDir string) (*Repository, error) {
	if err := os.MkdirAll(filepath.Dir(databasePath), 0755); err != nil {
		return nil, err
	}
	saveStore, err := store.Open(storeDir)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", sqliteDSN(databasePath))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	repository := &Repository{db: db, store: saveStore}
	if err := repository.flushStoreOutbox(context.Background()); err != nil {
		// Do not rebuild while committed outbox work is ahead of the store.
		repository.storeErr = fmt.Errorf("replay portable store outbox: %w", err)
		log.Printf("save store: %v; opened read-only with recovery deferred", repository.storeErr)
		return repository, nil
	}
	legacyComplete := repository.migrateLegacyDeletionMarkers()
	inventory := repository.inspectStore()
	inventory.complete = inventory.complete && legacyComplete
	inventory.deletionsComplete = inventory.deletionsComplete && legacyComplete
	imported, err := repository.rebuild(context.Background(), inventory)
	if err != nil {
		db.Close()
		return nil, err
	}
	// Reconcile can restore missing records even when inventory is incomplete.
	reconciled, err := repository.reconcile(context.Background())
	if err != nil {
		db.Close()
		return nil, err
	}
	recoveryComplete := imported && reconciled
	if !inventory.complete && inventory.deletionsComplete && reconciled {
		// The repair may have filled exactly the gaps the inventory reported.
		// Look again before settling for the degraded answer; damage that
		// reconcile cannot rewrite keeps it.
		inventory = repository.inspectStore()
		inventory.complete = inventory.complete && legacyComplete
		inventory.deletionsComplete = inventory.deletionsComplete && legacyComplete
		recoveryComplete = inventory.complete
	}
	if proof, ok := inventory.proof(); recoveryComplete && ok {
		// Reclamation requires the proof produced by the complete inventory.
		if err := repository.sweep(context.Background(), proof); err != nil {
			log.Printf("save store: sweep could not finish; the next open retries it: %v", err)
		}
	} else {
		repository.storeErr = errRecoveryInventoryIncomplete
		log.Printf("save store: opened in read-only recovery mode; durable mutations require a complete inventory")
	}
	return repository, nil
}

// sqliteDSN makes transactions acquire SQLite's database-wide writer order before reads.
func sqliteDSN(databasePath string) string {
	parameters := url.Values{
		"_pragma": {"busy_timeout(5000)"},
		"_txlock": {"immediate"},
	}
	return databasePath + "?" + parameters.Encode()
}

// Store is the portable save store this repository writes through.
func (r *Repository) Store() *store.Store { return r.store }

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) InsertOmnisave(ctx context.Context, save omnisave.Omnisave) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return err
	}
	if r.store.HasDeletion(store.DeletionOmnisave, save.ID) {
		return storage.ErrConflict
	}
	metadata, err := json.Marshal(save.Metadata)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO omnisaves(
			id, game_id, display_name, current_revision_id,
			forked_from_omnisave_id, forked_from_revision_id, created_at, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		save.ID, save.GameID, save.DisplayName, save.CurrentRevisionID,
		forkOmnisaveID(save.ForkedFrom), forkRevisionID(save.ForkedFrom),
		save.CreatedAt.Format(time.RFC3339Nano), string(metadata),
	)
	if err != nil {
		return translateUniqueViolation(err)
	}
	if err := r.enqueueOmnisaveProjection(ctx, tx, save.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return r.projectStore(ctx)
}

func (r *Repository) ListOmnisaves(ctx context.Context) ([]omnisave.Omnisave, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, game_id, display_name, current_revision_id,
			forked_from_omnisave_id, forked_from_revision_id, created_at,
			COALESCE(
				(SELECT created_at FROM revisions WHERE id = omnisaves.current_revision_id),
				created_at
			), metadata
		FROM omnisaves ORDER BY created_at, id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var saves []omnisave.Omnisave
	for rows.Next() {
		save, err := scanOmnisave(rows)
		if err != nil {
			return nil, err
		}
		saves = append(saves, *save)
	}
	return saves, rows.Err()
}

func (r *Repository) GetOmnisave(ctx context.Context, id string) (*omnisave.Omnisave, error) {
	save, err := scanOmnisave(r.db.QueryRowContext(ctx,
		`SELECT id, game_id, display_name, current_revision_id,
			forked_from_omnisave_id, forked_from_revision_id, created_at,
			COALESCE(
				(SELECT created_at FROM revisions WHERE id = omnisaves.current_revision_id),
				created_at
			), metadata
		FROM omnisaves WHERE id = ?`, id,
	))
	return save, translateNotFound(err)
}

func (r *Repository) UpdateOmnisaveDisplayName(ctx context.Context, id, displayName string) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx,
		`UPDATE omnisaves SET display_name = ? WHERE id = ?`, displayName, id,
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
	if err := r.enqueueOmnisaveProjection(ctx, tx, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return r.projectStore(ctx)
}

func (r *Repository) DeleteOmnisave(ctx context.Context, id string) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `DELETE FROM omnisaves WHERE id = ?`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		if r.store.HasDeletion(store.DeletionOmnisave, id) {
			return nil
		}
		return storage.ErrNotFound
	}

	// A deleted Omnisave stops owning its nodes, but a surviving fork may
	// still reach some of them through its fork point, its current revision,
	// or a revision it created. Only nodes unreachable from every surviving
	// save die.
	orphanedRevisionIDs, err := orphanRevisionIDs(ctx, tx)
	if err != nil {
		return err
	}
	revisionIDs, err := unreachableRevisionIDs(ctx, tx, orphanedRevisionIDs)
	if err != nil {
		return err
	}
	var hashes []string
	for _, revisionID := range revisionIDs {
		rows, err := tx.QueryContext(ctx,
			`SELECT DISTINCT artifact_sha256 FROM revision_files WHERE revision_id = ?`, revisionID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var hash string
			if err := rows.Scan(&hash); err != nil {
				rows.Close()
				return err
			}
			hashes = append(hashes, hash)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM revisions WHERE id = ?`, revisionID); err != nil {
			return err
		}
	}

	deletedAt := time.Now().UTC()
	if err := enqueueDeletion(ctx, tx, store.Deletion{
		TargetKind: store.DeletionOmnisave,
		TargetID:   id,
		DeletedAt:  deletedAt,
	}); err != nil {
		return err
	}
	for _, revisionID := range revisionIDs {
		if err := enqueueDeletion(ctx, tx, store.Deletion{
			TargetKind: store.DeletionRevision,
			TargetID:   revisionID,
			DeletedAt:  deletedAt,
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := r.projectStore(ctx); err != nil {
		return err
	}
	r.noteStoreLag("deletion of save "+id, r.dropRevisions(revisionIDs))
	r.reclaimArtifacts(ctx, hashes)
	return nil
}

func (r *Repository) ForkOmnisave(ctx context.Context, save omnisave.Omnisave) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return err
	}
	if r.store.HasDeletion(store.DeletionOmnisave, save.ID) {
		return storage.ErrConflict
	}
	saveMetadata, err := json.Marshal(save.Metadata)
	if err != nil {
		return err
	}
	if save.ForkedFrom == nil || save.CurrentRevisionID == nil ||
		*save.CurrentRevisionID != save.ForkedFrom.RevisionID {
		return storage.ErrNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var sourceGameID string
	if err := tx.QueryRowContext(ctx, `SELECT game_id FROM omnisaves WHERE id = ?`,
		save.ForkedFrom.OmnisaveID).Scan(&sourceGameID); err != nil {
		return translateNotFound(err)
	}
	member, err := revisionIsMember(ctx, tx, save.ForkedFrom.OmnisaveID, save.ForkedFrom.RevisionID)
	if err != nil {
		return err
	}
	if !member || sourceGameID != save.GameID {
		return storage.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO omnisaves(
		id, game_id, display_name, current_revision_id,
		forked_from_omnisave_id, forked_from_revision_id, created_at, metadata
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, save.ID, save.GameID, save.DisplayName,
		save.CurrentRevisionID, forkOmnisaveID(save.ForkedFrom), forkRevisionID(save.ForkedFrom),
		save.CreatedAt.Format(time.RFC3339Nano), string(saveMetadata)); err != nil {
		return translateUniqueViolation(err)
	}
	if err := r.enqueueOmnisaveProjection(ctx, tx, save.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return r.projectStore(ctx)
}

func (r *Repository) RestoreOmnisave(ctx context.Context, id, revisionID string, expectedCurrentRevisionID *string) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	actual, err := currentRevision(ctx, tx, id)
	if err != nil {
		return translateNotFound(err)
	}
	if !sameNullableString(actual, expectedCurrentRevisionID) {
		return &storage.CurrentRevisionConflict{ActualCurrentRevisionID: nullableStringPointer(actual)}
	}
	member, err := revisionIsMember(ctx, tx, id, revisionID)
	if err != nil {
		return err
	}
	if !member {
		return storage.ErrNotFound
	}
	result, err := tx.ExecContext(ctx, `UPDATE omnisaves SET current_revision_id = ?
		WHERE id = ? AND ((current_revision_id IS NULL AND ? IS NULL) OR current_revision_id = ?)`,
		revisionID, id, expectedCurrentRevisionID, expectedCurrentRevisionID)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil {
		return err
	} else if count == 0 {
		latest, lookupErr := currentRevision(ctx, tx, id)
		if lookupErr != nil {
			return translateNotFound(lookupErr)
		}
		return &storage.CurrentRevisionConflict{ActualCurrentRevisionID: nullableStringPointer(latest)}
	}
	if err := r.enqueueOmnisaveProjection(ctx, tx, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return r.projectStore(ctx)
}

func (r *Repository) CommitRevision(ctx context.Context, expectedCurrentRevisionID *string, revision omnisave.Revision) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return err
	}
	metadata, err := json.Marshal(revision.Metadata)
	if err != nil {
		return err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	actualCurrentRevisionID, err := currentRevision(ctx, tx, revision.OmnisaveID)
	if err != nil {
		return translateNotFound(err)
	}
	if !sameNullableString(actualCurrentRevisionID, expectedCurrentRevisionID) {
		return &storage.CurrentRevisionConflict{ActualCurrentRevisionID: nullableStringPointer(actualCurrentRevisionID)}
	}
	descriptors := make(map[string]int64, len(revision.Files))
	for _, file := range revision.Files {
		descriptors[file.Artifact.SHA256] = file.Artifact.Size
	}
	if err := r.requireAvailableArtifacts(ctx, tx, descriptors); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO revisions(
		id, game_id, omnisave_id, display_name, name_source, parent_id, created_at, metadata
	) SELECT ?, game_id, ?, ?, ?, ?, ?, ? FROM omnisaves WHERE id = ?`,
		revision.ID, revision.OmnisaveID, revision.DisplayName, revision.NameSource, revision.ParentID,
		revision.CreatedAt.Format(time.RFC3339Nano), string(metadata), revision.OmnisaveID)
	if err != nil {
		return translateUniqueViolation(err)
	}
	if err := insertRevisionFiles(ctx, tx, revision); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE omnisaves SET current_revision_id = ?
		WHERE id = ? AND ((current_revision_id IS NULL AND ? IS NULL) OR current_revision_id = ?)`,
		revision.ID, revision.OmnisaveID, expectedCurrentRevisionID, expectedCurrentRevisionID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		actual, lookupErr := currentRevision(ctx, tx, revision.OmnisaveID)
		if lookupErr != nil {
			return translateNotFound(lookupErr)
		}
		return &storage.CurrentRevisionConflict{ActualCurrentRevisionID: nullableStringPointer(actual)}
	}
	if err := r.enqueueRevisionProjection(ctx, tx, revision.ID); err != nil {
		return err
	}
	if err := r.enqueueOmnisaveProjection(ctx, tx, revision.OmnisaveID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return r.projectStore(ctx)
}

func (r *Repository) GetRevision(ctx context.Context, saveID, revisionID string) (*omnisave.Revision, error) {
	// The save has to exist before its membership is consulted: shared-ancestry
	// rows still name a deleted creator, and a deleted save's URLs must be dead.
	if _, err := r.GetOmnisave(ctx, saveID); err != nil {
		return nil, err
	}
	revision, err := scanRevision(r.db.QueryRowContext(ctx, `WITH RECURSIVE members(id) AS (
		SELECT id FROM revisions WHERE omnisave_id = ?
		UNION SELECT current_revision_id FROM omnisaves WHERE id = ? AND current_revision_id IS NOT NULL
		UNION SELECT forked_from_revision_id FROM omnisaves WHERE id = ? AND forked_from_revision_id IS NOT NULL
		UNION SELECT revisions.parent_id FROM revisions JOIN members ON revisions.id = members.id
			WHERE revisions.parent_id IS NOT NULL
	) SELECT
		id, omnisave_id, display_name, name_source, parent_id, created_at, metadata
		FROM revisions WHERE id = ? AND id IN (SELECT id FROM members)`, saveID, saveID, saveID, revisionID))
	if err != nil {
		return nil, translateNotFound(err)
	}
	revision.Files, err = r.listRevisionFiles(ctx, revision.ID)
	return revision, err
}

func (r *Repository) ListRevisions(ctx context.Context, saveID string) ([]omnisave.Revision, error) {
	if _, err := r.GetOmnisave(ctx, saveID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `WITH RECURSIVE members(id) AS (
		SELECT id FROM revisions WHERE omnisave_id = ?
		UNION SELECT current_revision_id FROM omnisaves WHERE id = ? AND current_revision_id IS NOT NULL
		UNION SELECT forked_from_revision_id FROM omnisaves WHERE id = ? AND forked_from_revision_id IS NOT NULL
		UNION SELECT revisions.parent_id FROM revisions JOIN members ON revisions.id = members.id
			WHERE revisions.parent_id IS NOT NULL
	) SELECT
		id, omnisave_id, display_name, name_source, parent_id, created_at, metadata
		FROM revisions WHERE id IN (SELECT id FROM members) ORDER BY created_at, id`, saveID, saveID, saveID)
	if err != nil {
		return nil, err
	}

	var revisions []omnisave.Revision
	for rows.Next() {
		revision, err := scanRevision(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		revisions = append(revisions, *revision)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range revisions {
		files, err := r.listRevisionFiles(ctx, revisions[index].ID)
		if err != nil {
			return nil, err
		}
		revisions[index].Files = files
	}
	return revisions, nil
}

// UpdateRevisionDisplayName renames a revision on behalf of a person, which is
// the only path that renames after commit, so it also stamps the name manual:
// automation may replace its own names but never one somebody chose.
func (r *Repository) UpdateRevisionDisplayName(ctx context.Context, saveID, revisionID, displayName string) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return err
	}
	if _, err := r.GetRevision(ctx, saveID, revisionID); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE revisions SET display_name = ?, name_source = ? WHERE id = ?`,
		displayName, omnisave.NameSourceManual, revisionID)
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
	// Revision labels are denormalized into lineage records. Snapshot every
	// live lineage in this transaction: it is deliberately broader than a
	// process-local membership preflight, and therefore stays correct when a
	// second repository creates a fork concurrently.
	saveIDs, err := queryIDs(ctx, tx, `SELECT id FROM omnisaves`)
	if err != nil {
		return err
	}
	for _, id := range saveIDs {
		if err := r.enqueueOmnisaveProjection(ctx, tx, id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return r.projectStore(ctx)
}

// DeleteRevision removes a reachable leaf that is neither current nor a fork origin.
func (r *Repository) DeleteRevision(ctx context.Context, saveID, revisionID string) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// The save has to exist and reach the revision through its membership, so
	// a deleted save's URLs stay dead — the same rule reads follow.
	var saveExists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM omnisaves WHERE id = ?)`, saveID,
	).Scan(&saveExists); err != nil {
		return err
	}
	if !saveExists {
		return storage.ErrNotFound
	}
	// Idempotent only through a live save: the marker check sits behind the
	// existence check so a deleted save's URLs answer not-found, not success.
	if r.store.HasDeletion(store.DeletionRevision, revisionID) {
		return nil
	}
	member, err := revisionIsMember(ctx, tx, saveID, revisionID)
	if err != nil {
		return err
	}
	if !member {
		return storage.ErrNotFound
	}
	var ownerID string
	if err := tx.QueryRowContext(ctx,
		`SELECT omnisave_id FROM revisions WHERE id = ?`, revisionID,
	).Scan(&ownerID); err != nil {
		return translateNotFound(err)
	}

	refusals := []struct {
		query  string
		reason string
	}{
		{`SELECT EXISTS(SELECT 1 FROM omnisaves WHERE current_revision_id = ?)`, storage.RevisionInUseCurrent},
		{`SELECT EXISTS(SELECT 1 FROM revisions WHERE parent_id = ?)`, storage.RevisionInUseChildren},
		{`SELECT EXISTS(SELECT 1 FROM omnisaves WHERE forked_from_revision_id = ?)`, storage.RevisionInUseForkOrigin},
	}
	for _, refusal := range refusals {
		var used bool
		if err := tx.QueryRowContext(ctx, refusal.query, revisionID).Scan(&used); err != nil {
			return err
		}
		if used {
			return &storage.RevisionInUse{Reason: refusal.reason}
		}
	}

	hashes, err := queryIDs(ctx, tx,
		`SELECT DISTINCT artifact_sha256 FROM revision_files WHERE revision_id = ?`, revisionID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM revisions WHERE id = ?`, revisionID); err != nil {
		return err
	}

	deletedAt := time.Now().UTC()
	if err := enqueueDeletion(ctx, tx, store.Deletion{
		TargetKind: store.DeletionRevision,
		TargetID:   revisionID,
		DeletedAt:  deletedAt,
	}); err != nil {
		return err
	}
	if err := r.enqueueOmnisaveProjection(ctx, tx, ownerID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := r.projectStore(ctx); err != nil {
		return err
	}
	r.noteStoreLag("deletion of revision "+revisionID, r.dropRevisions([]string{revisionID}))
	r.reclaimArtifacts(ctx, hashes)
	return nil
}

func unreachableRevisionIDs(
	ctx context.Context,
	tx *sql.Tx,
	candidates []string,
) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE retained(id) AS (
		SELECT revisions.id FROM revisions
		JOIN omnisaves ON omnisaves.id = revisions.omnisave_id
		UNION
		SELECT current_revision_id FROM omnisaves WHERE current_revision_id IS NOT NULL
		UNION
		SELECT forked_from_revision_id FROM omnisaves WHERE forked_from_revision_id IS NOT NULL
		UNION
		SELECT revisions.parent_id FROM revisions
		JOIN retained ON revisions.id = retained.id
		WHERE revisions.parent_id IS NOT NULL
	) SELECT id FROM retained`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	retained := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		retained[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var unreachable []string
	for _, id := range candidates {
		if !retained[id] {
			unreachable = append(unreachable, id)
		}
	}
	return unreachable, nil
}

func orphanRevisionIDs(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT revisions.id FROM revisions
		LEFT JOIN omnisaves ON omnisaves.id = revisions.omnisave_id
		WHERE omnisaves.id IS NULL`)
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

func (r *Repository) OpenArtifact(_ context.Context, hash string) (io.ReadCloser, error) {
	return r.openArtifact(hash)
}

func (r *Repository) StoreArtifact(_ context.Context, artifact storage.Artifact, payload io.Reader) error {
	return r.storeArtifact(artifact, payload)
}

func (r *Repository) StatArtifact(_ context.Context, hash string) (int64, error) {
	return r.statArtifact(hash)
}

func (r *Repository) listRevisionFiles(ctx context.Context, revisionID string) ([]omnisave.RevisionFile, error) {
	return listRevisionFilesFrom(ctx, r.db, revisionID)
}

func listRevisionFilesFrom(ctx context.Context, queryer storeQueryer, revisionID string) ([]omnisave.RevisionFile, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT path, artifact_format, artifact_sha256, artifact_size
		FROM revision_files WHERE revision_id = ? ORDER BY path`, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make([]omnisave.RevisionFile, 0)
	for rows.Next() {
		var file omnisave.RevisionFile
		if err := rows.Scan(&file.Path, &file.Artifact.Format, &file.Artifact.SHA256, &file.Artifact.Size); err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, rows.Err()
}

func insertRevisionFiles(ctx context.Context, tx *sql.Tx, revision omnisave.Revision) error {
	for _, file := range revision.Files {
		if _, err := tx.ExecContext(ctx, `INSERT INTO revision_files(
			revision_id, path, artifact_format, artifact_sha256, artifact_size
		) VALUES (?, ?, ?, ?, ?)`, revision.ID, file.Path, file.Artifact.Format,
			file.Artifact.SHA256, file.Artifact.Size); err != nil {
			return err
		}
	}
	return nil
}

func currentRevision(ctx context.Context, tx *sql.Tx, saveID string) (sql.NullString, error) {
	var current sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT current_revision_id FROM omnisaves WHERE id = ?`, saveID).Scan(&current)
	return current, err
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func revisionIsMember(ctx context.Context, queryer rowQueryer, saveID, revisionID string) (bool, error) {
	var member bool
	err := queryer.QueryRowContext(ctx, `WITH RECURSIVE members(id) AS (
		SELECT id FROM revisions WHERE omnisave_id = ?
		UNION SELECT current_revision_id FROM omnisaves WHERE id = ? AND current_revision_id IS NOT NULL
		UNION SELECT forked_from_revision_id FROM omnisaves WHERE id = ? AND forked_from_revision_id IS NOT NULL
		UNION SELECT revisions.parent_id FROM revisions JOIN members ON revisions.id = members.id
			WHERE revisions.parent_id IS NOT NULL
	) SELECT EXISTS(SELECT 1 FROM members WHERE id = ?)`, saveID, saveID, saveID, revisionID).Scan(&member)
	return member, err
}

func sameNullableString(value sql.NullString, expected *string) bool {
	if expected == nil {
		return !value.Valid
	}
	return value.Valid && value.String == *expected
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	copy := value.String
	return &copy
}

func forkOmnisaveID(origin *omnisave.ForkOrigin) any {
	if origin == nil {
		return nil
	}
	return origin.OmnisaveID
}

func forkRevisionID(origin *omnisave.ForkOrigin) any {
	if origin == nil {
		return nil
	}
	return origin.RevisionID
}

type scanner interface {
	Scan(dest ...any) error
}

func scanOmnisave(row scanner) (*omnisave.Omnisave, error) {
	var save omnisave.Omnisave
	var createdAt, currentRevisionCreatedAt, metadata string
	var current, forkSave, forkRevision sql.NullString
	if err := row.Scan(
		&save.ID, &save.GameID, &save.DisplayName, &current,
		&forkSave, &forkRevision, &createdAt, &currentRevisionCreatedAt, &metadata,
	); err != nil {
		return nil, err
	}
	save.CurrentRevisionID = nullableStringPointer(current)
	if forkSave.Valid && forkRevision.Valid {
		save.ForkedFrom = &omnisave.ForkOrigin{
			OmnisaveID: forkSave.String,
			RevisionID: forkRevision.String,
		}
	}
	var err error
	save.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, err
	}
	save.CurrentRevisionCreatedAt, err = time.Parse(time.RFC3339Nano, currentRevisionCreatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(metadata), &save.Metadata); err != nil {
		return nil, err
	}
	return &save, nil
}

func scanRevision(row scanner) (*omnisave.Revision, error) {
	var revision omnisave.Revision
	var createdAt, metadata string
	var parent sql.NullString
	if err := row.Scan(
		&revision.ID, &revision.OmnisaveID, &revision.DisplayName, &revision.NameSource,
		&parent, &createdAt, &metadata,
	); err != nil {
		return nil, err
	}
	revision.ParentID = nullableStringPointer(parent)
	var err error
	revision.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(metadata), &revision.Metadata); err != nil {
		return nil, err
	}
	return &revision, nil
}

func translateNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ErrNotFound
	}
	return err
}

// translateUniqueViolation turns the driver's constraint error on a duplicate
// identifier into the conflict the storage contract names, matching the memory
// repository.
func translateUniqueViolation(err error) error {
	var driverError *sqlitedriver.Error
	if errors.As(err, &driverError) {
		switch driverError.Code() {
		case sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY, sqlite3.SQLITE_CONSTRAINT_UNIQUE,
			sqlite3.SQLITE_CONSTRAINT_TRIGGER:
			return storage.ErrConflict
		}
	}
	return err
}

var _ storage.Repository = (*Repository)(nil)
