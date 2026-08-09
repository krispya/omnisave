package sqlite

import (
	"context"
	"database/sql"
	"log"
)

// This file reclaims at open what deletion marked and a crash left behind.
//
// Runtime deletion works in two steps: the transaction removes the rows and
// records the tombstones, and reclamation removes the manifests and objects
// afterwards. Everything after the commit can be interrupted, and the repair
// passes never delete — rebuild only adds to the database, reconcile only
// adds to the store — so the leftovers would otherwise accumulate forever: a
// manifest behind a tombstone, an object nothing references, an index row for
// either. The sweep collects them on open, after both repair passes, while
// nothing else is running. That timing is what makes it safe to delete
// blindly: with rebuild finished, the database names every live revision and
// every file those revisions hold, so what the sweep removes is provably
// dead, and no request can be creating a reference while it decides.
//
// The sweep stays conservative where it cannot judge. An unreadable lineage
// record may name tombstones it cannot report, and an unreadable manifest may
// be history — both are skipped, and skipping only ever keeps too much.

// sweep removes tombstone-condemned manifests and unreferenced objects. Its
// error is for the caller to log: a failed sweep leaves leftovers for the
// next one, never an unservable store.
func (r *Repository) sweep(ctx context.Context) error {
	// The tombstones say which manifests are leftovers rather than history.
	tombstonedSaves := make(map[string]bool)
	deletedRevisions := make(map[string]bool)
	if err := r.store.EachOmnisaveID(func(id string) error {
		record, err := r.store.GetOmnisave(id)
		if err != nil {
			return nil
		}
		if record.DeletedAt != nil {
			tombstonedSaves[record.ID] = true
		}
		for _, revisionID := range record.DeletedRevisions {
			deletedRevisions[revisionID] = true
		}
		return nil
	}); err != nil {
		return err
	}

	knownRevisions, err := tableIDs(ctx, r.db, "revisions")
	if err != nil {
		return err
	}
	removedManifests := 0
	if err := r.store.EachRevisionID(func(id string) error {
		// A manifest the database holds is live history, whatever a stale
		// tombstone says; the record rewrite that clears the tombstone may
		// simply not have happened yet.
		if knownRevisions[id] {
			return nil
		}
		condemned := deletedRevisions[id]
		if !condemned {
			manifest, err := r.store.GetRevision(id)
			if err != nil {
				return nil
			}
			condemned = tombstonedSaves[manifest.Omnisave.ID]
		}
		if !condemned {
			return nil
		}
		if err := r.store.RemoveRevision(id); err != nil {
			log.Printf("save store: could not sweep manifest %s: %v", id, err)
			return nil
		}
		removedManifests++
		return nil
	}); err != nil {
		return err
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
		if err := r.store.RemoveObject(hash); err != nil {
			log.Printf("save store: could not sweep object %s: %v", hash, err)
			return nil
		}
		removedObjects++
		return nil
	}); err != nil {
		return err
	}
	// Index rows for content that no longer rests here would answer a HEAD
	// with a size the store cannot honor, so they leave with their objects.
	if _, err := r.db.ExecContext(ctx, `DELETE FROM artifacts WHERE sha256 NOT IN (
		SELECT artifact_sha256 FROM revision_files
		UNION SELECT sha256 FROM game_media
	)`); err != nil {
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
