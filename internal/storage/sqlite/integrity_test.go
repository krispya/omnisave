package sqlite_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage"
	"github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

// The size an upload declares is what the API answers a HEAD with, and a
// deduplicated upload is the one path where nothing is read to check it
// against. A client that uploads content the server already holds must not be
// able to redefine its size.
func TestDeclaredSizeIsNeverTakenOnFaith(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	repository, err := sqlite.Open(
		filepath.Join(directory, "omnisave.db"),
		filepath.Join(directory, "store"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves := omnisaveservice.New(repository)

	contents := "honest save content"
	artifact := storeOmnisaveArtifact(t, ctx, saves, contents)
	if size, err := saves.StatArtifact(ctx, artifact.SHA256); err != nil || size != int64(len(contents)) {
		t.Fatalf("expected %d bytes indexed, got %d (%v)", len(contents), size, err)
	}

	// The same content again, with a size the caller made up. The store already
	// holds this object, so the payload is never read.
	lying := omnisave.Artifact{
		Format: artifact.Format, SHA256: artifact.SHA256, Size: 99999,
	}
	err = saves.StoreArtifact(ctx, lying, strings.NewReader(contents))
	if !errors.Is(err, omnisave.ErrInvalid) {
		t.Fatalf("expected a mismatch for a lied-about size, got %v", err)
	}
	if size, err := saves.StatArtifact(ctx, artifact.SHA256); err != nil || size != int64(len(contents)) {
		t.Fatalf("expected the true size to survive, got %d (%v)", size, err)
	}
}

// Service-layer preflight is advisory: an object can disappear after HEAD and
// before the revision transaction. The repository re-proves availability at
// the reference-creating boundary, so no row can acknowledge missing bytes.
func TestRevisionCommitRequiresArtifactAvailabilityAtThePersistenceBoundary(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	repository, err := sqlite.Open(
		filepath.Join(directory, "omnisave.db"),
		filepath.Join(directory, "store"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1", DisplayName: "Only save"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "bytes present during preflight")
	if err := repository.Store().RemoveObject(artifact.SHA256); err != nil {
		t.Fatal(err)
	}

	err = repository.CommitRevision(ctx, nil, omnisave.Revision{
		ID:         "revision-with-missing-bytes",
		OmnisaveID: save.ID,
		CreatedAt:  time.Now().UTC(),
		Files: []omnisave.RevisionFile{{
			Path: "save.dat", Artifact: artifact,
		}},
	})
	var unavailable *storage.ArtifactsUnavailable
	if !errors.As(err, &unavailable) || len(unavailable.SHA256) != 1 || unavailable.SHA256[0] != artifact.SHA256 {
		t.Fatalf("expected the persistence boundary to reject the missing object, got %v", err)
	}
	history, err := repository.ListRevisions(ctx, save.ID)
	if err != nil || len(history) != 0 {
		t.Fatalf("an unavailable artifact must not leave a revision row: %+v (%v)", history, err)
	}
}

// A portable marker may lag its committed delete when the store is down. The
// SQLite ledger closes that cross-process window: another repository cannot
// reuse the identifier merely because it has not observed the marker yet.
func TestDeletedIdentifiersCannotBeReusedBeforeOutboxProjection(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")
	first, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	game := catalog.Game{ID: "never-reused", Title: "Original"}
	if err := first.SaveGame(ctx, game, nil); err != nil {
		t.Fatal(err)
	}

	markerNamespace := filepath.Join(storeDir, "deletions", store.DeletionGame)
	if err := os.RemoveAll(markerNamespace); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerNamespace, []byte("store unavailable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := first.DeleteGame(ctx, game.ID); err == nil {
		t.Fatal("expected deletion to remain unacknowledged while its marker cannot be written")
	}
	game.Title = "Accidental reuse"
	if err := second.SaveGame(ctx, game, nil); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("expected the committed deletion ledger to reject reuse, got %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

// A store restored without its database has no size index at all, so an honest
// upload of content already present has to be confirmed against the object
// rather than rejected for lack of a row.
func TestDeduplicatedUploadIsConfirmedAgainstTheStore(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	storeDir := filepath.Join(directory, "store")
	saveStore, err := store.Open(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	contents := "content the store already holds"
	hash := sha256Hex(contents)
	if _, err := saveStore.PutObject(hash, strings.NewReader(contents)); err != nil {
		t.Fatal(err)
	}

	repository, err := sqlite.Open(filepath.Join(directory, "omnisave.db"), storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves := omnisaveservice.New(repository)

	honest := omnisave.Artifact{
		Format: "application/octet-stream", SHA256: hash, Size: int64(len(contents)),
	}
	if err := saves.StoreArtifact(ctx, honest, strings.NewReader(contents)); err != nil {
		t.Fatalf("expected an honest deduplicated upload to be accepted: %v", err)
	}
	if size, err := saves.StatArtifact(ctx, hash); err != nil || size != int64(len(contents)) {
		t.Fatalf("expected %d bytes, got %d (%v)", len(contents), size, err)
	}
}

// Reconciling writes what the database holds, but an immutable deletion marker
// remains the durable proof that a missing save was deliberately removed.
func TestReconcilingDoesNotResurrectADeletedSave(t *testing.T) {
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
	artifact := storeOmnisaveArtifact(t, ctx, saves, "content of a save about to be deleted")
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := saves.Delete(ctx, save.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopening runs the reconciling pass, which writes what the database has.
	// It must not take the tombstone back.
	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	marker, err := repository.Store().GetDeletion(store.DeletionOmnisave, save.ID)
	if err != nil || marker.TargetID != save.ID {
		t.Fatalf("expected the committed deletion marker to remain: %+v (%v)", marker, err)
	}
}

// Version 3 could leave a pre-commit tombstone beside a live row. Migration
// treats that ambiguous legacy state conservatively: the live data wins,
// because the old tombstone cannot prove its transaction committed.
func TestATombstoneForASaveThatWasNotDeletedIsCleared(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1", DisplayName: "Survivor"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	saveStore, err := store.Open(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	record, err := saveStore.GetOmnisave(save.ID)
	if err != nil {
		t.Fatal(err)
	}
	crashed := time.Now().UTC()
	record.DeletedAt = &crashed
	if err := saveStore.PutOmnisave(record); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	healed, err := repository.Store().GetOmnisave(save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if healed.DeletedAt != nil {
		t.Fatal("a tombstone for a save the database still holds was not cleared")
	}
	if healed.DisplayName != "Survivor" {
		t.Fatalf("expected the record rewritten from the row, got %q", healed.DisplayName)
	}
}

// Reconciling runs on every open and must cost only what is missing. A store
// already in step with its database is left completely untouched.
func TestReconcilingAHealthyStoreRewritesNothing(t *testing.T) {
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
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	before := modificationTimes(t, storeDir)
	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	for path, when := range modificationTimes(t, storeDir) {
		if was, existed := before[path]; !existed || !was.Equal(when) {
			t.Fatalf("reconciling rewrote %s", path)
		}
	}
}

// A record the store cannot accept must not keep the server down: the database
// holds the saves and can serve every one of them.
func TestOneUnwritableRecordDoesNotStopTheServer(t *testing.T) {
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
	orphanContent := "unreferenced content retained while reachability is unknown"
	orphanHash := sha256Hex(orphanContent)
	if _, err := repository.Store().PutObject(orphanHash, strings.NewReader(orphanContent)); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// Put a regular file where the manifest's directory belongs, so rewriting
	// it fails the way a damaged or hostile store would.
	manifests := filepath.Dir(manifestPath(t, storeDir, revision.ID))
	if err := os.RemoveAll(manifests); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifests, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatalf("expected the server to open despite an unwritable record: %v", err)
	}
	defer repository.Close()

	if _, err := omnisaveservice.New(repository).Get(ctx, save.ID); err != nil {
		t.Fatalf("expected the save to still be served: %v", err)
	}
	if !repository.Store().HasObject(orphanHash) {
		t.Fatal("incomplete recovery must retain even apparently unreferenced content")
	}
	if err := repository.UpdateOmnisaveDisplayName(ctx, save.ID, "unsafe rewrite"); err == nil {
		t.Fatal("durable mutations must stop until the portable inventory is complete")
	}
}

// manifestPath finds a revision's manifest by walking, since the store shards
// records by a hash of their identifier rather than by the identifier itself.
func manifestPath(t *testing.T, storeDir, revisionID string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(filepath.Join(storeDir, "revisions"),
		func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			if strings.Contains(entry.Name(), revisionID) {
				found = path
			}
			return nil
		})
	if err != nil || found == "" {
		t.Fatalf("could not find the manifest for %s (%v)", revisionID, err)
	}
	return found
}

func modificationTimes(t *testing.T, root string) map[string]time.Time {
	t.Helper()
	times := make(map[string]time.Time)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		times[path] = info.ModTime()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return times
}

func sha256Hex(contents string) string {
	sum := sha256.Sum256([]byte(contents))
	return hex.EncodeToString(sum[:])
}
