package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/storage"
	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

// This file keeps the portable store in step with the database.
//
// The database is the live authority — it enforces the head checks that make a
// commit atomic — and the store is the durable record that outlives it. Records
// are therefore written to the store immediately after the transaction that
// created them commits, never before: a manifest written first would describe a
// commit that a head conflict then rejected, and an invented revision is a
// worse failure than a missing one.
//
// That ordering leaves one gap, between the commit and the write. Reconcile
// closes it on the next start by writing whatever the database has and the
// store lacks, which is the same pass that imports a store-less deployment.
// The same reasoning sets what a failed store write costs at runtime: the
// operation it trails has already durably happened, so the failure is logged
// as a debt for reconcile rather than reported as if the operation had not.

// noteStoreLag logs a store write that failed after the database committed.
// Failing the request would report an operation that durably happened as one
// that did not — a client would retry a commit it already holds and be told
// its own revision is a conflict. The store is behind until the next open
// reconciles it, and that window is the one thing worth saying out loud: a
// copy of the store taken during it lacks what could not be written.
func (r *Repository) noteStoreLag(what string, err error) {
	if err != nil {
		log.Printf("save store: could not record %s; reconciling on the next open repairs it: %v", what, err)
	}
}

// recordRevision writes a revision's manifest, denormalizing the lineage and
// game identity a recovery needs.
func (r *Repository) recordRevision(ctx context.Context, revisionID string) error {
	manifest, err := r.buildRevision(ctx, revisionID)
	if err != nil {
		return err
	}
	return r.store.PutRevision(manifest)
}

