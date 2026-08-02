package sqlite_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
)

// This is the goal the store exists for, closed end to end: the database is
// destroyed outright, and opening the server against the surviving store gives
// everything back — games with their identity, saves with their current names,
// histories with their revision identifiers intact, content byte for byte, and
// deletions still deleted. The identifiers matter as much as the content: a
// device that held a sync baseline before the loss still names revisions the
// rebuilt server recognizes.
func TestAServerThatLostItsDatabaseRebuildsEverything(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
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

	main, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-chrono", DisplayName: "Main run"})
	if err != nil {
		t.Fatal(err)
	}
	contents := []string{"millennial fair", "ocean palace", "black omen"}
	var revisions []omnisave.Revision
	var head *string
	for _, payload := range contents {
		artifact := storeOmnisaveArtifact(t, ctx, saves, payload)
		revision, err := saves.CommitRevision(ctx, main.ID, omnisave.CreateRevision{
			ExpectedHeadID: head,
			Upserts:        []omnisave.RevisionFile{{Path: "saves/chrono.srm", Artifact: artifact}},
		})
		if err != nil {
			t.Fatal(err)
		}
		revisions = append(revisions, *revision)
		head = &revisions[len(revisions)-1].ID
	}

	fork, err := saves.Fork(ctx, main.ID, omnisave.ForkOmnisave{
		RevisionID: revisions[1].ID, DisplayName: "Ocean palace retry",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Renamed after its last commit, so the rebuilt name can only come from
	// the lineage record — the manifests all carry the old one.
	if err := repository.UpdateOmnisaveDisplayName(ctx, main.ID, "Main run, renamed"); err != nil {
		t.Fatal(err)
	}

	discarded, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-chrono", DisplayName: "Discarded"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "thrown away")
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

	// The loss. Nothing of the index survives.
	for _, leftover := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		if err := os.Remove(leftover); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves = omnisaveservice.New(repository)

	games, err := repository.ListGames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].Title != "Chrono Trigger" {
		t.Fatalf("expected the game back, got %+v", games)
	}
	if len(games[0].Identifiers) != 1 || games[0].Identifiers[0].Value != "1234" {
		t.Fatalf("expected the game's identifiers back, got %+v", games[0].Identifiers)
	}

	all, err := repository.ListOmnisaves(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected two saves back, got %d", len(all))
	}

	rebuilt, err := saves.Get(ctx, main.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.DisplayName != "Main run, renamed" {
		t.Fatalf("expected the name as last renamed, got %q", rebuilt.DisplayName)
	}
	if rebuilt.HeadRevisionID == nil || *rebuilt.HeadRevisionID != revisions[2].ID {
		t.Fatalf("expected the head at the newest revision, got %v", rebuilt.HeadRevisionID)
	}

	history, err := repository.ListRevisions(ctx, main.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("expected three revisions of history, got %d", len(history))
	}
	for index, revision := range history {
		if revision.ID != revisions[index].ID {
			t.Fatalf("expected revision %d to keep identifier %s, got %s", index, revisions[index].ID, revision.ID)
		}
	}
	if history[0].ParentID != nil || history[1].ParentID == nil || *history[1].ParentID != history[0].ID ||
		history[2].ParentID == nil || *history[2].ParentID != history[1].ID {
		t.Fatal("expected the parent chain to survive the rebuild")
	}

	restoredFork, err := saves.Get(ctx, fork.Omnisave.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restoredFork.ForkedFrom == nil || restoredFork.ForkedFrom.OmnisaveID != main.ID ||
		restoredFork.ForkedFrom.RevisionID != revisions[1].ID {
		t.Fatalf("expected the fork origin to survive, got %+v", restoredFork.ForkedFrom)
	}

	newest := history[2].Files[0]
	if size, err := saves.StatArtifact(ctx, newest.Artifact.SHA256); err != nil || size != newest.Artifact.Size {
		t.Fatalf("expected the artifact index rebuilt, got %d (%v)", size, err)
	}
	payload, err := saves.OpenArtifact(ctx, newest.Artifact.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := io.ReadAll(payload)
	payload.Close()
	if err != nil || string(restored) != contents[2] {
		t.Fatalf("expected the newest content back, got %q (%v)", restored, err)
	}

	if _, err := saves.Get(ctx, discarded.ID); err == nil {
		t.Fatal("a deleted save was resurrected by the rebuild")
	}
	record, err := repository.Store().GetOmnisave(discarded.ID)
	if err != nil || record.DeletedAt == nil {
		t.Fatalf("expected the tombstone to survive the rebuild, got %+v (%v)", record, err)
	}
}

// A database restored from an older backup is missing the revisions committed
// after the backup was taken, and the store still holds their manifests. They
// are imported and the head moves to where the store says history ended, not
// where the stale backup remembers it.
func TestRebuildImportsRevisionsADatabaseBackupMissed(t *testing.T) {
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
	artifact := storeOmnisaveArtifact(t, ctx, saves, "before the backup")
	first, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves = omnisaveservice.New(repository)
	artifact = storeOmnisaveArtifact(t, ctx, saves, "after the backup")
	second, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedHeadID: &first.ID,
		Upserts:        []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// The operator restores the stale backup over the database.
	if err := os.WriteFile(databasePath, backup, 0o644); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	history, err := repository.ListRevisions(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("expected the missed revision imported, got %d revision(s)", len(history))
	}
	restored, err := repository.GetOmnisave(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.HeadRevisionID == nil || *restored.HeadRevisionID != second.ID {
		t.Fatalf("expected the head where the store says history ended, got %v", restored.HeadRevisionID)
	}
}

// Manifests carry the lineage's identity themselves so that losing the lineage
// record — one more file that can be damaged or dropped from a partial copy —
// costs the record and not the save.
func TestALineageWhoseRecordWasLostIsRebuiltFromItsManifests(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)

	if err := repository.SaveGame(ctx, catalog.Game{ID: "game-chrono", Title: "Chrono Trigger"}, nil); err != nil {
		t.Fatal(err)
	}
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-chrono", DisplayName: "Sole survivor"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "the save itself")
	revision, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// Lose the lineage record and the whole database; keep the manifests.
	if err := os.Remove(recordPath(t, storeDir, "omnisaves", save.ID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(databasePath); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves = omnisaveservice.New(repository)

	rebuilt, err := saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatalf("expected the save reconstructed from its manifests: %v", err)
	}
	if rebuilt.DisplayName != "Sole survivor" {
		t.Fatalf("expected the name the manifests carry, got %q", rebuilt.DisplayName)
	}
	if rebuilt.HeadRevisionID == nil || *rebuilt.HeadRevisionID != revision.ID {
		t.Fatalf("expected the head derived from the manifests, got %v", rebuilt.HeadRevisionID)
	}

	// Reconciling wrote the reconstructed record back, healing the store.
	record, err := repository.Store().GetOmnisave(save.ID)
	if err != nil {
		t.Fatalf("expected the lineage record restored to the store: %v", err)
	}
	if record.DisplayName != "Sole survivor" {
		t.Fatalf("expected the restored record to carry the name, got %q", record.DisplayName)
	}
}

// A store that cannot accept a manifest must not fail the commit it trails:
// the database has already durably accepted it, and telling the client
// otherwise would make it retry a commit it holds and be told its own revision
// is a conflict. The store lags, says so in the log, and heals when it is next
// writable at open.
func TestACommitOutlivesAStoreThatCannotRecordIt(t *testing.T) {
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

	// Put a regular file where the manifests live, so recording one fails the
	// way a full or read-only disk would.
	manifests := filepath.Join(storeDir, "revisions")
	if err := os.RemoveAll(manifests); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifests, []byte("in the way"), 0o644); err != nil {
		t.Fatal(err)
	}

	revision, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatalf("expected the commit to succeed despite the store: %v", err)
	}
	after, err := saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.HeadRevisionID == nil || *after.HeadRevisionID != revision.ID {
		t.Fatalf("expected the committed revision at the head, got %v", after.HeadRevisionID)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// The store becomes writable again; the next open pays the debt.
	if err := os.Remove(manifests); err != nil {
		t.Fatal(err)
	}
	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	manifest, err := repository.Store().GetRevision(revision.ID)
	if err != nil {
		t.Fatalf("expected the manifest recorded once the store healed: %v", err)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "save.dat" {
		t.Fatalf("expected the healed manifest to name the file, got %+v", manifest.Files)
	}
}

// recordPath finds a record's file by walking its kind's directory, since the
// store shards records by a hash of their identifier.
func recordPath(t *testing.T, storeDir, kind, id string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(filepath.Join(storeDir, kind),
		func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			if strings.TrimSuffix(entry.Name(), ".json") == id {
				found = path
			}
			return nil
		})
	if err != nil || found == "" {
		t.Fatalf("could not find the %s record for %s (%v)", kind, id, err)
	}
	return found
}
