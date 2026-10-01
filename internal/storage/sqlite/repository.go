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

	sqlitedriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/krisbaumgartner/omnisave/internal/access"
	"github.com/krisbaumgartner/omnisave/internal/artifact"
	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/device"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/settings"
	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

type Repository struct {
	db    *sql.DB
	store *store.Store
	// storeErr stops later portable mutations after an outbox projection
	// failure. Open replays the queue before exposing the repository again.
	storeErr error
	// mutate serializes in-process mutations and their portable-store side effects.
	mutate sync.Mutex
	// cleanup tracks deferred deletion work so Close can drain it before the
	// database goes away.
	cleanup sync.WaitGroup
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
	r.cleanup.Wait()
	return r.db.Close()
}

// WaitForCleanup blocks until deferred deletion cleanup has settled. Reads
// never need it — deleted rows are gone at commit — but Close drains it, and
// tests use it before inspecting the store or reclaimed artifacts.
func (r *Repository) WaitForCleanup() {
	r.cleanup.Wait()
}

// deferDeletionCleanup finishes a committed deletion's physical half — the
// portable-store projection and the cleanup behind it — off the caller's
// request, so a delete answers as soon as its rows are gone. The work runs
// under the mutation lock like the inline path did, and a crash before it
// runs is repaired the same way as one during it: the next open replays the
// outbox and sweeps.
func (r *Repository) deferDeletionCleanup(what string, cleanup func(ctx context.Context)) {
	r.cleanup.Add(1)
	go func() {
		defer r.cleanup.Done()
		r.mutate.Lock()
		defer r.mutate.Unlock()
		ctx := context.Background()
		if err := r.projectStore(ctx); err != nil {
			log.Printf("save store: %s committed, but projecting it failed: %v; durable mutations stop until the next open", what, err)
			return
		}
		cleanup(ctx)
	}()
}

func (r *Repository) InsertOmnisave(ctx context.Context, save omnisave.Omnisave) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return err
	}
	if r.store.HasDeletion(store.DeletionOmnisave, save.ID) {
		return errIdentifierTaken
	}
	if save.PathFormatVersion == 0 {
		save.PathFormatVersion = omnisave.PathFormatNative
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
	// A save belongs to a Library game. The check runs inside the insert's
	// write transaction, which holds SQLite's writer order (sqliteDSN), so a
	// game deletion cannot land between the check and the insert.
	var gameExists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM games WHERE id = ?)`, save.GameID).Scan(&gameExists); err != nil {
		return err
	}
	if !gameExists {
		return omnisave.ErrInvalid
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO omnisaves(
			id, game_id, display_name, path_format_version, current_revision_id,
			forked_from_omnisave_id, forked_from_revision_id, created_at, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		save.ID, save.GameID, save.DisplayName, save.PathFormatVersion, save.CurrentRevisionID,
		forkOmnisaveID(save.ForkedFrom), forkRevisionID(save.ForkedFrom),
		save.CreatedAt.Format(time.RFC3339Nano), string(metadata),
	)
	if err != nil {
		return translateUniqueViolation(err, errIdentifierTaken)
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
		`SELECT id, game_id, display_name, path_format_version, current_revision_id,
			forked_from_omnisave_id, forked_from_revision_id, created_at,
			COALESCE(
				(SELECT created_at FROM revisions WHERE id = omnisaves.current_revision_id),
				created_at
			),
			COALESCE(
				(SELECT created_at FROM revisions
					WHERE omnisave_id = omnisaves.id
					ORDER BY created_at DESC, id DESC LIMIT 1),
				created_at
			),
			(SELECT saved_at FROM revisions WHERE id = omnisaves.current_revision_id),
			metadata
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
		`SELECT id, game_id, display_name, path_format_version, current_revision_id,
			forked_from_omnisave_id, forked_from_revision_id, created_at,
			COALESCE(
				(SELECT created_at FROM revisions WHERE id = omnisaves.current_revision_id),
				created_at
			),
			COALESCE(
				(SELECT created_at FROM revisions
					WHERE omnisave_id = omnisaves.id
					ORDER BY created_at DESC, id DESC LIMIT 1),
				created_at
			),
			(SELECT saved_at FROM revisions WHERE id = omnisaves.current_revision_id),
			metadata
		FROM omnisaves WHERE id = ?`, id,
	))
	return save, translateNotFound(err, omnisave.ErrNotFound)
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
		return omnisave.ErrNotFound
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
		// The ledger row commits with the delete; the portable marker may still
		// be in deferred cleanup, so it cannot be the only witness.
		committed, err := deletionCommitted(ctx, tx, store.DeletionOmnisave, id)
		if err != nil {
			return err
		}
		if committed || r.store.HasDeletion(store.DeletionOmnisave, id) {
			return nil
		}
		return omnisave.ErrNotFound
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
	r.deferDeletionCleanup("deletion of save "+id, func(ctx context.Context) {
		r.noteStoreLag("deletion of save "+id, r.dropRevisions(revisionIDs))
		r.reclaimArtifacts(ctx, hashes)
	})
	return nil
}

