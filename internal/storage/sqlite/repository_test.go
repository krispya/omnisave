package sqlite_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage"
	"github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
)

func TestRecordsSurviveRepositoryRestart(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	artifactDir := filepath.Join(directory, "artifacts")

	repository, err := sqlite.Open(databasePath, artifactDir)
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
	fork, err := saves.Fork(ctx, save.ID, omnisave.ForkOmnisave{
		RevisionID: revision.ID, DisplayName: "Alternate route",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, artifactDir)
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
	if len(history) != 1 || history[0].ID != revision.ID {
		t.Fatalf("unexpected history: %v", history)
	}
	stored, err = saves.Get(ctx, save.ID)
	if err != nil || stored.HeadRevisionID == nil || *stored.HeadRevisionID != revision.ID {
		t.Fatalf("unexpected head after restart: %v, %v", stored, err)
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

func TestDeleteOmnisaveKeepsSharedArtifacts(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	repository, err := sqlite.Open(
		filepath.Join(directory, "omnisave.db"),
		filepath.Join(directory, "artifacts"),
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
	second, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "shared contents")
	_, err = saves.CommitRevision(ctx, first.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saves.CommitRevision(ctx, second.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	}); err != nil {
		t.Fatal(err)
	}

	if err := saves.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	payload, err := saves.OpenArtifact(ctx, artifact.SHA256)
	if err != nil {
		t.Fatalf("shared artifact should remain: %v", err)
	}
	payload.Close()

	if err := saves.Delete(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := saves.OpenArtifact(ctx, artifact.SHA256); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("unreferenced artifact should be deleted, got %v", err)
	}
}

func TestCommitAndRefMovementAreAtomic(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	repository, err := sqlite.Open(filepath.Join(directory, "omnisave.db"), filepath.Join(directory, "artifacts"))
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
		ExpectedHeadID: &root.ID,
		Upserts:        []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedHeadID: &root.ID,
		Upserts:        []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	}); !errors.Is(err, omnisave.ErrConflict) {
		t.Fatalf("expected stale commit conflict, got %v", err)
	}
	history, err := saves.ListRevisions(ctx, save.ID)
	stored, headErr := saves.Get(ctx, save.ID)
	if err != nil || headErr != nil || len(history) != 2 || stored.HeadRevisionID == nil || *stored.HeadRevisionID != winner.ID {
		t.Fatalf("stale revision became visible: history=%v save=%v errors=%v/%v", history, stored, err, headErr)
	}
}

func TestCatalogMediaSurvivesRepositoryRestart(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	artifactDir := filepath.Join(directory, "artifacts")
	repository, err := sqlite.Open(databasePath, artifactDir)
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

	repository, err = sqlite.Open(databasePath, artifactDir)
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
		filepath.Join(directory, "artifacts"),
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
	if _, err := saves.CommitRevision(ctx, doomed.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: shared}},
	}); err != nil {
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

	if _, err := repository.GetGame(ctx, game.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("deleted game should be gone, got %v", err)
	}
	if _, err := saves.Get(ctx, doomed.ID); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("the game's save should be gone, got %v", err)
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
	if err := repository.DeleteGame(ctx, game.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("deleting a missing game should report not found, got %v", err)
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
