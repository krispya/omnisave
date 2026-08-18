package sqlite_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
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

	// The immutable marker stays, so recovery can distinguish deletion from
	// than that it never existed.
	copied, err := store.Open(elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := copied.GetDeletion(store.DeletionOmnisave, discarded.ID)
	if err != nil || marker.TargetID != discarded.ID {
		t.Fatalf("expected the copy to carry the deletion marker: %+v (%v)", marker, err)
	}
}

func TestCommittedDeletionOverridesAnOlderDatabaseBackup(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")
	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1", DisplayName: "Discarded"})
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
	if err := omnisaveservice.New(repository).Delete(ctx, save.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databasePath, backup, 0o644); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	if _, err := repository.GetOmnisave(ctx, save.ID); err == nil {
		t.Fatal("an older database backup overrode the committed deletion marker")
	}
}

func TestDeletedGamesStayDeletedWhenOnlyThePortableStoreSurvives(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")
	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	game := catalog.Game{ID: "game-to-forget", Title: "Forgotten Game"}
	if err := repository.SaveGame(ctx, game, nil); err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: game.ID, DisplayName: "Discarded"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "discarded game content")
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteGame(ctx, game.ID); err != nil {
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
	saves = omnisaveservice.New(repository)
	if _, err := repository.GetGame(ctx, game.ID); err == nil {
		t.Fatal("the game record was resurrected despite its committed marker")
	}
	if _, err := saves.Get(ctx, save.ID); err == nil {
		t.Fatal("a save deleted with its game was resurrected")
	}
	if _, err := repository.Store().GetDeletion(store.DeletionGame, game.ID); err != nil {
		t.Fatalf("expected the game deletion marker to survive: %v", err)
	}
}

// A revision deleted out of a live save must not come back, even when a crash
// before the manifest's removal leaves the manifest in the store for the next
// open's rebuild to find.
func TestDeletedRevisionsStayDeletedWhenTheirManifestSurvives(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1", DisplayName: "Main run"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "kept content")
	first, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	discardedArtifact := storeOmnisaveArtifact(t, ctx, saves, "content about to be pruned")
	second, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &first.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: discardedArtifact}},
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

	manifest, err := repository.Store().GetRevision(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := saves.DeleteRevision(ctx, save.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	repository.WaitForCleanup()
	// The crash: the deletion committed, but the manifest outlived it.
	if err := repository.Store().PutRevision(manifest); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves = omnisaveservice.New(repository)

	history, err := saves.ListRevisions(ctx, save.ID)
	if err != nil || len(history) != 1 || history[0].ID != first.ID {
		t.Fatalf("the deleted revision should stay deleted: history=%+v err=%v", history, err)
	}
	marker, err := repository.Store().GetDeletion(store.DeletionRevision, second.ID)
	if err != nil || marker.TargetID != second.ID {
		t.Fatalf("the deletion marker should survive the reopen: %+v (%v)", marker, err)
	}
	// The sweep collects what the crash left: the manifest and the content
	// only it referenced.
	if repository.Store().HasRevision(second.ID) {
		t.Fatal("the leftover manifest should be swept at open")
	}
	if repository.Store().HasObject(discardedArtifact.SHA256) {
		t.Fatal("content only the deleted revision referenced should be swept at open")
	}
}

// Opening reclaims objects nothing references — content a crash orphaned
// between storing and committing, or a reclamation that could not finish.
func TestOpeningSweepsWhatNothingReferences(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1", DisplayName: "Main run"})
	if err != nil {
		t.Fatal(err)
	}
	kept := storeOmnisaveArtifact(t, ctx, saves, "referenced content")
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: kept}},
	}); err != nil {
		t.Fatal(err)
	}
	// An upload no revision ever came to reference.
	orphanContent := []byte("orphaned upload")
	orphanSum := sha256.Sum256(orphanContent)
	orphanHash := hex.EncodeToString(orphanSum[:])
	if _, err := repository.Store().PutObject(orphanHash, bytes.NewReader(orphanContent)); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	if repository.Store().HasObject(orphanHash) {
		t.Fatal("an object nothing references should be swept at open")
	}
	if !repository.Store().HasObject(kept.SHA256) {
		t.Fatal("referenced content must survive the sweep")
	}
}

