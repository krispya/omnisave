package sqlite

import (
	"context"
	"database/sql"
	"log"
)

// This file reclaims at open what deletion marked and a crash left behind.
//
// Runtime deletion works in two steps: the transaction removes the rows and
// records immutable markers, and reclamation removes the manifests and objects
// afterwards. Everything after the commit can be interrupted. Rebuild deletes
// only rows named by positive markers and otherwise adds; reconcile only adds
// to the store. Physical leftovers would therefore accumulate forever. The
// sweep collects them on open after both repair passes. At that point the
// database names every live revision and file, and the complete inventory
// proves no unreadable portable record was mistaken for absence.
//
// The sweep cannot be called without the complete inventory produced by
// recovery. Damage therefore disables the whole pass instead of turning an
// unreadable record into evidence that its content is garbage.

// sweep removes marker-condemned manifests and unreferenced objects. Its
// error is for the caller to log: a failed sweep leaves leftovers for the
// next one, never an unservable store.
func (r *Repository) sweep(ctx context.Context, proof completeInventory) error {
	knownRevisions, err := tableIDs(ctx, r.db, "revisions")
	if err != nil {
		return err
	}
	removedManifests := 0
	for id, manifest := range proof.revisions {
		if knownRevisions[id] {
			continue
		}
		_, revisionDeleted := proof.deletedRevisions[id]
		_, saveDeleted := proof.deletedSaves[manifest.Omnisave.ID]
		condemned := revisionDeleted || saveDeleted
		if !condemned {
			continue
		}
		if err := r.store.RemoveRevision(id); err != nil {
			log.Printf("save store: could not sweep manifest %s: %v", id, err)
			continue
		}
		removedManifests++
	}

	referenced, err := referencedArtifacts(ctx, r.db)
	if err != nil {
		return err
	}
	removedObjects := 0
	if err := r.store.EachObject(func(hash string) error {
		if referenced[hash] {
			return nil
		}
		r.reclaimArtifacts(ctx, []string{hash})
		if r.store.HasObject(hash) {
			return nil
		}
		removedObjects++
		return nil
	}); err != nil {
		return err
	}
	if removedManifests+removedObjects > 0 {
		log.Printf("save store: swept %d leftover manifest(s) and %d unreferenced object(s)",
			removedManifests, removedObjects)
	}
	return nil
}

// referencedArtifacts is every hash the database still needs: the files of
// every live revision and every piece of game media.
func referencedArtifacts(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT artifact_sha256 FROM revision_files
		UNION SELECT sha256 FROM game_media`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	referenced := make(map[string]bool)
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		referenced[hash] = true
	}
	return referenced, rows.Err()
}
