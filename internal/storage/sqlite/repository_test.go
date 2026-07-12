package sqlite_test

import (
	"bytes"
	"context"
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
		Slot:   "default",
	})
	if err != nil {
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
	if stored.GameID != "pokemon-emerald-usa" {
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