// A tombstone written for a deletion whose transaction never committed is a
// leftover, not a decision: the database still holds the revision, so the next
// open keeps it and clears the entry.
func TestAStaleRevisionTombstoneIsClearedWhenTheDeleteNeverCommitted(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1", DisplayName: "Main run"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "content that survives")
	revision, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The crash: the tombstone landed, the deleting transaction did not.
	record, err := repository.Store().GetOmnisave(save.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.DeletedRevisions = append(record.DeletedRevisions, revision.ID)
	if err := repository.Store().PutOmnisave(record); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves = omnisaveservice.New(repository)

	history, err := saves.ListRevisions(ctx, save.ID)
	if err != nil || len(history) != 1 || history[0].ID != revision.ID {
		t.Fatalf("the revision the database still holds should survive: history=%+v err=%v", history, err)
	}
	record, err = repository.Store().GetOmnisave(save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.DeletedRevisions) != 0 {
		t.Fatalf("the stale tombstone should be cleared, got %v", record.DeletedRevisions)
	}
}

// A store that lost a manifest the database still holds must not trap the
// server: reconcile rewrites the manifest, the inventory is retaken in the
// same open, and the server comes up fully mutable — degraded mode is for
// damage no additive pass can rewrite, not for gaps the database can fill.
func TestAMissingManifestIsRepairedAndMutationsResume(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1", DisplayName: "Main run"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "content the database still holds")
	revision, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// The store loses the manifest the record's Current Revision names.
	var manifestPath string
	if err := filepath.WalkDir(filepath.Join(storeDir, "revisions"),
		func(path string, entry os.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && filepath.Ext(path) == ".json" {
				manifestPath = path
			}
			return err
		}); err != nil {
		t.Fatal(err)
	}
	if manifestPath == "" {
		t.Fatal("no manifest was written")
	}
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves = omnisaveservice.New(repository)

	if !repository.Store().HasRevision(revision.ID) {
		t.Fatal("reconcile should rewrite the manifest the database still holds")
	}
	displayName := "Renamed after repair"
	if _, err := saves.Update(ctx, save.ID, omnisave.UpdateOmnisave{DisplayName: &displayName}); err != nil {
		t.Fatalf("the repaired server should accept mutations, got %v", err)
	}
}

// A deleted save's portable record can outlive its manifests: the immutable
// deletion marker, rather than absence, is what keeps the save deleted. That
// expected leftover must not make the rest of the server read-only on restart.
func TestADeletedSaveRecordDoesNotBlockLaterMutations(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	deleted, err := saves.Create(ctx, omnisave.CreateOmnisave{
		GameID: "game-1", DisplayName: "Discarded run",
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "discarded content")
	if _, err := saves.CommitRevision(ctx, deleted.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := saves.Delete(ctx, deleted.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves = omnisaveservice.New(repository)

	if _, err := saves.Get(ctx, deleted.ID); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("the deletion marker did not keep the discarded save deleted: %v", err)
	}
	if _, err := saves.Create(ctx, omnisave.CreateOmnisave{
		GameID: "game-1", DisplayName: "New run",
	}); err != nil {
		t.Fatalf("a deleted save's stale record blocked an unrelated mutation: %v", err)
	}
}

// Queued portable-store work that cannot be applied leaves the server
// readable: recovery is deferred — rebuild must not import records the queue
// is still ahead of — and every durable mutation waits for an open that lands
// the queue.
func TestAnUnreplayableOutboxOpensReadOnly(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1", DisplayName: "Main run"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// Poison the queue the way a future format or a corrupted row would.
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO store_outbox(kind, target_id, payload, created_at)
		VALUES ('unknown-action', 'x', '{}', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatalf("an unreplayable outbox should defer recovery, not fail the open: %v", err)
	}
	defer repository.Close()
	saves = omnisaveservice.New(repository)

	listed, err := saves.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != save.ID {
		t.Fatalf("reads should serve from the database: %v, %v", listed, err)
	}
	displayName := "Renamed"
	if _, err := saves.Update(ctx, save.ID, omnisave.UpdateOmnisave{DisplayName: &displayName}); err == nil {
		t.Fatal("durable mutations should wait for an open that lands the queued work")
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

	deletedSaves := make(map[string]bool)
	if err := saveStore.EachDeletionID(store.DeletionOmnisave, func(id string) error {
		deletedSaves[id] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	deletedRevisions := make(map[string]bool)
	if err := saveStore.EachDeletionID(store.DeletionRevision, func(id string) error {
		deletedRevisions[id] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	lineages := make(map[string]store.Omnisave)
	if err := saveStore.EachOmnisave(func(record store.Omnisave) error {
		if !deletedSaves[record.ID] {
			lineages[record.ID] = record
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	history := make(map[string][]store.Revision)
	byID := make(map[string]store.Revision)
	if err := saveStore.EachRevision(func(manifest store.Revision) error {
		if deletedSaves[manifest.Omnisave.ID] || deletedRevisions[manifest.ID] {
			return nil
		}
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
