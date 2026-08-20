package sqlite_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
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
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

func TestRecordsSurviveRepositoryRestart(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	displayName := "Before the final boss"
	if _, err := saves.Update(ctx, save.ID, omnisave.UpdateOmnisave{DisplayName: &displayName}); err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "game-save contents")
	revision, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "pokemon.sav", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	revisionName := "Before the Elite Four"
	if _, err := saves.UpdateRevision(ctx, save.ID, revision.ID, omnisave.UpdateRevision{
		DisplayName: &revisionName,
	}); err != nil {
		t.Fatal(err)
	}
	fork, err := saves.Fork(ctx, save.ID, omnisave.ForkOmnisave{
		RevisionID: revision.ID, DisplayName: "Alternate route",
	})
	if err != nil {
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

	stored, err := saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.GameID != "pokemon-emerald-usa" || stored.DisplayName != displayName {
		t.Fatalf("unexpected save: %v", stored)
	}
	history, err := saves.ListRevisions(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ID != revision.ID || history[0].DisplayName != revisionName {
		t.Fatalf("unexpected history: %v", history)
	}
	stored, err = saves.Get(ctx, save.ID)
	if err != nil || stored.CurrentRevisionID == nil || *stored.CurrentRevisionID != revision.ID {
		t.Fatalf("unexpected current revision after restart: %v, %v", stored, err)
	}
	storedFork, err := saves.Get(ctx, fork.Omnisave.ID)
	if err != nil || storedFork.ForkedFrom == nil || storedFork.ForkedFrom.RevisionID != revision.ID {
		t.Fatalf("unexpected fork after restart: %v, %v", storedFork, err)
	}
	forkHistory, err := saves.ListRevisions(ctx, fork.Omnisave.ID)
	if err != nil || len(forkHistory) != 1 || len(forkHistory[0].Files) != 1 {
		t.Fatalf("unexpected fork history after restart: %v, %v", forkHistory, err)
	}
	payload, err := saves.OpenArtifact(ctx, artifact.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer payload.Close()
	contents, err := io.ReadAll(payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "game-save contents" {
		t.Fatalf("unexpected artifact contents: %q", contents)
	}
}

func TestCurrentRevisionDateFollowsTheSelectedSnapshot(t *testing.T) {
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

	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.CurrentRevisionCreatedAt.Equal(stored.CreatedAt) {
		t.Fatalf("expected a fresh save current at its creation, got %v", stored.CurrentRevisionCreatedAt)
	}

	artifact := storeOmnisaveArtifact(t, ctx, saves, "game-save contents")
	revision, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "pokemon.sav", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err = saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.CurrentRevisionCreatedAt.Equal(revision.CreatedAt) {
		t.Fatalf("expected the commit to select the new revision date %v, got %v", revision.CreatedAt, stored.CurrentRevisionCreatedAt)
	}

	displayName := "After the commit"
	renamed, err := saves.Update(ctx, save.ID, omnisave.UpdateOmnisave{DisplayName: &displayName})
	if err != nil {
		t.Fatal(err)
	}
	if !renamed.CurrentRevisionCreatedAt.Equal(revision.CreatedAt) {
		t.Fatalf("expected a rename to leave the current revision date at %v, got %v", revision.CreatedAt, renamed.CurrentRevisionCreatedAt)
	}
}

// SavedAt is a client-reported fact, so it has to survive every representation
// a revision passes through: the row, the omnisave's current-revision surface,
// the portable manifest, and a rebuild from that manifest alone.
func TestSavedAtRoundTripsThroughCommitAndRebuild(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")
	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)

	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CurrentRevisionSavedAt != nil {
		t.Fatalf("expected no saved date before any commit, got %v", stored.CurrentRevisionSavedAt)
	}

	savedAt := time.Date(2024, 8, 3, 17, 12, 9, 0, time.UTC)
	artifact := storeOmnisaveArtifact(t, ctx, saves, "old game-save contents")
	revision, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		SavedAt: &savedAt,
		Upserts: []omnisave.RevisionFile{{Path: "pokemon.sav", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision.SavedAt == nil || !revision.SavedAt.Equal(savedAt) {
		t.Fatalf("expected the commit to carry the saved date %v, got %v", savedAt, revision.SavedAt)
	}
	history, err := saves.ListRevisions(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].SavedAt == nil || !history[0].SavedAt.Equal(savedAt) {
		t.Fatalf("expected the listed revision to carry the saved date, got %v", history)
	}
	stored, err = saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CurrentRevisionSavedAt == nil || !stored.CurrentRevisionSavedAt.Equal(savedAt) {
		t.Fatalf("expected the save to surface the current revision's saved date, got %v", stored.CurrentRevisionSavedAt)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// The database is lost; the portable manifests are all that remains.
	if err := os.Remove(databasePath); err != nil {
		t.Fatal(err)
	}
	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves = omnisaveservice.New(repository)
	rebuilt, err := saves.GetRevision(ctx, save.ID, revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.SavedAt == nil || !rebuilt.SavedAt.Equal(savedAt) {
		t.Fatalf("expected the rebuilt revision to keep the saved date %v, got %v", savedAt, rebuilt.SavedAt)
	}
}

func TestDeletingASourceKeepsTheRevisionGraphSharedByAFork(t *testing.T) {
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

	first, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "shared contents")
	root, err := saves.CommitRevision(ctx, first.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := saves.Fork(ctx, first.ID, omnisave.ForkOmnisave{RevisionID: root.ID})
	if err != nil {
		t.Fatal(err)
	}

	if err := saves.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	history, err := saves.ListRevisions(ctx, second.Omnisave.ID)
	if err != nil || len(history) != 1 || history[0].ID != root.ID {
		t.Fatalf("the fork should retain its shared ancestry: history=%+v err=%v", history, err)
	}
	payload, err := saves.OpenArtifact(ctx, artifact.SHA256)
	if err != nil {
		t.Fatalf("shared artifact should remain: %v", err)
	}
	payload.Close()

	if err := saves.Delete(ctx, second.Omnisave.ID); err != nil {
		t.Fatal(err)
	}
	repository.WaitForCleanup()
	if _, err := saves.OpenArtifact(ctx, artifact.SHA256); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("unreferenced artifact should be deleted, got %v", err)
	}
}

func TestDeletingASourceKeepsAForkPointTheForkRewoundBelow(t *testing.T) {
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

	source, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald"})
	if err != nil {
		t.Fatal(err)
	}
	rootArtifact := storeOmnisaveArtifact(t, ctx, saves, "start")
	root, err := saves.CommitRevision(ctx, source.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: rootArtifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	forkPointArtifact := storeOmnisaveArtifact(t, ctx, saves, "midgame")
	forkPoint, err := saves.CommitRevision(ctx, source.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &root.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: forkPointArtifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tipArtifact := storeOmnisaveArtifact(t, ctx, saves, "endgame")
	tip, err := saves.CommitRevision(ctx, source.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &forkPoint.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: tipArtifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fork, err := saves.Fork(ctx, source.ID, omnisave.ForkOmnisave{RevisionID: forkPoint.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saves.Restore(ctx, fork.Omnisave.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &forkPoint.ID,
		RevisionID:                root.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if err := saves.Delete(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	repository.WaitForCleanup()
	// The fork was started at the fork point, so deleting the source must not
	// erase that node even while the fork's current sits below it.
	history, err := saves.ListRevisions(ctx, fork.Omnisave.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("the fork should keep its fork point and ancestry: history=%+v err=%v", history, err)
	}
	payload, err := saves.OpenArtifact(ctx, forkPointArtifact.SHA256)
	if err != nil {
		t.Fatalf("the fork point's content should remain: %v", err)
	}
	payload.Close()
	if _, err := saves.GetRevision(ctx, fork.Omnisave.ID, tip.ID); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("the source-only descendant should be gone, got %v", err)
	}
	if _, err := saves.OpenArtifact(ctx, tipArtifact.SHA256); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("the source-only artifact should be deleted, got %v", err)
	}
	if _, err := saves.Restore(ctx, fork.Omnisave.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &root.ID,
		RevisionID:                forkPoint.ID,
	}); err != nil {
		t.Fatalf("the fork should fast-forward back to its fork point: %v", err)
	}
}

func TestCommitAndRefMovementAreAtomic(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	repository, err := sqlite.Open(filepath.Join(directory, "omnisave.db"), filepath.Join(directory, "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "snapshot")
	root, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	winner, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &root.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &root.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	}); !errors.Is(err, omnisave.ErrConflict) {
		t.Fatalf("expected stale commit conflict, got %v", err)
	}
	history, err := saves.ListRevisions(ctx, save.ID)
	stored, headErr := saves.Get(ctx, save.ID)
	if err != nil || headErr != nil || len(history) != 2 || stored.CurrentRevisionID == nil || *stored.CurrentRevisionID != winner.ID {
		t.Fatalf("stale revision became visible: history=%v save=%v errors=%v/%v", history, stored, err, headErr)
	}
}

func TestCatalogMediaSurvivesRepositoryRestart(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")
	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	game := catalog.Game{
		ID:             "super-mario-world",
		Title:          "Super Mario World",
		Platform:       "Super Nintendo Entertainment System",
		MetadataSource: "hasheous",
		Identifiers:    []catalog.GameIdentifier{{Namespace: "hasheous.game", Value: "337"}},
		Fingerprints: []catalog.GameFingerprint{{
			Platform: "snes", Algorithm: "sha1", Value: "6b47bb75d16514b6a476aa0c73a683a2a4c18765",
		}},
		RefreshedAt: time.Now().UTC(),
	}
	rom := catalog.GameROM{
		ID:       "smw-usa",
		GameID:   game.ID,
		SHA1:     "6b47bb75d16514b6a476aa0c73a683a2a4c18765",
		Source:   "no-intro",
		SourceID: "1628019",
	}
	if err := repository.SaveGame(ctx, game, &rom); err != nil {
		t.Fatal(err)
	}
	contents := []byte("cover image")
	sum := sha256.Sum256(contents)
	hash := hex.EncodeToString(sum[:])
	if err := repository.StoreArtifact(ctx, storage.Artifact{
		Format: "image/png", SHA256: hash, Size: int64(len(contents)),
	}, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveGameMedia(ctx, catalog.GameMedia{
		ID: "smw-cover", GameID: game.ID, Kind: "cover", Format: "image/png",
		SHA256: hash, Size: int64(len(contents)), Provider: "hasheous", ProviderID: "cover-id",
	}); err != nil {
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
	stored, err := repository.FindGameByFingerprint(ctx, game.Fingerprints[0])
	if err != nil {
		t.Fatal(err)
	}
	if stored.Title != game.Title || len(stored.Media) != 1 || len(stored.Identifiers) != 1 {
		t.Fatalf("unexpected stored catalog game: %v", stored)
	}
	payload, err := repository.OpenArtifact(ctx, stored.Media[0].SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer payload.Close()
	got, err := io.ReadAll(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, contents) {
		t.Fatalf("unexpected stored media: %q", got)
	}
}

func TestCatalogIdentityClaimsAreAtomic(t *testing.T) {
	ctx := context.Background()
	repository, err := sqlite.Open(filepath.Join(t.TempDir(), "omnisave.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	first := catalog.Game{
		ID: "first", Title: "First", MetadataSource: "client", RefreshedAt: time.Now().UTC(),
		Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "10"}},
	}
	if err := repository.SaveGame(ctx, first, nil); err != nil {
		t.Fatal(err)
	}
	conflicting := catalog.Game{
		ID: "second", Title: "Second", MetadataSource: "client", RefreshedAt: time.Now().UTC(),
		Identifiers: []catalog.GameIdentifier{
			{Namespace: "igdb.game", Value: "20"},
			{Namespace: "steam.app", Value: "10"},
		},
	}
	if err := repository.SaveGame(ctx, conflicting, nil); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("expected an identity conflict, got %v", err)
	}
	if _, err := repository.GetGame(ctx, conflicting.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("conflicting game was partially saved: %v", err)
	}
	if _, err := repository.FindGameByIdentifier(ctx, conflicting.Identifiers[0]); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("non-conflicting claim escaped the rolled back transaction: %v", err)
	}
}

// Deleting a game removes the game record, every Omnisave that references it
// along with their revision history, and any artifacts no other record uses.
func TestDeleteGameRemovesSavesAndArtifacts(t *testing.T) {
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

	game := catalog.Game{
		ID: "super-metroid", Title: "Super Metroid",
		MetadataSource: "hasheous", RefreshedAt: time.Now().UTC(),
	}
	if err := repository.SaveGame(ctx, game, nil); err != nil {
		t.Fatal(err)
	}
	cover := []byte("cover image")
	coverSum := sha256.Sum256(cover)
	coverHash := hex.EncodeToString(coverSum[:])
	if err := repository.StoreArtifact(ctx, storage.Artifact{
		Format: "image/png", SHA256: coverHash, Size: int64(len(cover)),
	}, bytes.NewReader(cover)); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveGameMedia(ctx, catalog.GameMedia{
		ID: "sm-cover", GameID: game.ID, Kind: "cover", Format: "image/png",
		SHA256: coverHash, Size: int64(len(cover)), Provider: "hasheous", ProviderID: "cover-id",
	}); err != nil {
		t.Fatal(err)
	}

	doomed, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: game.ID})
	if err != nil {
		t.Fatal(err)
	}
	shared := storeOmnisaveArtifact(t, ctx, saves, "shared contents")
	doomedRevision, err := saves.CommitRevision(ctx, doomed.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: shared}},
	})
	if err != nil {
		t.Fatal(err)
	}
	survivor, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "another-game"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saves.CommitRevision(ctx, survivor.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: shared}},
	}); err != nil {
		t.Fatal(err)
	}

	if err := repository.DeleteGame(ctx, game.ID); err != nil {
		t.Fatal(err)
	}
	repository.WaitForCleanup()

	if _, err := repository.GetGame(ctx, game.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("deleted game should be gone, got %v", err)
	}
	if _, err := saves.Get(ctx, doomed.ID); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("the game's save should be gone, got %v", err)
	}
	if repository.Store().HasRevision(doomedRevision.ID) {
		t.Fatal("the game's revision manifest should be gone")
	}
	if _, err := saves.OpenArtifact(ctx, coverHash); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("unreferenced cover artifact should be deleted, got %v", err)
	}
	payload, err := saves.OpenArtifact(ctx, shared.SHA256)
	if err != nil {
		t.Fatalf("artifact shared with another game's save should remain: %v", err)
	}
	payload.Close()
	if _, err := saves.Get(ctx, survivor.ID); err != nil {
		t.Fatalf("the other game's save should remain: %v", err)
	}
	if err := repository.DeleteGame(ctx, game.ID); err != nil {
		t.Fatalf("repeating a committed game deletion should be idempotent, got %v", err)
	}
	if err := repository.DeleteGame(ctx, "never-existed"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("deleting a game that never existed should report not found, got %v", err)
	}
}