func (r *Repository) ForkOmnisave(ctx context.Context, save omnisave.Omnisave) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return err
	}
	if r.store.HasDeletion(store.DeletionOmnisave, save.ID) {
		return errIdentifierTaken
	}
	saveMetadata, err := json.Marshal(save.Metadata)
	if err != nil {
		return err
	}
	if save.ForkedFrom == nil || save.CurrentRevisionID == nil ||
		*save.CurrentRevisionID != save.ForkedFrom.RevisionID {
		return omnisave.ErrNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var sourceGameID string
	var sourcePathFormat int
	if err := tx.QueryRowContext(ctx, `SELECT game_id, path_format_version FROM omnisaves WHERE id = ?`,
		save.ForkedFrom.OmnisaveID).Scan(&sourceGameID, &sourcePathFormat); err != nil {
		return translateNotFound(err, omnisave.ErrNotFound)
	}
	if sourcePathFormat != omnisave.PathFormatNative {
		return omnisave.ErrPathFormatMigrationRequired
	}
	member, err := revisionIsMember(ctx, tx, save.ForkedFrom.OmnisaveID, save.ForkedFrom.RevisionID)
	if err != nil {
		return err
	}
	if !member || sourceGameID != save.GameID {
		return omnisave.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO omnisaves(
		id, game_id, display_name, path_format_version, current_revision_id,
		forked_from_omnisave_id, forked_from_revision_id, created_at, metadata
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, save.ID, save.GameID, save.DisplayName,
		sourcePathFormat, save.CurrentRevisionID, forkOmnisaveID(save.ForkedFrom), forkRevisionID(save.ForkedFrom),
		save.CreatedAt.Format(time.RFC3339Nano), string(saveMetadata)); err != nil {
		return translateUniqueViolation(err, errIdentifierTaken)
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
		return translateNotFound(err, omnisave.ErrNotFound)
	}
	if !sameNullableString(actual, expectedCurrentRevisionID) {
		return revisionConflict(expectedCurrentRevisionID, actual)
	}
	if err := requireNativePathFormat(ctx, tx, id); err != nil {
		return err
	}
	member, err := revisionIsMember(ctx, tx, id, revisionID)
	if err != nil {
		return err
	}
	if !member {
		return omnisave.ErrNotFound
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
			return translateNotFound(lookupErr, omnisave.ErrNotFound)
		}
		return revisionConflict(expectedCurrentRevisionID, latest)
	}
	if err := r.enqueueOmnisaveProjection(ctx, tx, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return r.projectStore(ctx)
}

func (r *Repository) CommitRevision(ctx context.Context, expectedCurrentRevisionID *string, revision omnisave.Revision, keepCurrent bool) error {
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
		return translateNotFound(err, omnisave.ErrNotFound)
	}
	if !sameNullableString(actualCurrentRevisionID, expectedCurrentRevisionID) {
		return revisionConflict(expectedCurrentRevisionID, actualCurrentRevisionID)
	}
	if err := requireNativePathFormat(ctx, tx, revision.OmnisaveID); err != nil {
		return err
	}
	descriptors := make(map[string]int64, len(revision.Files))
	for _, file := range revision.Files {
		descriptors[file.Artifact.SHA256] = file.Artifact.Size
	}
	if err := r.requireAvailableArtifacts(ctx, tx, descriptors); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO revisions(
		id, game_id, omnisave_id, display_name, name_source, parent_id, created_at, saved_at, metadata
	) SELECT ?, game_id, ?, ?, ?, ?, ?, ?, ? FROM omnisaves WHERE id = ?`,
		revision.ID, revision.OmnisaveID, revision.DisplayName, revision.NameSource, revision.ParentID,
		revision.CreatedAt.Format(time.RFC3339Nano), formatNullableTime(revision.SavedAt),
		string(metadata), revision.OmnisaveID)
	if err != nil {
		return translateUniqueViolation(err, errIdentifierTaken)
	}
	if err := insertRevisionFiles(ctx, tx, revision); err != nil {
		return err
	}
	// A keep-current commit leaves the pointer where the check above proved
	// it; the guarded UPDATE otherwise doubles as a second conflict check.
	if !keepCurrent {
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
				return translateNotFound(lookupErr, omnisave.ErrNotFound)
			}
			return revisionConflict(expectedCurrentRevisionID, actual)
		}
	}
	// Marks waiting on this save now have the snapshot they were waiting for.
	if err := claimPendingAchievements(ctx, tx, revision); err != nil {
		return err
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
		id, omnisave_id, display_name, name_source, parent_id, created_at, saved_at, metadata
		FROM revisions WHERE id = ? AND id IN (SELECT id FROM members)`, saveID, saveID, saveID, revisionID))
	if err != nil {
		return nil, translateNotFound(err, omnisave.ErrNotFound)
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
		id, omnisave_id, display_name, name_source, parent_id, created_at, saved_at, metadata
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

// UpdateRevisionDisplayName renames a revision on behalf of a person and stamps
// the name manual so its provenance remains clear until an explicit relabel.
func (r *Repository) UpdateRevisionDisplayName(ctx context.Context, saveID, revisionID, displayName string) error {
	return r.updateRevisionDisplayName(ctx, saveID, revisionID, displayName, omnisave.NameSourceManual)
}

// UpdateRevisionLabel stores an explicitly requested label and replaces the
// name's provenance with the labeler's.
func (r *Repository) UpdateRevisionLabel(ctx context.Context, saveID, revisionID, displayName string) error {
	return r.updateRevisionDisplayName(ctx, saveID, revisionID, displayName, omnisave.NameSourceLabeler)
}

func (r *Repository) updateRevisionDisplayName(
	ctx context.Context,
	saveID, revisionID, displayName, nameSource string,
) error {
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
	result, err := tx.ExecContext(ctx,
		`UPDATE revisions SET display_name = ?, name_source = ? WHERE id = ?`,
		displayName, nameSource, revisionID,
	)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return omnisave.ErrNotFound
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

// MigrateRevisionPaths renames a lineage's location vocabulary in place:
// `from/rest` becomes `to/rest` on every file of every revision the save
// itself owns, where `from` is the vocabulary the retired fromVersion
// speaks (the omnisave.PathFormatMigrations chain owns that mapping and the
// version the rename advances to). Identities,
// artifacts, ancestry, and achievements are untouched — only path labels
// change — which is what keeps the operation reversible and every reference
// into the lineage valid. The rename is also recorded on the row as a
// migration fact, because snapshot manifests in the portable store are
// immutable: recovery replays the fact over imported manifests so a rebuilt
// database reaches this vocabulary again.
//
// Refused whenever the rewrite could not be a whole lineage's rename: a
// save that shares revisions with a fork in either direction would leave a
// mixed-vocabulary history on one side, a lineage with files outside `from`
// is either already migrated (empty) or was never single-voiced (mixed),
// and a rename minting a path longer than any commit may reference would
// break the bound every other write upholds. The caller owns the evidence
// that `to` is the right spelling; these guards only ensure the rename is
// total or absent.
func (r *Repository) MigrateRevisionPaths(ctx context.Context, saveID string, fromVersion int, to string) (omnisave.MigrationResult, error) {
	none := omnisave.MigrationResult{}
	migration, migratable := omnisave.MigrationFrom(fromVersion)
	if !migratable || migration.Kind != omnisave.MigrationKindLocationRename {
		return none, &omnisave.MigrationRefused{Reason: omnisave.MigrationRefusedUnknownVersion}
	}
	from := migration.Location
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return none, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return none, err
	}
	defer tx.Rollback()

	var pathFormatVersion int
	var recorded string
	if err := tx.QueryRowContext(ctx,
		`SELECT path_format_version, path_migrations FROM omnisaves WHERE id = ?`, saveID,
	).Scan(&pathFormatVersion, &recorded); err != nil {
		return none, translateNotFound(err, omnisave.ErrNotFound)
	}
	if pathFormatVersion != fromVersion {
		return none, &omnisave.MigrationRefused{Reason: omnisave.MigrationRefusedVersion}
	}
	var forkFamily bool
	if err := tx.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM omnisaves WHERE id = ? AND forked_from_revision_id IS NOT NULL)
		OR EXISTS(SELECT 1 FROM omnisaves WHERE id != ? AND forked_from_revision_id IN
			(SELECT id FROM revisions WHERE omnisave_id = ?))
		OR EXISTS(SELECT 1 FROM revisions child JOIN revisions parent ON child.parent_id = parent.id
			WHERE parent.omnisave_id = ? AND child.omnisave_id != ?)`,
		saveID, saveID, saveID, saveID, saveID,
	).Scan(&forkFamily); err != nil {
		return none, err
	}
	if forkFamily {
		return none, &omnisave.MigrationRefused{Reason: omnisave.MigrationRefusedForkFamily}
	}
	prefix := from + "/"
	var speaking, total, overlong int
	// The length bound counts bytes, because that is what a commit's own
	// validator counts; length() on text would count characters and let a
	// rename mint a path the next commit would refuse. The prefix
	// comparison stays on text: a retired location is ASCII, so its
	// character and byte offsets coincide, and the offsets are what the
	// rename below reuses.
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FILTER (WHERE substr(path, 1, ?) = ?), COUNT(*),
			COUNT(*) FILTER (WHERE substr(path, 1, ?) = ?
				AND length(CAST(path AS BLOB)) - ? + ? > ?)
		FROM revision_files WHERE revision_id IN (SELECT id FROM revisions WHERE omnisave_id = ?)`,
		len(prefix), prefix, len(prefix), prefix, len(prefix), len(to)+1,
		omnisave.MaxRevisionPathLength, saveID,
	).Scan(&speaking, &total, &overlong); err != nil {
		return none, err
	}
	if speaking == 0 {
		return none, &omnisave.MigrationRefused{Reason: omnisave.MigrationRefusedEmpty}
	}
	if speaking != total {
		return none, &omnisave.MigrationRefused{Reason: omnisave.MigrationRefusedMixed}
	}
	if overlong > 0 {
		return none, &omnisave.MigrationRefused{Reason: omnisave.MigrationRefusedPathLength}
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE revision_files SET path = ? || substr(path, ?)
		WHERE revision_id IN (SELECT id FROM revisions WHERE omnisave_id = ?)`,
		to+"/", len(prefix)+1, saveID,
	)
	if err != nil {
		return none, err
	}
	files, err := result.RowsAffected()
	if err != nil {
		return none, err
	}
	migrations, err := appendPathMigration(recorded, omnisave.PathMigration{
		Kind:        migration.Kind,
		FromVersion: fromVersion,
		ToVersion:   migration.ToVersion,
		From:        from,
		To:          to,
		MigratedAt:  time.Now().UTC(),
	})
	if err != nil {
		return none, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE omnisaves SET path_format_version = ?, path_migrations = ? WHERE id = ?`,
		migration.ToVersion, migrations, saveID); err != nil {
		return none, err
	}
	var revisions int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM revisions WHERE omnisave_id = ?`, saveID,
	).Scan(&revisions); err != nil {
		return none, err
	}
	if err := r.enqueueOmnisaveProjection(ctx, tx, saveID); err != nil {
		return none, err
	}
	if err := tx.Commit(); err != nil {
		return none, err
	}
	return omnisave.MigrationResult{
		PathFormatVersion: migration.ToVersion,
		Revisions:         revisions,
		Files:             int(files),
	}, r.projectStore(ctx)
}

// appendPathMigration adds one applied rename to a row's recorded, encoded
// migration facts.
func appendPathMigration(recorded string, applied omnisave.PathMigration) (string, error) {
	migrations, err := decodePathMigrations(recorded)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(append(migrations, applied))
	return string(encoded), err
}

// decodePathMigrations reads a row's encoded migration facts; an empty list
// decodes to nil so record projections omit the field entirely.
func decodePathMigrations(recorded string) ([]omnisave.PathMigration, error) {
	if recorded == "" || recorded == "[]" {
		return nil, nil
	}
	var migrations []omnisave.PathMigration
	if err := json.Unmarshal([]byte(recorded), &migrations); err != nil {
		return nil, err
	}
	return migrations, nil
}

// RecordAchievements files unlocks against a save. An achievement already
// recorded keeps the placement it was first given, so a Device repeating a
// report — after a crash, or from a second machine — never moves a mark.
func (r *Repository) RecordAchievements(ctx context.Context, saveID string, achievements []omnisave.Achievement) ([]omnisave.Achievement, error) {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var saveExists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM omnisaves WHERE id = ?)`, saveID,
	).Scan(&saveExists); err != nil {
		return nil, err
	}
	if !saveExists {
		return nil, omnisave.ErrNotFound
	}

	recorded := make([]omnisave.Achievement, 0, len(achievements))
	for _, achievement := range achievements {
		result, err := tx.ExecContext(ctx, `INSERT INTO achievements(
			omnisave_id, achievement_id, name, description, unlocked_at, revision_id
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(omnisave_id, achievement_id) DO NOTHING`,
			saveID, achievement.ID, achievement.Name, achievement.Description,
			achievement.UnlockedAt.Unix(), achievement.RevisionID)
		if err != nil {
			return nil, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if count > 0 {
			recorded = append(recorded, achievement)
		}
	}
	if len(recorded) == 0 {
		return nil, tx.Commit()
	}
	if err := r.enqueueOmnisaveProjection(ctx, tx, saveID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return recorded, r.projectStore(ctx)
}

func (r *Repository) ListAchievements(ctx context.Context, saveID string) ([]omnisave.Achievement, error) {
	if _, err := r.GetOmnisave(ctx, saveID); err != nil {
		return nil, err
	}
	return listAchievementsFrom(ctx, r.db, saveID)
}

func listAchievementsFrom(ctx context.Context, queryer storeQueryer, saveID string) ([]omnisave.Achievement, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT achievement_id, name, description, unlocked_at, revision_id
		FROM achievements WHERE omnisave_id = ? ORDER BY unlocked_at, achievement_id`, saveID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	achievements := make([]omnisave.Achievement, 0)
	for rows.Next() {
		var (
			achievement omnisave.Achievement
			unlockedAt  int64
			revisionID  sql.NullString
		)
		if err := rows.Scan(&achievement.ID, &achievement.Name, &achievement.Description,
			&unlockedAt, &revisionID); err != nil {
			return nil, err
		}
		achievement.UnlockedAt = time.Unix(unlockedAt, 0).UTC()
		if revisionID.Valid {
			achievement.RevisionID = &revisionID.String
		}
		achievements = append(achievements, achievement)
	}
	return achievements, rows.Err()
}

// claimPendingAchievements gives a freshly committed revision every mark on
// this save that is still waiting for one — an unlock reported before the
// save was written, or one whose revision was later deleted.
func claimPendingAchievements(ctx context.Context, tx *sql.Tx, revision omnisave.Revision) error {
	_, err := tx.ExecContext(ctx, `UPDATE achievements SET revision_id = ?
		WHERE omnisave_id = ? AND revision_id IS NULL AND unlocked_at <= ?`,
		revision.ID, revision.OmnisaveID, revision.CreatedAt.Unix())
	return err
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
		return omnisave.ErrNotFound
	}
	// Idempotent only through a live save: the marker check sits behind the
	// existence check so a deleted save's URLs answer not-found, not success.
	// The ledger commits with the delete; the portable marker may lag in
	// deferred cleanup.
	committed, err := deletionCommitted(ctx, tx, store.DeletionRevision, revisionID)
	if err != nil {
		return err
	}
	if committed || r.store.HasDeletion(store.DeletionRevision, revisionID) {
		return nil
	}
	member, err := revisionIsMember(ctx, tx, saveID, revisionID)
	if err != nil {
		return err
	}
	if !member {
		return omnisave.ErrNotFound
	}
	var ownerID string
	if err := tx.QueryRowContext(ctx,
		`SELECT omnisave_id FROM revisions WHERE id = ?`, revisionID,
	).Scan(&ownerID); err != nil {
		return translateNotFound(err, omnisave.ErrNotFound)
	}

	refusals := []struct {
		query  string
		reason string
	}{
		{`SELECT EXISTS(SELECT 1 FROM omnisaves WHERE current_revision_id = ?)`, omnisave.RevisionInUseCurrent},
		{`SELECT EXISTS(SELECT 1 FROM revisions WHERE parent_id = ?)`, omnisave.RevisionInUseChildren},
		{`SELECT EXISTS(SELECT 1 FROM omnisaves WHERE forked_from_revision_id = ?)`, omnisave.RevisionInUseForkOrigin},
	}
	for _, refusal := range refusals {
		var used bool
		if err := tx.QueryRowContext(ctx, refusal.query, revisionID).Scan(&used); err != nil {
			return err
		}
		if used {
			return &omnisave.RevisionInUse{Reason: refusal.reason}
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
	r.deferDeletionCleanup("deletion of revision "+revisionID, func(ctx context.Context) {
		r.noteStoreLag("deletion of revision "+revisionID, r.dropRevisions([]string{revisionID}))
		r.reclaimArtifacts(ctx, hashes)
	})
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

func (r *Repository) StoreArtifact(_ context.Context, descriptor artifact.Artifact, payload io.Reader) error {
	return r.storeArtifact(descriptor, payload)
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

// requireNativePathFormat is the mutation boundary that keeps a legacy
// lineage from accumulating another vocabulary before its explicit upgrade.
func requireNativePathFormat(ctx context.Context, tx *sql.Tx, saveID string) error {
	var version int
	if err := tx.QueryRowContext(ctx,
		`SELECT path_format_version FROM omnisaves WHERE id = ?`, saveID,
	).Scan(&version); err != nil {
		return translateNotFound(err, omnisave.ErrNotFound)
	}
	if version != omnisave.PathFormatNative {
		return omnisave.ErrPathFormatMigrationRequired
	}
	return nil
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

func formatNullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.Format(time.RFC3339Nano)
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
	var createdAt, currentRevisionCreatedAt, latestRevisionCreatedAt, metadata string
	var current, forkSave, forkRevision, currentRevisionSavedAt sql.NullString
	if err := row.Scan(
		&save.ID, &save.GameID, &save.DisplayName, &save.PathFormatVersion, &current,
		&forkSave, &forkRevision, &createdAt, &currentRevisionCreatedAt,
		&latestRevisionCreatedAt, &currentRevisionSavedAt, &metadata,
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
	save.LatestRevisionCreatedAt, err = time.Parse(time.RFC3339Nano, latestRevisionCreatedAt)
	if err != nil {
		return nil, err
	}
	save.CurrentRevisionSavedAt, err = parseNullableTime(currentRevisionSavedAt)
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
	var parent, savedAt sql.NullString
	if err := row.Scan(
		&revision.ID, &revision.OmnisaveID, &revision.DisplayName, &revision.NameSource,
		&parent, &createdAt, &savedAt, &metadata,
	); err != nil {
		return nil, err
	}
	revision.ParentID = nullableStringPointer(parent)
	var err error
	revision.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, err
	}
	revision.SavedAt, err = parseNullableTime(savedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(metadata), &revision.Metadata); err != nil {
		return nil, err
	}
	return &revision, nil
}

// translateNotFound reports a missing row as the calling domain's notFound.
func translateNotFound(err, notFound error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return notFound
	}
	return err
}

// errIdentifierTaken reports a write that reused an identifier already in use
// or already deleted. Services mint fresh identifiers, so it signals a bug
// rather than anything a caller can act on.
var errIdentifierTaken = errors.New("sqlite: identifier already used")

// translateUniqueViolation turns the driver's constraint error on a duplicate
// identifier into the calling domain's conflict.
func translateUniqueViolation(err, conflict error) error {
	var driverError *sqlitedriver.Error
	if errors.As(err, &driverError) {
		switch driverError.Code() {
		case sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY, sqlite3.SQLITE_CONSTRAINT_UNIQUE,
			sqlite3.SQLITE_CONSTRAINT_TRIGGER:
			return conflict
		}
	}
	return err
}

// revisionConflict reports a stale write with the Current Revision it expected
// and the one it found.
func revisionConflict(expected *string, actual sql.NullString) error {
	conflict := &omnisave.CurrentRevisionConflict{ActualCurrentRevisionID: nullableStringPointer(actual)}
	if expected != nil {
		value := *expected
		conflict.ExpectedCurrentRevisionID = &value
	}
	return conflict
}

// Repository is the one persistence adapter behind every domain's contract.
var (
	_ omnisave.Repository = (*Repository)(nil)
	_ catalog.Repository  = (*Repository)(nil)
	_ device.Repository   = (*Repository)(nil)
	_ access.Repository   = (*Repository)(nil)
	_ settings.Repository = (*Repository)(nil)
)
