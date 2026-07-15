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
	save, err := saves.Create(ctx, omnisave.CreateOmniSave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	displayName := "Before the final boss"
	if _, err := saves.Update(ctx, save.ID, omnisave.UpdateOmniSave{DisplayName: &displayName}); err != nil {
		t.Fatal(err)
	}
	revision, err := saves.AddRevision(ctx, save.ID, omnisave.CreateRevision{
		Format: "application/vnd.omnisave.raw-save.v1",
	}, bytes.NewReader([]byte("game-save contents")))
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
	payload, err := saves.OpenArtifact(ctx, revision.Artifact.SHA256)
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

func TestDeleteOmniSaveKeepsSharedArtifacts(t *testing.T) {
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

	first, err := saves.Create(ctx, omnisave.CreateOmniSave{GameID: "pokemon-emerald"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := saves.Create(ctx, omnisave.CreateOmniSave{GameID: "pokemon-emerald"})
	if err != nil {
		t.Fatal(err)
	}
	firstRevision, err := saves.AddRevision(ctx, first.ID, omnisave.CreateRevision{
		Format: "application/vnd.omnisave.raw-save.v1",
	}, bytes.NewReader([]byte("shared contents")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saves.AddRevision(ctx, second.ID, omnisave.CreateRevision{
		Format: "application/vnd.omnisave.raw-save.v1",
	}, bytes.NewReader([]byte("shared contents"))); err != nil {
		t.Fatal(err)
	}

	if err := saves.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	payload, err := saves.OpenArtifact(ctx, firstRevision.Artifact.SHA256)
	if err != nil {
		t.Fatalf("shared artifact should remain: %v", err)
	}
	payload.Close()

	if err := saves.Delete(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := saves.OpenArtifact(ctx, firstRevision.Artifact.SHA256); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("unreferenced artifact should be deleted, got %v", err)
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
		ID:          "super-mario-world",
		Title:       "Super Mario World",
		Platform:    "Super Nintendo Entertainment System",
		Provider:    "hasheous",
		ProviderID:  "337",
		RefreshedAt: time.Now().UTC(),
	}
	rom := catalog.GameROM{
		ID:       "smw-usa",
		GameID:   game.ID,
		SHA1:     "6b47bb75d16514b6a476aa0c73a683a2a4c18765",
		Source:   "no-intro",
		SourceID: "1628019",
	}
	if err := repository.SaveGame(ctx, game, rom); err != nil {
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
	stored, err := repository.FindGameByFingerprint(ctx, catalog.Fingerprint{SHA1: rom.SHA1})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Title != game.Title || len(stored.Media) != 1 {
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
