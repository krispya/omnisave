// Package sqlite persists Omnisave metadata in SQLite and artifacts on disk.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
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
}

// Open opens the save store and the database over it, applies pending schema
// migrations, and repairs each from the other: rebuild imports what the store
// holds and the database lacks, then reconcile writes what the database holds
// and the store lacks. Together they mean whichever of the two survived is
// enough — a lost database grows back from the store, and a lost store grows
// back from the database.
//
// The two are not peers. The store is the durable record — one directory that
// recovers every save on its own — and the database is a fast index over it
// that also holds deployment secrets, which is why it lives outside the store
// rather than in it.
func Open(databasePath, storeDir string) (*Repository, error) {
	if err := os.MkdirAll(filepath.Dir(databasePath), 0755); err != nil {
		return nil, err
	}
	saveStore, err := store.Open(storeDir)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", databasePath)
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
	if err := repository.rebuild(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if err := repository.reconcile(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return repository, nil
}

// Store is the portable save store this repository writes through.
func (r *Repository) Store() *store.Store { return r.store }

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) InsertOmnisave(ctx context.Context, save omnisave.Omnisave) error {
	metadata, err := json.Marshal(save.Metadata)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx,
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
	r.noteStoreLag("save "+save.ID, r.recordOmnisave(ctx, save.ID))
	return nil
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
	result, err := r.db.ExecContext(ctx,
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
	r.noteStoreLag("save "+id, r.recordOmnisave(ctx, id))
	return nil
}

func (r *Repository) DeleteOmnisave(ctx context.Context, id string) error {
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

	var unreferenced []string
	for _, hash := range hashes {
		var used bool
		if err := tx.QueryRowContext(ctx,
			`SELECT
				EXISTS(SELECT 1 FROM revision_files WHERE artifact_sha256 = ?) OR
				EXISTS(SELECT 1 FROM game_media WHERE sha256 = ?)`, hash, hash,
		).Scan(&used); err != nil {
			return err
		}
		if !used {
			unreferenced = append(unreferenced, hash)
		}
	}
	if err := r.tombstoneOmnisave(id, time.Now().UTC()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	r.noteStoreLag("deletion of save "+id, r.dropRevisions(revisionIDs))
	for _, hash := range unreferenced {
		if err := r.removeArtifact(hash); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ForkOmnisave(ctx context.Context, save omnisave.Omnisave) error {
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
	if err := tx.Commit(); err != nil {
		return err
	}
	r.noteStoreLag("save "+save.ID, r.recordOmnisave(ctx, save.ID))
	return nil
}

func (r *Repository) RestoreOmnisave(ctx context.Context, id, revisionID string, expectedCurrentRevisionID *string) error {
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
	if err := tx.Commit(); err != nil {
		return err
	}
	r.noteStoreLag("current revision of save "+id, r.recordOmnisave(ctx, id))
	return nil
}

func (r *Repository) CommitRevision(ctx context.Context, expectedCurrentRevisionID *string, revision omnisave.Revision) error {
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

	_, err = tx.ExecContext(ctx, `INSERT INTO revisions(
		id, game_id, omnisave_id, parent_id, created_at, metadata
	) SELECT ?, game_id, ?, ?, ?, ? FROM omnisaves WHERE id = ?`,
		revision.ID, revision.OmnisaveID, revision.ParentID,
		revision.CreatedAt.Format(time.RFC3339Nano), string(metadata), revision.OmnisaveID)
	if err != nil {
		return err
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
	if err := tx.Commit(); err != nil {
		return err
	}
	r.noteStoreLag("revision "+revision.ID, r.recordRevision(ctx, revision.ID))
	r.noteStoreLag("current revision of save "+revision.OmnisaveID, r.recordOmnisave(ctx, revision.OmnisaveID))
	return nil
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
		id, omnisave_id, display_name, parent_id, created_at, metadata
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
		id, omnisave_id, display_name, parent_id, created_at, metadata
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

func (r *Repository) UpdateRevisionDisplayName(ctx context.Context, saveID, revisionID, displayName string) error {
	if _, err := r.GetRevision(ctx, saveID, revisionID); err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE revisions SET display_name = ? WHERE id = ?`,
		displayName, revisionID)
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
	memberSaveIDs, err := r.memberOmnisaveIDs(ctx, revisionID)
	if err != nil {
		return err
	}
	for _, memberSaveID := range memberSaveIDs {
		r.noteStoreLag("revision name "+revisionID, r.recordOmnisave(ctx, memberSaveID))
	}
	return nil
}

func (r *Repository) memberOmnisaveIDs(ctx context.Context, revisionID string) ([]string, error) {
	saves, err := r.ListOmnisaves(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, save := range saves {
		if _, err := r.GetRevision(ctx, save.ID, revisionID); err == nil {
			ids = append(ids, save.ID)
		} else if !errors.Is(err, storage.ErrNotFound) {
			return nil, err
		}
	}
	return ids, nil
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
	rows, err := r.db.QueryContext(ctx, `SELECT path, artifact_format, artifact_sha256, artifact_size
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
		&revision.ID, &revision.OmnisaveID, &revision.DisplayName, &parent, &createdAt, &metadata,
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
		case sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY, sqlite3.SQLITE_CONSTRAINT_UNIQUE:
			return storage.ErrConflict
		}
	}
	return err
}

var _ storage.Repository = (*Repository)(nil)
