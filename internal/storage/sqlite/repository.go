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
	_ "modernc.org/sqlite"
)

type Repository struct {
	db          *sql.DB
	artifactDir string
}

// Open opens the database and applies pending schema migrations.
func Open(databasePath, artifactDir string) (*Repository, error) {
	if err := os.MkdirAll(filepath.Dir(databasePath), 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
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
	return &Repository{db: db, artifactDir: artifactDir}, nil
}

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
			id, game_id, display_name, head_revision_id,
			forked_from_omnisave_id, forked_from_revision_id, created_at, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		save.ID, save.GameID, save.DisplayName, save.HeadRevisionID,
		forkOmnisaveID(save.ForkedFrom), forkRevisionID(save.ForkedFrom),
		save.CreatedAt.Format(time.RFC3339Nano), string(metadata),
	)
	return err
}

func (r *Repository) ListOmnisaves(ctx context.Context) ([]omnisave.Omnisave, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, game_id, display_name, head_revision_id,
			forked_from_omnisave_id, forked_from_revision_id, created_at,
			COALESCE(
				(SELECT created_at FROM revisions WHERE id = omnisaves.head_revision_id),
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
		`SELECT id, game_id, display_name, head_revision_id,
			forked_from_omnisave_id, forked_from_revision_id, created_at,
			COALESCE(
				(SELECT created_at FROM revisions WHERE id = omnisaves.head_revision_id),
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
	return nil
}

func (r *Repository) DeleteOmnisave(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT DISTINCT artifact_sha256 FROM revision_files WHERE revision_id IN (
			SELECT id FROM revisions WHERE omnisave_id = ?
		)`, id,
	)
	if err != nil {
		return err
	}
	var hashes []string
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
	if err := rows.Err(); err != nil {
		return err
	}

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
	if err := tx.Commit(); err != nil {
		return err
	}

	for _, hash := range unreferenced {
		if err := r.removeArtifact(hash); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ForkOmnisave(ctx context.Context, save omnisave.Omnisave, initial omnisave.Revision) error {
	saveMetadata, err := json.Marshal(save.Metadata)
	if err != nil {
		return err
	}
	revisionMetadata, err := json.Marshal(initial.Metadata)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO omnisaves(
		id, game_id, display_name, head_revision_id,
		forked_from_omnisave_id, forked_from_revision_id, created_at, metadata
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, save.ID, save.GameID, save.DisplayName,
		save.HeadRevisionID, forkOmnisaveID(save.ForkedFrom), forkRevisionID(save.ForkedFrom),
		save.CreatedAt.Format(time.RFC3339Nano), string(saveMetadata)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO revisions(
		id, omnisave_id, parent_id, created_at, metadata
	) VALUES (?, ?, ?, ?, ?)`, initial.ID, initial.OmnisaveID, initial.ParentID,
		initial.CreatedAt.Format(time.RFC3339Nano), string(revisionMetadata)); err != nil {
		return err
	}
	if err := insertRevisionFiles(ctx, tx, initial); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) CommitRevision(ctx context.Context, expectedHeadID *string, revision omnisave.Revision) error {
	metadata, err := json.Marshal(revision.Metadata)
	if err != nil {
		return err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	actualHeadID, err := currentHead(ctx, tx, revision.OmnisaveID)
	if err != nil {
		return translateNotFound(err)
	}
	if !sameNullableString(actualHeadID, expectedHeadID) {
		return &storage.HeadConflict{ActualHeadID: nullableStringPointer(actualHeadID)}
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO revisions(id, omnisave_id, parent_id, created_at, metadata)
		VALUES (?, ?, ?, ?, ?)`, revision.ID, revision.OmnisaveID, revision.ParentID,
		revision.CreatedAt.Format(time.RFC3339Nano), string(metadata))
	if err != nil {
		return err
	}
	if err := insertRevisionFiles(ctx, tx, revision); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE omnisaves SET head_revision_id = ?
		WHERE id = ? AND ((head_revision_id IS NULL AND ? IS NULL) OR head_revision_id = ?)`,
		revision.ID, revision.OmnisaveID, expectedHeadID, expectedHeadID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		actual, lookupErr := currentHead(ctx, tx, revision.OmnisaveID)
		if lookupErr != nil {
			return translateNotFound(lookupErr)
		}
		return &storage.HeadConflict{ActualHeadID: nullableStringPointer(actual)}
	}
	return tx.Commit()
}

func (r *Repository) GetRevision(ctx context.Context, saveID, revisionID string) (*omnisave.Revision, error) {
	revision, err := scanRevision(r.db.QueryRowContext(ctx, `SELECT
		id, omnisave_id, parent_id, created_at, metadata
		FROM revisions WHERE omnisave_id = ? AND id = ?`, saveID, revisionID))
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
	rows, err := r.db.QueryContext(ctx, `SELECT
		id, omnisave_id, parent_id, created_at, metadata
		FROM revisions WHERE omnisave_id = ? ORDER BY created_at, id`, saveID)
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

func currentHead(ctx context.Context, tx *sql.Tx, saveID string) (sql.NullString, error) {
	var head sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT head_revision_id FROM omnisaves WHERE id = ?`, saveID).Scan(&head)
	return head, err
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
	var createdAt, updatedAt, metadata string
	var head, forkSave, forkRevision sql.NullString
	if err := row.Scan(
		&save.ID, &save.GameID, &save.DisplayName, &head,
		&forkSave, &forkRevision, &createdAt, &updatedAt, &metadata,
	); err != nil {
		return nil, err
	}
	save.HeadRevisionID = nullableStringPointer(head)
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
	save.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
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
		&revision.ID, &revision.OmnisaveID, &parent, &createdAt, &metadata,
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

var _ storage.Repository = (*Repository)(nil)