func (r *Repository) buildRevision(ctx context.Context, revisionID string) (store.Revision, error) {
	var (
		manifest  store.Revision
		parent    sql.NullString
		createdAt string
		metadata  string
		gameID    string
		gameTitle string
		platform  sql.NullString
	)
	err := r.db.QueryRowContext(ctx, `SELECT
			revisions.id, revisions.parent_id, revisions.created_at, revisions.metadata,
			omnisaves.id, omnisaves.display_name, omnisaves.game_id,
			COALESCE(games.title, ''), games.platform
		FROM revisions
		JOIN omnisaves ON omnisaves.id = revisions.omnisave_id
		LEFT JOIN games ON games.id = omnisaves.game_id
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
	if manifest.Game.Identifiers, err = r.listGameIdentifiers(ctx, gameID); err != nil {
		return store.Revision{}, err
	}

	files, err := r.listRevisionFiles(ctx, revisionID)
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

// recordOmnisave writes a lineage's record — the name, the fork origin, and the
// facts that change without a commit.
func (r *Repository) recordOmnisave(ctx context.Context, id string) error {
	record, err := r.buildOmnisave(ctx, id)
	if err != nil {
		return err
	}
	return r.store.PutOmnisave(record)
}

func (r *Repository) buildOmnisave(ctx context.Context, id string) (store.Omnisave, error) {
	var (
		record     store.Omnisave
		forkedSave sql.NullString
		forkedRev  sql.NullString
		createdAt  string
		metadata   string
	)
	err := r.db.QueryRowContext(ctx, `SELECT id, game_id, display_name,
			forked_from_omnisave_id, forked_from_revision_id, created_at, metadata
		FROM omnisaves WHERE id = ?`, id).Scan(
		&record.ID, &record.GameID, &record.DisplayName,
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
	return record, nil
}

// tombstoneOmnisave marks a lineage deleted. The record stays so that restoring
// this store does not resurrect a save its owner threw away; a lineage the
// store never knew about needs no tombstone.
//
// This runs before the deleting transaction commits, which is the opposite of
// how a commit is recorded and for the opposite reason. Recording a commit
// early would invent a revision that a head conflict then refused. Recording a
// deletion late would leave a save that the database has forgotten and the
// store still offers, and the store is what a restore reads — a deletion that
// a crash can undo is not a deletion. A tombstone written for a delete that
// then fails is harmless and self-correcting: the row survives, so the next
// reconcile rewrites the record from it with no deletion time.
func (r *Repository) tombstoneOmnisave(id string, at time.Time) error {
	record, err := r.store.GetOmnisave(id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil
	case err != nil:
		return err
	}
	record.DeletedAt = &at
	return r.store.PutOmnisave(record)
}

// dropRevisions removes manifests after the rows behind them are gone. It runs
// after the commit, because a manifest removed for a delete that then failed
// would be a snapshot missing from a save that still exists. Failing here
// leaves manifests behind a tombstone, which a recovery skips and the next
// reconcile does not resurrect.
func (r *Repository) dropRevisions(revisionIDs []string) error {
	for _, revisionID := range revisionIDs {
		if err := r.store.RemoveRevision(revisionID); err != nil {
			return err
		}
	}
	return nil
}

// recordGame writes a game's identity, so a recovered save carries a title
// rather than an opaque identifier.
func (r *Repository) recordGame(game catalog.Game) error {
	return r.store.PutGame(store.Game{
		ID:              game.ID,
		Title:           game.Title,
		SortTitle:       game.SortTitle,
		Platform:        game.Platform,
		PlatformCompany: game.PlatformCompany,
		Publisher:       game.Publisher,
		Identifiers:     game.Identifiers,
		Fingerprints:    game.Fingerprints,
	})
}

// reconcile writes to the store what the database holds and the store lacks: a
// crash between a commit and its manifest, or a store directory deleted or
// replaced under a healthy database.
//
// It runs on every open and its cost has to stay proportional to what is
// missing rather than to the whole history, so a manifest already present is
// never rebuilt and a record already correct is never rewritten. A single
// unreadable row is reported and stepped over — the database is the live
// authority and can serve every save perfectly well, so refusing to start over
// one bad record would turn a blemish into an outage.
func (r *Repository) reconcile(ctx context.Context) error {
	games, err := r.ListGames(ctx)
	if err != nil {
		return fmt.Errorf("reconcile games: %w", err)
	}
	var failures int
	note := func(what string, err error) {
		failures++
		log.Printf("save store: could not record %s: %v", what, err)
	}

	for _, game := range games {
		if err := r.recordGame(game); err != nil {
			note("game "+game.ID, err)
		}
	}

	saves, err := r.ListOmnisaves(ctx)
	if err != nil {
		return fmt.Errorf("reconcile saves: %w", err)
	}
	for _, save := range saves {
		if err := r.recordOmnisave(ctx, save.ID); err != nil {
			note("save "+save.ID, err)
		}
		missing, err := r.unrecordedRevisions(ctx, save.ID)
		if err != nil {
			note("revisions of "+save.ID, err)
			continue
		}
		for _, revisionID := range missing {
			if err := r.recordRevision(ctx, revisionID); err != nil {
				note("revision "+revisionID, err)
			}
		}
	}
	if failures > 0 {
		log.Printf("save store: %d record(s) could not be written; the database still holds them", failures)
	}
	return nil
}

// unrecordedRevisions names the revisions of a save that have no manifest yet.
// Asking the store first costs one stat per revision, against the several
// queries that building a manifest would take — and on a healthy store the
// answer is almost always none of them.
func (r *Repository) unrecordedRevisions(ctx context.Context, omnisaveID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id FROM revisions WHERE omnisave_id = ?`, omnisaveID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var missing []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if !r.store.HasRevision(id) {
			missing = append(missing, id)
		}
	}
	return missing, rows.Err()
}

// lineagesOfGame maps each of a game's saves to its revisions, for a deletion
// that has to record the same in the store once the rows are gone.
func lineagesOfGame(ctx context.Context, tx *sql.Tx, gameID string) (map[string][]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT omnisaves.id, revisions.id
		FROM omnisaves LEFT JOIN revisions ON revisions.omnisave_id = omnisaves.id
		WHERE omnisaves.game_id = ?`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lineages := make(map[string][]string)
	for rows.Next() {
		var omnisaveID string
		var revisionID sql.NullString
		if err := rows.Scan(&omnisaveID, &revisionID); err != nil {
			return nil, err
		}
		if _, seen := lineages[omnisaveID]; !seen {
			lineages[omnisaveID] = nil
		}
		if revisionID.Valid {
			lineages[omnisaveID] = append(lineages[omnisaveID], revisionID.String)
		}
	}
	return lineages, rows.Err()
}

// revisionIDsOf lists a lineage's revisions, for a deletion that has to remove
// their manifests after the rows are gone.
func revisionIDsOf(ctx context.Context, tx *sql.Tx, omnisaveID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM revisions WHERE omnisave_id = ?`, omnisaveID)
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