// Deleting a game has to drop the manifests of every node in its graph by the
// game identity revisions carry — nodes whose creator save was already
// deleted, retained for a fork, have no surviving lineage to find them
// through. Missing them, the next open would resurrect the game from its
// leftover manifests.
func TestADeletedGameStaysDeletedWhenAForkOutlivedItsSource(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")
	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)

	game := catalog.Game{
		ID: "super-metroid", Title: "Super Metroid",
		MetadataSource: "hasheous", RefreshedAt: time.Now().UTC(),
	}
	if err := repository.SaveGame(ctx, game, nil); err != nil {
		t.Fatal(err)
	}
	source, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: game.ID})
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
	if _, err := saves.Fork(ctx, source.ID, omnisave.ForkOmnisave{RevisionID: second.ID}); err != nil {
		t.Fatal(err)
	}
	// The source dies first, orphaning its nodes onto the surviving fork.
	if err := saves.Delete(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteGame(ctx, game.ID); err != nil {
		t.Fatal(err)
	}
	repository.WaitForCleanup()
	for _, revisionID := range []string{first.ID, second.ID} {
		if repository.Store().HasRevision(revisionID) {
			t.Fatalf("the deleted game's manifest %s should be gone", revisionID)
		}
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	if _, err := repository.GetGame(ctx, game.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("the deleted game was resurrected by the rebuild: %v", err)
	}
	all, err := repository.ListOmnisaves(ctx)
	if err != nil || len(all) != 0 {
		t.Fatalf("the deleted game's saves should stay deleted, got %+v (%v)", all, err)
	}
}

// The current-revision check is what makes a restore atomic; a stale
// expectation is refused with the actual pointer, on the real repository and
// not only the memory fake.
func TestRestoreWithAStaleExpectationReportsTheActualCurrent(t *testing.T) {
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

	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald"})
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

	err = repository.RestoreOmnisave(ctx, save.ID, first.ID, &first.ID)
	var conflict *storage.CurrentRevisionConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("expected a current revision conflict, got %v", err)
	}
	if conflict.ActualCurrentRevisionID == nil || *conflict.ActualCurrentRevisionID != second.ID {
		t.Fatalf("expected the conflict to carry the actual current %s, got %v",
			second.ID, conflict.ActualCurrentRevisionID)
	}
	stored, err := repository.GetOmnisave(ctx, save.ID)
	if err != nil || stored.CurrentRevisionID == nil || *stored.CurrentRevisionID != second.ID {
		t.Fatalf("a stale restore moved the pointer: %v (%v)", stored, err)
	}
}

