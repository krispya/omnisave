package sqlite_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

// This is the test the store exists for. A server runs, saves accumulate, and
// then the store directory alone is copied somewhere else — no database, no
// configuration, no server. Everything about every save has to still be there.
//
// It reads the copy through the store package only, exactly as a recovery tool
// or a person with a text editor would, and reconstructs each save from what it
// finds: the lineages, their names, their histories, the file each revision
// held, and the bytes of that file.
func TestSavesAreRecoverableFromACopyOfTheStoreAlone(t *testing.T) {
	ctx := context.Background()
	original := t.TempDir()
	repository, err := sqlite.Open(
		filepath.Join(original, "omnisave.db"),
		filepath.Join(original, "store"),
	)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)

	if err := repository.SaveGame(ctx, catalog.Game{
		ID:       "game-chrono",
		Title:    "Chrono Trigger",
		Platform: "snes",
		Identifiers: []catalog.GameIdentifier{
			{Namespace: "hasheous.game", Value: "1234"},
		},
	}, nil); err != nil {
		t.Fatal(err)
	}

	save, err := saves.Create(ctx, omnisave.CreateOmnisave{
		GameID: "game-chrono", DisplayName: "Main run",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Three commits followed by a rewind: recovery must retain the whole graph
	// and its explicit pointer rather than infer the newest node as current.
	contents := []string{"save at the millennial fair", "save at the ocean palace", "save at the black omen"}
	var revisions []omnisave.Revision
	var current *string
	for _, payload := range contents {
		artifact := storeOmnisaveArtifact(t, ctx, saves, payload)
		revision, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
			ExpectedCurrentRevisionID: current,
			Upserts:                   []omnisave.RevisionFile{{Path: "saves/chrono.srm", Artifact: artifact}},
		})
		if err != nil {
			t.Fatal(err)
		}
		revisions = append(revisions, *revision)
		current = &revisions[len(revisions)-1].ID
	}
	if _, err := saves.Restore(ctx, save.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &revisions[2].ID,
		RevisionID:                revisions[0].ID,
	}); err != nil {
		t.Fatal(err)
	}

	// A second lineage, forked from the middle of the first, so the copy has to
	// carry more than one save and keep their histories apart.
	fork, err := saves.Fork(ctx, save.ID, omnisave.ForkOmnisave{
		RevisionID: revisions[1].ID, DisplayName: "Ocean palace retry",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// Copy the directory the way somebody would: the store only, nothing else.
	elsewhere := filepath.Join(t.TempDir(), "copied-store")
	copyDirectory(t, filepath.Join(original, "store"), elsewhere)

	recovered := recoverFromStore(t, elsewhere)
	if len(recovered) != 2 {
		t.Fatalf("expected two saves recovered, got %d", len(recovered))
	}

	main, found := recovered[save.ID]
	if !found {
		t.Fatal("the main save was not recovered")
	}
	if main.DisplayName != "Main run" {
		t.Fatalf("expected the save's name, got %q", main.DisplayName)
	}
	if main.GameTitle != "Chrono Trigger" {
		t.Fatalf("expected the game's title, got %q", main.GameTitle)
	}
	if len(main.History) != 3 {
		t.Fatalf("expected three revisions of history, got %d", len(main.History))
	}
	if main.CurrentID != revisions[0].ID {
		t.Fatalf("expected the rewound revision to remain current, got %s", main.CurrentID)
	}
	if len(main.Files) != 1 || main.Files["saves/chrono.srm"] != contents[0] {
		t.Fatalf("expected the current save file back, got %v", main.Files)
	}

	forked, found := recovered[fork.Omnisave.ID]
	if !found {
		t.Fatal("the forked save was not recovered")
	}
	if forked.DisplayName != "Ocean palace retry" {
		t.Fatalf("expected the fork's name, got %q", forked.DisplayName)
	}
	if forked.Files["saves/chrono.srm"] != contents[1] {
		t.Fatalf("expected the fork to hold the content it forked from, got %v", forked.Files)
	}
}

// A save the owner deleted must not come back when the store is restored.
func TestDeletedSavesStayDeletedInACopyOfTheStore(t *testing.T) {
	ctx := context.Background()
	original := t.TempDir()
	repository, err := sqlite.Open(
		filepath.Join(original, "omnisave.db"),
		filepath.Join(original, "store"),
	)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)

	kept, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1", DisplayName: "Kept"})
	if err != nil {
		t.Fatal(err)
	}
	discarded, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1", DisplayName: "Discarded"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "content of a save about to be thrown away")
	if _, err := saves.CommitRevision(ctx, discarded.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := saves.Delete(ctx, discarded.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	elsewhere := filepath.Join(t.TempDir(), "copied-store")
	copyDirectory(t, filepath.Join(original, "store"), elsewhere)

	recovered := recoverFromStore(t, elsewhere)
	if _, found := recovered[discarded.ID]; found {
		t.Fatal("a deleted save was resurrected by recovery")
	}
	if _, found := recovered[kept.ID]; !found {
		t.Fatal("the kept save was lost")
	}

	// The tombstone stays, so a recovery can say the save was deleted rather
	// than that it never existed.
	copied, err := store.Open(elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	record, err := copied.GetOmnisave(discarded.ID)
	if err != nil {
		t.Fatalf("expected the deleted save's record to remain: %v", err)
	}
	if record.DeletedAt == nil {
		t.Fatal("expected the record to carry a deletion time")
	}
}

// A crash between a commit and its manifest, or a store deleted out from under
// a healthy database, is repaired on the next open.
func TestOpeningRebuildsWhatTheStoreIsMissing(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1", DisplayName: "Only save"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "save content")
	revision, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// Lose the records but keep the content, which is what a crash after
	// storing artifacts and before writing a manifest leaves behind.
	for _, kind := range []string{"revisions", "omnisaves", "games"} {
		if err := os.RemoveAll(filepath.Join(storeDir, kind)); err != nil {
			t.Fatal(err)
		}
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	manifest, err := repository.Store().GetRevision(revision.ID)
	if err != nil {
		t.Fatalf("expected the manifest to be rebuilt: %v", err)
	}
	if manifest.Omnisave.DisplayName != "Only save" || len(manifest.Files) != 1 {
		t.Fatalf("expected the rebuilt manifest to be complete, got %+v", manifest)
	}
	if manifest.Files[0].Path != "save.dat" || manifest.Files[0].SHA256 != artifact.SHA256 {
		t.Fatalf("expected the rebuilt manifest to name the file, got %+v", manifest.Files)
	}
}

// recoveredSave is one save as reconstructed from the store, with no help from
// any database: the current revision's files, resolved to their content.
type recoveredSave struct {
	DisplayName string
	GameTitle   string
	CurrentID   string
	History     []store.Revision
	Files       map[string]string
}

// recoverFromStore reconstructs every save in a store directory using nothing
// but the directory. This is the recovery a person or a tool performs, written
// out: read the lineage records, group the manifests under them, follow the
// parent links around shared ancestry, and resolve current files to content.
func recoverFromStore(t *testing.T, root string) map[string]recoveredSave {
	t.Helper()
	saveStore, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}

	lineages := make(map[string]store.Omnisave)
	if err := saveStore.EachOmnisave(func(record store.Omnisave) error {
		if record.DeletedAt == nil {
			lineages[record.ID] = record
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	history := make(map[string][]store.Revision)
	byID := make(map[string]store.Revision)
	if err := saveStore.EachRevision(func(manifest store.Revision) error {
		history[manifest.Omnisave.ID] = append(history[manifest.Omnisave.ID], manifest)
		byID[manifest.ID] = manifest
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	recovered := make(map[string]recoveredSave)
	for id, record := range lineages {
		members := make(map[string]store.Revision)
		for _, manifest := range history[id] {
			members[manifest.ID] = manifest
		}
		if record.CurrentRevisionID != nil {
			for revision, exists := byID[*record.CurrentRevisionID]; exists; revision, exists = byID[parentID(revision)] {
				members[revision.ID] = revision
				if revision.Parent == nil {
					break
				}
			}
		}
		manifests := make([]store.Revision, 0, len(members))
		for _, manifest := range members {
			manifests = append(manifests, manifest)
		}
		sort.Slice(manifests, func(a, b int) bool {
			return manifests[a].CreatedAt.Before(manifests[b].CreatedAt)
		})

		var current store.Revision
		if record.CurrentRevisionID != nil {
			current = byID[*record.CurrentRevisionID]
		}

		files := make(map[string]string, len(current.Files))
		for _, file := range current.Files {
			reader, err := saveStore.OpenObject(file.SHA256)
			if err != nil {
				t.Fatalf("recovering %s: %v", file.Path, err)
			}
			content, err := io.ReadAll(reader)
			reader.Close()
			if err != nil {
				t.Fatal(err)
			}
			if int64(len(content)) != file.Size {
				t.Fatalf("expected %s to be %d bytes, recovered %d", file.Path, file.Size, len(content))
			}
			files[file.Path] = string(content)
		}

		title := record.GameID
		if game, err := saveStore.GetGame(record.GameID); err == nil {
			title = game.Title
		}
		recovered[id] = recoveredSave{
			DisplayName: record.DisplayName,
			GameTitle:   title,
			CurrentID:   current.ID,
			History:     manifests,
			Files:       files,
		}
	}
	return recovered
}

func parentID(revision store.Revision) string {
	if revision.Parent == nil {
		return ""
	}
	return *revision.Parent
}

func copyDirectory(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(to, os.DirFS(from)); err != nil {
		t.Fatal(err)
	}
}
