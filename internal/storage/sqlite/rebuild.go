package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"sort"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

// This file rebuilds the database from the store — the reverse of reconcile.
//
// The store is the durable record, so a database missing what the store holds
// is an index that has fallen behind its data: deleted outright, restored from
// an older backup, or created fresh beside a store carried over from another
// machine. Opening walks the store's identifiers, imports whatever the
// database does not hold, and leaves alone everything it does — the database
// stays the live authority on what it already knows.
//
// The walk reads names, not records: a record's file is named by its
// identifier, so a healthy open discovers it is missing nothing at the cost of
// a directory listing, and reads only the records it is about to import.
//
// Damage stops nothing here. An unreadable record is logged and skipped, and a
// manifest whose lineage record was lost is imported through the identity it
// carries itself — which is what the denormalized identity in manifests is
// for. The one asymmetry: a tombstoned lineage is never imported, because the
// tombstone records its owner throwing it away, and honoring that is the
// difference between recovery and resurrection.

// rebuild imports into the database what the store holds and the database
// lacks. It runs on open, before reconcile, so the two passes together leave
// database and store agreeing whichever of them survived.
func (r *Repository) rebuild(ctx context.Context) error {
	var imported struct{ games, saves, revisions, damaged, missingObjects int }
	note := func(what string, err error) {
		imported.damaged++
		log.Printf("save store: could not read %s during rebuild: %v", what, err)
	}

	knownGames, err := tableIDs(ctx, r.db, "games")
	if err != nil {
		return err
	}
	knownSaves, err := tableIDs(ctx, r.db, "omnisaves")
	if err != nil {
		return err
	}
	knownRevisions, err := tableIDs(ctx, r.db, "revisions")
	if err != nil {
		return err
	}
	revisionNames := make(map[string]map[string]string)

	// Games first: they reference nothing, and the lineages imported after
	// them read better with their titles already in place.
	if err := r.store.EachGameID(func(id string) error {
		if knownGames[id] {
			return nil
		}
		record, err := r.store.GetGame(id)
		if err != nil {
			note("game "+id, err)
			return nil
		}
		if err := r.importGame(ctx, record); err != nil {
			return err
		}
		knownGames[id] = true
		imported.games++
		return nil
	}); err != nil {
		return err
	}

	// Lineage records: import live ones the database lacks, and remember the
	// tombstoned ones so their manifests — leftovers of a deletion a crash
	// interrupted — are not imported behind the owner's back.
	tombstoned := make(map[string]bool)
	if err := r.store.EachOmnisaveID(func(id string) error {
		record, err := r.store.GetOmnisave(id)
		if err != nil {
			note("save "+id, err)
			return nil
		}
		revisionNames[id] = record.RevisionNames
		if knownSaves[id] {
			return nil
		}
		if record.DeletedAt != nil {
			tombstoned[id] = true
			return nil
		}
		if err := r.importOmnisave(ctx, record); err != nil {
			return err
		}
		knownSaves[id] = true
		imported.saves++
		return nil
	}); err != nil {
		return err
	}

	// Manifests the database lacks, grouped by lineage. Only these are read.
	arrivals := make(map[string][]store.Revision)
	if err := r.store.EachRevisionID(func(id string) error {
		if knownRevisions[id] {
			return nil
		}
		manifest, err := r.store.GetRevision(id)
		if err != nil {
			note("revision "+id, err)
			return nil
		}
		arrivals[manifest.Omnisave.ID] = append(arrivals[manifest.Omnisave.ID], manifest)
		return nil
	}); err != nil {
		return err
	}

	for omnisaveID, manifests := range arrivals {
		if tombstoned[omnisaveID] {
			continue
		}
		if !knownSaves[omnisaveID] {
			// No lineage record and no row: the record was lost. The manifests
			// carry the lineage's identity themselves, so the save is
			// reconstructed from them rather than abandoned with them.
			record := lineageFromManifests(manifests)
			log.Printf("save store: reconstructing save %s (%q) from its manifests; its record was lost",
				record.ID, record.DisplayName)
			if err := r.importOmnisave(ctx, record); err != nil {
				return err
			}
			knownSaves[omnisaveID] = true
			imported.saves++
		}
		// A game known nowhere else is reconstructed the same way, whether its
		// lineage's record survived or not — the manifests name the game
		// regardless of which other record was lost.
		if game, found := gameFromManifests(manifests, knownGames); found {
			if err := r.importGame(ctx, game); err != nil {
				return err
			}
			knownGames[game.ID] = true
			imported.games++
		}
		count, missing, err := r.importRevisions(
			ctx, omnisaveID, manifests, knownRevisions, revisionNames[omnisaveID],
		)
		if err != nil {
			return err
		}
		imported.revisions += count
		imported.missingObjects += missing
	}

	if imported.games+imported.saves+imported.revisions > 0 {
		log.Printf("save store: rebuilt index from the store: %d game(s), %d save(s), %d revision(s) imported",
			imported.games, imported.saves, imported.revisions)
	}
	if imported.missingObjects > 0 {
		log.Printf("save store: %d file(s) named by imported manifests have no object in the store; those snapshots are incomplete here and their other files remain recoverable", imported.missingObjects)
	}
	if imported.damaged > 0 {
		log.Printf("save store: %d record(s) could not be read and were skipped; the store may be damaged", imported.damaged)
	}
	return nil
}

