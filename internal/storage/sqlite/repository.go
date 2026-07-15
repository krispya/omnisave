// Package sqlite persists OmniSave metadata in SQLite and artifacts on disk.
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

func (r *Repository) InsertOmniSave(ctx context.Context, save omnisave.OmniSave) error {
	metadata, err := json.Marshal(save.Metadata)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO omnisaves(id, game_id, slot, created_at, metadata) VALUES (?, ?, ?, ?, ?)`,
		save.ID, save.GameID, save.Slot, save.CreatedAt.Format(time.RFC3339Nano), string(metadata),
	)
	return err
}

func (r *Repository) ListOmniSaves(ctx context.Context) ([]omnisave.OmniSave, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, game_id, slot, created_at, metadata FROM omnisaves ORDER BY created_at, id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var saves []omnisave.OmniSave
	for rows.Next() {
		save, err := scanOmniSave(rows)
		if err != nil {
			return nil, err
		}
		saves = append(saves, *save)
	}
	return saves, rows.Err()
}

func (r *Repository) GetOmniSave(ctx context.Context, id string) (*omnisave.OmniSave, error) {
	save, err := scanOmniSave(r.db.QueryRowContext(ctx,
		`SELECT id, game_id, slot, created_at, metadata FROM omnisaves WHERE id = ?`, id,
	))
	return save, translateNotFound(err)
}

func (r *Repository) DeleteOmniSave(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT DISTINCT artifact_sha256 FROM revisions WHERE omnisave_id = ?`, id,
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

	if _, err := tx.ExecContext(ctx, `DELETE FROM revision_parents WHERE revision_id IN (
		SELECT id FROM revisions WHERE omnisave_id = ?
	)`, id); err != nil {
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
			`SELECT EXISTS(SELECT 1 FROM revisions WHERE artifact_sha256 = ?)`, hash,
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
		if err := os.Remove(r.artifactPath(hash)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (r *Repository) InsertRevision(ctx context.Context, revision omnisave.Revision, payload io.Reader) error {
	if err := r.storeArtifact(revision.Artifact, payload); err != nil {
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
	_, err = tx.ExecContext(ctx, `INSERT INTO revisions(
		id, omnisave_id, created_at, artifact_format, artifact_sha256, artifact_size, metadata
	) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		revision.ID, revision.OmniSaveID, revision.CreatedAt.Format(time.RFC3339Nano),
		revision.Artifact.Format, revision.Artifact.SHA256, revision.Artifact.Size, string(metadata),
	)
	if err != nil {
		return err
	}
	for position, parentID := range revision.ParentIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO revision_parents(revision_id, parent_id, position) VALUES (?, ?, ?)`,
			revision.ID, parentID, position,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) GetRevision(ctx context.Context, saveID, revisionID string) (*omnisave.Revision, error) {
	revision, err := scanRevision(r.db.QueryRowContext(ctx, `SELECT
		id, omnisave_id, created_at, artifact_format, artifact_sha256, artifact_size, metadata
		FROM revisions WHERE omnisave_id = ? AND id = ?`, saveID, revisionID))
	if err != nil {
		return nil, translateNotFound(err)
	}
	revision.ParentIDs, err = r.listParents(ctx, revision.ID)
	return revision, err
}

func (r *Repository) ListRevisions(ctx context.Context, saveID string) ([]omnisave.Revision, error) {
	if _, err := r.GetOmniSave(ctx, saveID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT
		id, omnisave_id, created_at, artifact_format, artifact_sha256, artifact_size, metadata
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
		parents, err := r.listParents(ctx, revisions[index].ID)
		if err != nil {
			return nil, err
		}
		revisions[index].ParentIDs = parents
	}
	return revisions, nil
}

func (r *Repository) DeleteRevision(ctx context.Context, saveID, revisionID string) error {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM revisions WHERE omnisave_id = ? AND id = ?`, saveID, revisionID,
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

func (r *Repository) OpenArtifact(_ context.Context, hash string) (io.ReadCloser, error) {
	return r.openArtifact(hash)
}

func (r *Repository) listParents(ctx context.Context, revisionID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT parent_id FROM revision_parents WHERE revision_id = ? ORDER BY position`, revisionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	parents := make([]string, 0)
	for rows.Next() {
		var parent string
		if err := rows.Scan(&parent); err != nil {
			return nil, err
		}
		parents = append(parents, parent)
	}
	return parents, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanOmniSave(row scanner) (*omnisave.OmniSave, error) {
	var save omnisave.OmniSave
	var createdAt, metadata string
	if err := row.Scan(&save.ID, &save.GameID, &save.Slot, &createdAt, &metadata); err != nil {
		return nil, err
	}
	var err error
	save.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
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
	if err := row.Scan(
		&revision.ID, &revision.OmniSaveID, &createdAt,
		&revision.Artifact.Format, &revision.Artifact.SHA256, &revision.Artifact.Size, &metadata,
	); err != nil {
		return nil, err
	}
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
