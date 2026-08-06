package sqlite_test

import (
	"context"
	"fmt"
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
	var current *string
	for _, payload := range contents {
		artifact := storeOmnisaveArtifact(t, ctx, saves, payload)
		revision, err := saves.CommitRevision(ctx, main.ID, omnisave.CreateRevision{
			ExpectedCurrentRevisionID: current,
			Upserts:                   []omnisave.RevisionFile{{Path: "saves/chrono.srm", Artifact: artifact}},
		})
		if err != nil {
			t.Fatal(err)
		}
		revisions = append(revisions, *revision)
		current = &revisions[len(revisions)-1].ID
	}
	revisionName := "Before the Ocean Palace"
	if _, err := saves.UpdateRevision(ctx, main.ID, revisions[1].ID, omnisave.UpdateRevision{
		DisplayName: &revisionName,
	}); err != nil {
		t.Fatal(err)
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
	if rebuilt.CurrentRevisionID == nil || *rebuilt.CurrentRevisionID != revisions[2].ID {
		t.Fatalf("expected the newest revision to remain current, got %v", rebuilt.CurrentRevisionID)
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
	if history[1].DisplayName != revisionName {
		t.Fatalf("expected the revision name to survive rebuild, got %q", history[1].DisplayName)
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
// are imported — but the Current Revision stays where the database says: the
// database is the live authority on a save it already knows, and the imported
// history remains reachable to fast-forward onto.
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
		ExpectedCurrentRevisionID: &first.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
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
	imported, err := repository.GetRevision(ctx, save.ID, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if imported.ParentID == nil || *imported.ParentID != first.ID {
		t.Fatalf("expected the imported revision to keep its parent, got %v", imported.ParentID)
	}
	restored, err := repository.GetOmnisave(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.CurrentRevisionID == nil || *restored.CurrentRevisionID != first.ID {
		t.Fatalf("expected current where the database remembers it, got %v", restored.CurrentRevisionID)
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
	if rebuilt.CurrentRevisionID == nil || *rebuilt.CurrentRevisionID != revision.ID {
		t.Fatalf("expected current derived from the manifests, got %v", rebuilt.CurrentRevisionID)
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
	if after.CurrentRevisionID == nil || *after.CurrentRevisionID != revision.ID {
		t.Fatalf("expected the committed revision to be current, got %v", after.CurrentRevisionID)
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

// A fork's first own commit names a parent its source created, so rebuild has
// to order imports across every lineage at once: resolved per lineage, a fork
// whose manifests were reached before its source's would import that commit
// as a root and sever the chain for good. Three forks make a wrong order
// near-certain, and the loss is repeated because the old failure depended on
// map iteration order.
func TestForkRevisionChainsSurviveLosingTheDatabase(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)

	source, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-chrono", DisplayName: "Main run"})
	if err != nil {
		t.Fatal(err)
	}
	rootArtifact := storeOmnisaveArtifact(t, ctx, saves, "the start")
	root, err := saves.CommitRevision(ctx, source.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: rootArtifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	forkPointArtifact := storeOmnisaveArtifact(t, ctx, saves, "the fork point")
	forkPoint, err := saves.CommitRevision(ctx, source.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &root.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: forkPointArtifact}},
	})
	if err != nil {
		t.Fatal(err)
	}

	forkIDs := make([]string, 3)
	tips := make(map[string][]string)
	for index := range forkIDs {
		fork, err := saves.Fork(ctx, source.ID, omnisave.ForkOmnisave{
			RevisionID: forkPoint.ID, DisplayName: "Retry",
		})
		if err != nil {
			t.Fatal(err)
		}
		forkIDs[index] = fork.Omnisave.ID
		parent := forkPoint.ID
		for step := 0; step < 2; step++ {
			artifact := storeOmnisaveArtifact(t, ctx, saves,
				fmt.Sprintf("fork %d progress %d", index, step))
			revision, err := saves.CommitRevision(ctx, fork.Omnisave.ID, omnisave.CreateRevision{
				ExpectedCurrentRevisionID: &parent,
				Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
			})
			if err != nil {
				t.Fatal(err)
			}
			tips[fork.Omnisave.ID] = append(tips[fork.Omnisave.ID], revision.ID)
			parent = revision.ID
		}
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	for cycle := 0; cycle < 3; cycle++ {
		for _, leftover := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
			if err := os.Remove(leftover); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
		repository, err = sqlite.Open(databasePath, storeDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, forkID := range forkIDs {
			history, err := repository.ListRevisions(ctx, forkID)
			if err != nil {
				t.Fatal(err)
			}
			parents := make(map[string]*string, len(history))
			for _, revision := range history {
				parents[revision.ID] = revision.ParentID
			}
			if len(history) != 4 {
				t.Fatalf("cycle %d: expected the fork's chain of 4, got %d revision(s)", cycle, len(history))
			}
			own := tips[forkID]
			for id, wantParent := range map[string]string{
				own[1]:       own[0],
				own[0]:       forkPoint.ID,
				forkPoint.ID: root.ID,
			} {
				if parents[id] == nil || *parents[id] != wantParent {
					t.Fatalf("cycle %d: revision %s lost its parent %s, got %v", cycle, id, wantParent, parents[id])
				}
			}
			if parents[root.ID] != nil {
				t.Fatalf("cycle %d: the true root grew a parent: %v", cycle, parents[root.ID])
			}
		}
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Restoring an ancestor is recorded with the Omnisave record, so a save
// reconstructed from the store adopts that pointer rather than a newest tip.
func TestARewoundCurrentSurvivesLosingTheDatabase(t *testing.T) {
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
	first, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeOmnisaveArtifact(t, ctx, saves, "early")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &first.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeOmnisaveArtifact(t, ctx, saves, "late")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saves.Restore(ctx, save.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &second.ID,
		RevisionID:                first.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
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
	restored, err := repository.GetOmnisave(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.CurrentRevisionID == nil || *restored.CurrentRevisionID != first.ID {
		t.Fatalf("expected the rewound pointer to survive, got %v", restored.CurrentRevisionID)
	}
}

// Ancestors retained past their creator's deletion belong to the database as
// much as any revision, so a store directory lost under a healthy database
// gets their manifests back too — not only the ones surviving saves created.
func TestReconcileRewritesAncestorsRetainedPastTheirCreator(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	source, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1", DisplayName: "Source"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := saves.CommitRevision(ctx, source.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeOmnisaveArtifact(t, ctx, saves, "early")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := saves.CommitRevision(ctx, source.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &first.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeOmnisaveArtifact(t, ctx, saves, "late")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fork, err := saves.Fork(ctx, source.ID, omnisave.ForkOmnisave{RevisionID: second.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := saves.Delete(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// The store is lost while the database survives; reconcile regrows it.
	if err := os.RemoveAll(storeDir); err != nil {
		t.Fatal(err)
	}
	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	for _, revisionID := range []string{first.ID, second.ID} {
		if !repository.Store().HasRevision(revisionID) {
			t.Fatalf("expected the retained ancestor %s recorded again", revisionID)
		}
	}
	history, err := repository.ListRevisions(ctx, fork.Omnisave.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("expected the fork's ancestry intact: history=%+v err=%v", history, err)
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