// importGame writes a store game record through the same path a provider
// lookup would take, so identity claims merge instead of colliding.
func (r *Repository) importGame(ctx context.Context, record store.Game) error {
	return r.SaveGame(ctx, catalog.Game{
		ID:              record.ID,
		Title:           record.Title,
		SortTitle:       record.SortTitle,
		Platform:        record.Platform,
		PlatformCompany: record.PlatformCompany,
		Publisher:       record.Publisher,
		Identifiers:     record.Identifiers,
		Fingerprints:    record.Fingerprints,
	}, nil)
}

func (r *Repository) importOmnisave(ctx context.Context, record store.Omnisave) error {
	metadata, err := json.Marshal(record.Metadata)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO omnisaves(
		id, game_id, display_name, head_revision_id,
		forked_from_omnisave_id, forked_from_revision_id, created_at, metadata
	) VALUES (?, ?, ?, NULL, ?, ?, ?, ?)`,
		record.ID, record.GameID, record.DisplayName,
		forkOmnisaveID(record.ForkedFrom), forkRevisionID(record.ForkedFrom),
		record.CreatedAt.Format(time.RFC3339Nano), string(metadata))
	return err
}

// importRevisions inserts a lineage's arrived manifests parents-first, then
// derives the head — the revision no other revision of the lineage claims as
// its parent — and settles it in the row. It reports how many revisions were
// imported and how many of their files name objects the store does not hold:
// those are counted rather than refused, because a partial copy loses the
// missing bytes either way and the manifest is the only thing that still says
// what they were.
func (r *Repository) importRevisions(
	ctx context.Context,
	omnisaveID string,
	manifests []store.Revision,
	knownRevisions map[string]bool,
	revisionNames map[string]string,
) (int, int, error) {
	ordered, orphaned := orderForImport(manifests, knownRevisions)
	for _, manifest := range orphaned {
		log.Printf("save store: revision %s of save %s names parent %s, which is nowhere; importing it as a root",
			manifest.ID, omnisaveID, *manifest.Parent)
	}

	missingObjects := 0
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	for _, manifest := range ordered {
		metadata, err := json.Marshal(manifest.Metadata)
		if err != nil {
			return 0, 0, err
		}
		parent := manifest.Parent
		if parent != nil && !knownRevisions[*parent] {
			parent = nil
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO revisions(
			id, omnisave_id, display_name, parent_id, created_at, metadata
		) VALUES (?, ?, ?, ?, ?, ?)`, manifest.ID, omnisaveID, revisionNames[manifest.ID], parent,
			manifest.CreatedAt.Format(time.RFC3339Nano), string(metadata)); err != nil {
			return 0, 0, err
		}
		for _, file := range manifest.Files {
			if _, err := tx.ExecContext(ctx, `INSERT INTO revision_files(
				revision_id, path, artifact_format, artifact_sha256, artifact_size
			) VALUES (?, ?, ?, ?, ?)`, manifest.ID, file.Path, file.Format, file.SHA256, file.Size); err != nil {
				return 0, 0, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO artifacts(sha256, size)
				VALUES (?, ?)`, file.SHA256, file.Size); err != nil {
				return 0, 0, err
			}
			if !r.store.HasObject(file.SHA256) {
				missingObjects++
			}
		}
		knownRevisions[manifest.ID] = true
	}

	if _, err := tx.ExecContext(ctx, `UPDATE omnisaves SET head_revision_id = (
			SELECT r.id FROM revisions r WHERE r.omnisave_id = omnisaves.id
			AND NOT EXISTS (SELECT 1 FROM revisions c
				WHERE c.omnisave_id = r.omnisave_id AND c.parent_id = r.id)
			ORDER BY r.created_at DESC, r.id DESC LIMIT 1
		) WHERE id = ?`, omnisaveID); err != nil {
		return 0, 0, err
	}
	return len(ordered), missingObjects, tx.Commit()
}

// orderForImport sorts manifests so every parent precedes its children, which
// is the order the parent_id constraint accepts. A manifest whose parent is
// neither here nor already in the database is returned in orphaned as well:
// its snapshot is complete — manifests never depend on their parents for
// content — but its place in the chain is lost, so it imports as a root.
func orderForImport(manifests []store.Revision, knownRevisions map[string]bool) (ordered, orphaned []store.Revision) {
	sort.Slice(manifests, func(a, b int) bool {
		if !manifests[a].CreatedAt.Equal(manifests[b].CreatedAt) {
			return manifests[a].CreatedAt.Before(manifests[b].CreatedAt)
		}
		return manifests[a].ID < manifests[b].ID
	})

	arriving := make(map[string]bool, len(manifests))
	for _, manifest := range manifests {
		arriving[manifest.ID] = true
	}
	placed := make(map[string]bool, len(manifests))
	remaining := manifests
	for len(remaining) > 0 {
		var deferred []store.Revision
		progressed := false
		for _, manifest := range remaining {
			switch {
			case manifest.Parent == nil,
				knownRevisions[*manifest.Parent],
				placed[*manifest.Parent]:
				ordered = append(ordered, manifest)
				placed[manifest.ID] = true
				progressed = true
			case !arriving[*manifest.Parent]:
				orphaned = append(orphaned, manifest)
				ordered = append(ordered, manifest)
				placed[manifest.ID] = true
				progressed = true
			default:
				deferred = append(deferred, manifest)
			}
		}
		if !progressed {
			// A parent cycle cannot be written by this program; a hand-edited
			// or damaged store could hold one. Import the rest as roots.
			for _, manifest := range remaining {
				orphaned = append(orphaned, manifest)
				ordered = append(ordered, manifest)
			}
			break
		}
		remaining = deferred
	}
	return ordered, orphaned
}

// lineageFromManifests reconstructs a lineage record from what its manifests
// carry. The newest manifest names the save as it was last committed — a
// rename after the final commit is lost, which is the price of losing the
// record — and the oldest bounds when the lineage began.
func lineageFromManifests(manifests []store.Revision) store.Omnisave {
	newest, oldest := manifests[0], manifests[0]
	for _, manifest := range manifests[1:] {
		if manifest.CreatedAt.After(newest.CreatedAt) {
			newest = manifest
		}
		if manifest.CreatedAt.Before(oldest.CreatedAt) {
			oldest = manifest
		}
	}
	return store.Omnisave{
		ID:          newest.Omnisave.ID,
		GameID:      newest.Game.ID,
		DisplayName: newest.Omnisave.DisplayName,
		CreatedAt:   oldest.CreatedAt,
	}
}

// gameFromManifests synthesizes a catalog identity for a game whose record was
// lost along with the database, from the identity its manifests carry. Only a
// game with a title is worth inventing a record for; an identifier alone adds
// nothing a manifest does not already hold.
func gameFromManifests(manifests []store.Revision, knownGames map[string]bool) (store.Game, bool) {
	newest := manifests[0]
	for _, manifest := range manifests[1:] {
		if manifest.CreatedAt.After(newest.CreatedAt) {
			newest = manifest
		}
	}
	game := newest.Game
	if game.ID == "" || game.Title == "" || knownGames[game.ID] {
		return store.Game{}, false
	}
	return store.Game{
		ID:          game.ID,
		Title:       game.Title,
		Platform:    game.Platform,
		Identifiers: game.Identifiers,
	}, true
}

func tableIDs(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM `+table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}