// Committing after restoring an ancestor branches the history inside the same
// Omnisave: two children of one parent, both listed.
func TestCommitAfterARestoreBranchesTheHistory(t *testing.T) {
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

	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald"})
	if err != nil {
		t.Fatal(err)
	}
	root, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeOmnisaveArtifact(t, ctx, saves, "start")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	older, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &root.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeOmnisaveArtifact(t, ctx, saves, "one way")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saves.Restore(ctx, save.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &older.ID,
		RevisionID:                root.ID,
	}); err != nil {
		t.Fatal(err)
	}
	newer, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &root.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeOmnisaveArtifact(t, ctx, saves, "another way")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if newer.ParentID == nil || *newer.ParentID != root.ID {
		t.Fatalf("expected the commit to extend the restored revision, got parent %v", newer.ParentID)
	}

	history, err := repository.ListRevisions(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("expected both branches listed, got %d revision(s)", len(history))
	}
	children := 0
	for _, revision := range history {
		if revision.ParentID != nil && *revision.ParentID == root.ID {
			children++
		}
	}
	if children != 2 {
		t.Fatalf("expected two children of the branch point, got %d", children)
	}
}

// A deleted save's URLs must be dead even while shared-ancestry rows still
// name it as their creator; sqlite and the memory fake have to agree.
func TestADeletedSavesRevisionsAreUnreachableThroughItsID(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	sqliteRepository, err := sqlite.Open(
		filepath.Join(directory, "omnisave.db"),
		filepath.Join(directory, "store"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteRepository.Close()

	repositories := map[string]storage.OmnisaveRepository{
		"sqlite": sqliteRepository,
		"memory": storagetest.NewMemoryRepository(),
	}
	for name, repository := range repositories {
		t.Run(name, func(t *testing.T) {
			now := time.Now().UTC()
			source := omnisave.Omnisave{ID: "source-" + name, GameID: "game-1", CreatedAt: now}
			if err := repository.InsertOmnisave(ctx, source); err != nil {
				t.Fatal(err)
			}
			revision := omnisave.Revision{ID: "revision-" + name, OmnisaveID: source.ID, CreatedAt: now}
			if err := repository.CommitRevision(ctx, nil, revision, false); err != nil {
				t.Fatal(err)
			}
			fork := omnisave.Omnisave{
				ID: "fork-" + name, GameID: "game-1", CurrentRevisionID: &revision.ID,
				ForkedFrom: &omnisave.ForkOrigin{OmnisaveID: source.ID, RevisionID: revision.ID},
				CreatedAt:  now,
			}
			if err := repository.ForkOmnisave(ctx, fork); err != nil {
				t.Fatal(err)
			}
			if err := repository.DeleteOmnisave(ctx, source.ID); err != nil {
				t.Fatal(err)
			}

			if _, err := repository.GetRevision(ctx, source.ID, revision.ID); !errors.Is(err, storage.ErrNotFound) {
				t.Fatalf("a deleted save's revision stayed readable: %v", err)
			}
			if _, err := repository.ListRevisions(ctx, source.ID); !errors.Is(err, storage.ErrNotFound) {
				t.Fatalf("a deleted save's history stayed listable: %v", err)
			}
			if err := repository.UpdateRevisionDisplayName(ctx, source.ID, revision.ID, "renamed"); !errors.Is(err, storage.ErrNotFound) {
				t.Fatalf("a deleted save's revision stayed renamable: %v", err)
			}
			if _, err := repository.GetRevision(ctx, fork.ID, revision.ID); err != nil {
				t.Fatalf("the fork should still reach the shared node: %v", err)
			}
		})
	}
}

// Provenance is append-only: untracking annotates the record, deleting every
// save leaves it untouched, and only deleting the game removes it. The device
// itself outlives its games.
func TestProvenanceSurvivesUntrackAndSaveDeletion(t *testing.T) {
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

	game := catalog.Game{
		ID: "super-metroid", Title: "Super Metroid",
		MetadataSource: "hasheous", RefreshedAt: time.Now().UTC(),
	}
	if err := repository.SaveGame(ctx, game, nil); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	device := catalog.Device{ID: "device-1", Name: "Steam Deck", Platform: "linux", CreatedAt: now, LastSeenAt: now}
	if err := repository.UpsertDevice(ctx, device); err != nil {
		t.Fatal(err)
	}
	if err := repository.TrackGame(ctx, game.ID, catalog.GameTracking{
		DeviceID: device.ID, Adapter: "retroarch", Installed: true,
		FirstTrackedAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: game.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := saves.Delete(ctx, save.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.GetGame(ctx, game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Provenance) != 1 || stored.Provenance[0].DeviceName != "Steam Deck" ||
		!stored.Provenance[0].Installed || stored.Provenance[0].UntrackedAt != nil {
		t.Fatalf("provenance should survive save deletion: %+v", stored.Provenance)
	}

	if err := repository.UntrackGame(ctx, game.ID, device.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	stored, err = repository.GetGame(ctx, game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Provenance) != 1 || stored.Provenance[0].UntrackedAt == nil {
		t.Fatalf("untracking should annotate the record, not remove it: %+v", stored.Provenance)
	}
	firstTracked := stored.Provenance[0].FirstTrackedAt

	if err := repository.TrackGame(ctx, game.ID, catalog.GameTracking{
		DeviceID: device.ID, Adapter: "retroarch", Installed: true,
		FirstTrackedAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	stored, err = repository.GetGame(ctx, game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Provenance[0].UntrackedAt != nil || !stored.Provenance[0].FirstTrackedAt.Equal(firstTracked) {
		t.Fatalf("re-tracking should clear untracking and keep the original first-tracked time: %+v", stored.Provenance)
	}

	if err := repository.DeleteGame(ctx, game.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveGame(ctx, catalog.Game{
		ID: "another-game", Title: "Another", MetadataSource: "client", RefreshedAt: time.Now().UTC(),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := repository.TrackGame(ctx, "another-game", catalog.GameTracking{
		DeviceID: device.ID, FirstTrackedAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("the device should outlive its deleted games: %v", err)
	}
}

func TestArtifactsRestCompressedButKeepTheirIdentity(t *testing.T) {
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

	contents := strings.Repeat("very compressible save data. ", 200)
	artifact := storeOmnisaveArtifact(t, ctx, saves, contents)

	stored, err := os.Stat(filepath.Join(directory, "store", "objects",
		artifact.SHA256[:2], artifact.SHA256+".gz"))
	if err != nil {
		t.Fatalf("expected the artifact to rest as a compressed object: %v", err)
	}
	if stored.Size() >= int64(len(contents)) {
		t.Fatalf("expected compression to shrink %d bytes, stored %d", len(contents), stored.Size())
	}
	size, err := saves.StatArtifact(ctx, artifact.SHA256)
	if err != nil || size != int64(len(contents)) {
		t.Fatalf("expected the logical size, got %d (%v)", size, err)
	}
	payload, err := saves.OpenArtifact(ctx, artifact.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer payload.Close()
	restored, err := io.ReadAll(payload)
	if err != nil || string(restored) != contents {
		t.Fatalf("expected the exact content back, got %d bytes (%v)", len(restored), err)
	}
}

// fixedNamer names every committed revision the same way, standing in for a
// game's labeler.
type fixedNamer struct{ name string }

func (n *fixedNamer) HasLabeler(context.Context, string) bool { return true }

func (n *fixedNamer) NameRevision(context.Context, string, []omnisave.RevisionFile) string {
	return n.name
}

func TestRevisionNamesRememberWhoSetThem(t *testing.T) {
	ctx := context.Background()
	original := t.TempDir()
	repository, err := sqlite.Open(
		filepath.Join(original, "omnisave.db"),
		filepath.Join(original, "store"),
	)
	if err != nil {
		t.Fatal(err)
	}
	namer := &fixedNamer{name: "Necro A5, flr 12"}
	saves := omnisaveservice.NewWithNamer(repository, namer)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-spire2"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{
			Path: "remote/run.save", Artifact: storeOmnisaveArtifact(t, ctx, saves, "floor 12"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	namer.name = "Necro A5, flr 13"
	relabeled, err := saves.LabelRevision(ctx, save.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if relabeled.DisplayName != namer.name || relabeled.NameSource != omnisave.NameSourceLabeler {
		t.Fatalf("the labeler's new answer did not replace its old one: %+v", relabeled)
	}
	displayName := "The good run"
	if _, err := saves.UpdateRevision(ctx, save.ID, first.ID, omnisave.UpdateRevision{
		DisplayName: &displayName,
	}); err != nil {
		t.Fatal(err)
	}
	namer.name = "Automation must not win"
	protected, err := saves.LabelRevision(ctx, save.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if protected.DisplayName != displayName || protected.NameSource != omnisave.NameSourceManual {
		t.Fatalf("the labeler replaced a person's name: %+v", protected)
	}
	namer.name = "Necro A5, flr 12"
	second, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &first.ID,
		Upserts: []omnisave.RevisionFile{{
			Path: "remote/run.save", Artifact: storeOmnisaveArtifact(t, ctx, saves, "floor 13"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// A database rebuilt from a copy of the store alone must still know which
	// name a person chose and which the labeler wrote, or later automation
	// could not tell whose names it may replace.
	elsewhere := t.TempDir()
	copyDirectory(t, filepath.Join(original, "store"), filepath.Join(elsewhere, "store"))
	rebuilt, err := sqlite.Open(
		filepath.Join(elsewhere, "omnisave.db"),
		filepath.Join(elsewhere, "store"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rebuilt.Close()
	renamed, err := rebuilt.GetRevision(ctx, save.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.DisplayName != "The good run" || renamed.NameSource != omnisave.NameSourceManual {
		t.Fatalf("the manual name did not survive the rebuild: %+v", renamed)
	}
	labeled, err := rebuilt.GetRevision(ctx, save.ID, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if labeled.DisplayName != "Necro A5, flr 12" || labeled.NameSource != omnisave.NameSourceLabeler {
		t.Fatalf("the labeler's name did not survive the rebuild: %+v", labeled)
	}
}

func TestDeleteRevisionPrunesAnUnneededTip(t *testing.T) {
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

	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	shared := storeOmnisaveArtifact(t, ctx, saves, "shared contents")
	unique := storeOmnisaveArtifact(t, ctx, saves, "tip-only contents")
	first, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: shared}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &first.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "extra.dat", Artifact: unique}},
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

	if err := saves.DeleteRevision(ctx, save.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	repository.WaitForCleanup()
	history, err := saves.ListRevisions(ctx, save.ID)
	if err != nil || len(history) != 1 || history[0].ID != first.ID {
		t.Fatalf("expected only the kept revision: history=%+v err=%v", history, err)
	}
	if _, err := saves.OpenArtifact(ctx, unique.SHA256); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("content only the deleted tip referenced should be gone, got %v", err)
	}
	payload, err := saves.OpenArtifact(ctx, shared.SHA256)
	if err != nil {
		t.Fatalf("content the kept revision references should remain: %v", err)
	}
	payload.Close()

	if repository.Store().HasRevision(second.ID) {
		t.Fatal("the deleted revision's manifest should be removed from the store")
	}
	marker, err := repository.Store().GetDeletion(store.DeletionRevision, second.ID)
	if err != nil || marker.TargetID != second.ID {
		t.Fatalf("expected an immutable deletion marker, got %+v (%v)", marker, err)
	}
	if err := saves.DeleteRevision(ctx, save.ID, second.ID); err != nil {
		t.Fatalf("repeating a committed revision deletion should be idempotent, got %v", err)
	}
	if err := saves.DeleteRevision(ctx, "does-not-exist", second.ID); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("a deleted revision under a missing save should stay not found, got %v", err)
	}
}

func TestDeleteRevisionRefusesWhatTheGraphStillNeeds(t *testing.T) {
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

	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "contents")
	first, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &first.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "other.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}

	refusalReason := func(err error) string {
		t.Helper()
		var inUse *omnisave.RevisionInUse
		if !errors.As(err, &inUse) {
			t.Fatalf("expected a revision-in-use refusal, got %v", err)
		}
		return inUse.Reason
	}

	if reason := refusalReason(saves.DeleteRevision(ctx, save.ID, second.ID)); reason != omnisave.RevisionInUseCurrent {
		t.Fatalf("deleting the current revision should refuse as current, got %q", reason)
	}
	if reason := refusalReason(saves.DeleteRevision(ctx, save.ID, first.ID)); reason != omnisave.RevisionInUseChildren {
		t.Fatalf("deleting a parent should refuse for its children, got %q", reason)
	}

	// A fork origin neither current anywhere nor built upon still anchors the
	// fork's ancestry, so it refuses as the fork's origin.
	if _, err := saves.Restore(ctx, save.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &second.ID,
		RevisionID:                first.ID,
	}); err != nil {
		t.Fatal(err)
	}
	fork, err := saves.Fork(ctx, save.ID, omnisave.ForkOmnisave{RevisionID: second.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saves.Restore(ctx, fork.Omnisave.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &second.ID,
		RevisionID:                first.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if reason := refusalReason(saves.DeleteRevision(ctx, save.ID, second.ID)); reason != omnisave.RevisionInUseForkOrigin {
		t.Fatalf("deleting a fork's origin should refuse as fork origin, got %q", reason)
	}

	if err := saves.DeleteRevision(ctx, save.ID, "does-not-exist"); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("an unknown revision should be not found, got %v", err)
	}
	if err := saves.DeleteRevision(ctx, "does-not-exist", second.ID); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("an unknown save should be not found, got %v", err)
	}
	other, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	if err := saves.DeleteRevision(ctx, other.ID, second.ID); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("a revision outside the save's membership should be not found, got %v", err)
	}
}

func storeOmnisaveArtifact(t *testing.T, ctx context.Context, saves omnisave.Service, contents string) omnisave.Artifact {
	t.Helper()
	sum := sha256.Sum256([]byte(contents))
	artifact := omnisave.Artifact{
		Format: "application/octet-stream", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(contents)),
	}
	if err := saves.StoreArtifact(ctx, artifact, bytes.NewReader([]byte(contents))); err != nil {
		t.Fatal(err)
	}
	return artifact
}
