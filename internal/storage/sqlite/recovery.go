package sqlite

import (
	"errors"
	"log"

	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

var (
	errInvalidStoreIdentity        = errors.New("record identity does not match its path")
	errRecoveryInventoryIncomplete = errors.New("portable store recovery inventory is incomplete")
)

// recoveryInventory is the result of one complete namespace walk. Recovery
// may use readable records immediately, but only a complete inventory can
// prove that an absent record is truly absent and authorize reclamation.
type recoveryInventory struct {
	complete          bool
	deletionsComplete bool
	games             map[string]store.Game
	saves             map[string]store.Omnisave
	revisions         map[string]store.Revision
	deletedSaves      map[string]store.Deletion
	deletedRevisions  map[string]store.Deletion
	deletedGames      map[string]store.Deletion
}

// completeInventory is deliberately constructible only from an inventory
// whose entire reachability namespace was read. Its presence is the sweep's
// proof that unknown data was not mistaken for garbage.
type completeInventory struct {
	revisions        map[string]store.Revision
	deletedSaves     map[string]store.Deletion
	deletedRevisions map[string]store.Deletion
}

func (inventory recoveryInventory) proof() (completeInventory, bool) {
	if !inventory.complete {
		return completeInventory{}, false
	}
	return completeInventory{
		revisions:        inventory.revisions,
		deletedSaves:     inventory.deletedSaves,
		deletedRevisions: inventory.deletedRevisions,
	}, true
}

func (r *Repository) inspectStore() recoveryInventory {
	inventory := recoveryInventory{
		complete:          true,
		deletionsComplete: true,
		games:             make(map[string]store.Game),
		saves:             make(map[string]store.Omnisave),
		revisions:         make(map[string]store.Revision),
		deletedSaves:      make(map[string]store.Deletion),
		deletedRevisions:  make(map[string]store.Deletion),
		deletedGames:      make(map[string]store.Deletion),
	}
	note := func(what string, err error) {
		inventory.complete = false
		log.Printf("save store: could not inventory %s: %v", what, err)
	}
	noteDeletion := func(what string, err error) {
		inventory.deletionsComplete = false
		note(what, err)
	}

	readDeletions := func(kind string, target map[string]store.Deletion) {
		if err := r.store.EachDeletionID(kind, func(id string) error {
			marker, err := r.store.GetDeletion(kind, id)
			if err != nil {
				noteDeletion("deletion marker "+kind+"/"+id, err)
				return nil
			}
			if marker.TargetKind != kind || marker.TargetID != id || marker.DeletedAt.IsZero() {
				noteDeletion("deletion marker "+kind+"/"+id, errInvalidStoreIdentity)
				return nil
			}
			target[id] = marker
			return nil
		}); err != nil {
			noteDeletion(kind+" deletion namespace", err)
		}
	}
	readDeletions(store.DeletionOmnisave, inventory.deletedSaves)
	readDeletions(store.DeletionRevision, inventory.deletedRevisions)
	readDeletions(store.DeletionGame, inventory.deletedGames)

	if err := r.store.EachGameID(func(id string) error {
		record, err := r.store.GetGame(id)
		if err != nil {
			note("game "+id, err)
			return nil
		}
		if record.ID != id {
			note("game "+id, errInvalidStoreIdentity)
			return nil
		}
		inventory.games[id] = record
		return nil
	}); err != nil {
		note("game namespace", err)
	}
	if err := r.store.EachOmnisaveID(func(id string) error {
		record, err := r.store.GetOmnisave(id)
		if err != nil {
			note("save "+id, err)
			return nil
		}
		if record.ID != id {
			note("save "+id, errInvalidStoreIdentity)
			return nil
		}
		inventory.saves[id] = record
		return nil
	}); err != nil {
		note("save namespace", err)
	}
	if err := r.store.EachRevisionID(func(id string) error {
		manifest, err := r.store.GetRevision(id)
		if err != nil {
			note("revision "+id, err)
			return nil
		}
		if manifest.ID != id {
			note("revision "+id, errInvalidStoreIdentity)
			return nil
		}
		inventory.revisions[id] = manifest
		return nil
	}); err != nil {
		note("revision namespace", err)
	}

	for id, manifest := range inventory.revisions {
		seenPaths := make(map[string]bool, len(manifest.Files))
		invalid := manifest.Omnisave.ID == "" || manifest.Game.ID == "" ||
			manifest.Parent != nil && *manifest.Parent == id
		for _, file := range manifest.Files {
			if file.Path == "" || !store.ValidHash(file.SHA256) || file.Size < 0 || seenPaths[file.Path] {
				invalid = true
			}
			seenPaths[file.Path] = true
		}
		if invalid {
			delete(inventory.revisions, id)
			note("revision "+id, errors.New("manifest is structurally invalid"))
		}
	}
	for id, manifest := range inventory.revisions {
		if manifest.Parent == nil {
			continue
		}
		if _, found := inventory.revisions[*manifest.Parent]; !found {
			if _, deleted := inventory.deletedRevisions[*manifest.Parent]; !deleted {
				note("revision "+id, errors.New("parent manifest is missing"))
			}
		}
	}
	for id, record := range inventory.saves {
		// A committed deletion marker is positive proof that this lineage record
		// is no longer live. Its manifests may already have been reclaimed, so
		// validating the stale Current Revision or fork origin would mistake
		// successful cleanup for store damage and lock unrelated saves read-only.
		if _, deleted := inventory.deletedSaves[id]; deleted {
			continue
		}
		if record.CurrentRevisionID != nil {
			if _, deleted := inventory.deletedRevisions[*record.CurrentRevisionID]; deleted {
				record.CurrentRevisionID = nil
				inventory.saves[id] = record
			} else if _, found := inventory.revisions[*record.CurrentRevisionID]; !found {
				note("save "+id, errors.New("current revision manifest is missing"))
			}
		}
		if record.ForkedFrom != nil {
			if _, deleted := inventory.deletedRevisions[record.ForkedFrom.RevisionID]; deleted {
				record.ForkedFrom = nil
				inventory.saves[id] = record
			} else if _, found := inventory.revisions[record.ForkedFrom.RevisionID]; !found {
				note("save "+id, errors.New("fork origin manifest is missing"))
			}
		}
	}
	return inventory
}
