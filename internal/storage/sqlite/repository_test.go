package sqlite_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
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
	save, err := saves.Create(ctx, omnisave.CreateOmniSave{
		GameID: "pokemon-emerald-usa",
		Slot:   "slot-1",
	})
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

	first, err := saves.Create(ctx, omnisave.CreateOmniSave{GameID: "pokemon-emerald", Slot: "one"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := saves.Create(ctx, omnisave.CreateOmniSave{GameID: "pokemon-emerald", Slot: "two"})
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
