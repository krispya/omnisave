package service_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
)

func TestCreateOmniSave(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())

	created, err := saves.Create(ctx, omnisave.CreateOmniSave{
		GameID: "pokemon-emerald-usa",
		Slot:   "default",
		Metadata: map[string]string{
			"label": "My Pokémon save",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	stored, err := saves.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.GameID != "pokemon-emerald-usa" || stored.Slot != "default" {
		t.Fatalf("unexpected save: %v", stored)
	}
	if stored.Metadata["label"] != "My Pokémon save" {
		t.Fatalf("unexpected metadata: %v", stored.Metadata)
	}
}

func TestListOmniSaves(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())

	created, err := saves.Create(ctx, omnisave.CreateOmniSave{
		GameID: "pokemon-emerald-usa",
		Slot:   "default",
	})
	if err != nil {
		t.Fatal(err)
	}

	list, err := saves.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("expected the created save, got %v", list)
	}
}

func TestAddAndReadRevision(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())

	save, err := saves.Create(ctx, omnisave.CreateOmniSave{GameID: "pokemon-emerald-usa", Slot: "default"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := saves.AddRevision(ctx, save.ID, omnisave.CreateRevision{
		Format: "application/vnd.omnisave.raw-save.v1",
	}, bytes.NewReader([]byte("game-save contents")))
	if err != nil {
		t.Fatal(err)
	}

	stored, err := saves.GetRevision(ctx, save.ID, revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.OmniSaveID != save.ID || stored.Artifact.Size != int64(len("game-save contents")) {
		t.Fatalf("unexpected revision: %v", stored)
	}
}

func TestReadRevisionHistory(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())

	save, err := saves.Create(ctx, omnisave.CreateOmniSave{GameID: "pokemon-emerald-usa", Slot: "default"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := saves.AddRevision(ctx, save.ID, omnisave.CreateRevision{
		Format: "application/vnd.omnisave.raw-save.v1",
	}, bytes.NewReader([]byte("first game-save contents")))
	if err != nil {
		t.Fatal(err)
	}
	second, err := saves.AddRevision(ctx, save.ID, omnisave.CreateRevision{
		ParentIDs: []string{first.ID},
		Format:    "application/vnd.omnisave.raw-save.v1",
	}, bytes.NewReader([]byte("later game-save contents")))
	if err != nil {
		t.Fatal(err)
	}

	history, err := saves.ListRevisions(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || !slices.Equal(second.ParentIDs, []string{first.ID}) {
		t.Fatalf("unexpected history: %v", history)
	}
}

func TestDeleteOnlyLeafRevision(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())

	save, err := saves.Create(ctx, omnisave.CreateOmniSave{GameID: "pokemon-emerald-usa", Slot: "default"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := saves.AddRevision(ctx, save.ID, omnisave.CreateRevision{
		Format: "application/vnd.omnisave.raw-save.v1",
	}, bytes.NewReader([]byte("first game-save contents")))
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := saves.AddRevision(ctx, save.ID, omnisave.CreateRevision{
		ParentIDs: []string{parent.ID},
		Format:    "application/vnd.omnisave.raw-save.v1",
	}, bytes.NewReader([]byte("later game-save contents")))
	if err != nil {
		t.Fatal(err)
	}

	if err := saves.DeleteRevision(ctx, save.ID, parent.ID); !errors.Is(err, omnisave.ErrInUse) {
		t.Fatalf("revision with a child should remain, got %v", err)
	}
	if err := saves.DeleteRevision(ctx, save.ID, leaf.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := saves.GetRevision(ctx, save.ID, leaf.ID); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("deleted revision should not be found, got %v", err)
	}
}

func TestReadArtifact(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())

	save, err := saves.Create(ctx, omnisave.CreateOmniSave{GameID: "pokemon-emerald-usa", Slot: "default"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := saves.AddRevision(ctx, save.ID, omnisave.CreateRevision{
		Format: "application/vnd.omnisave.raw-save.v1",
	}, bytes.NewReader([]byte("game-save contents")))
	if err != nil {
		t.Fatal(err)
	}

	payload, err := saves.OpenArtifact(ctx, revision.Artifact.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer payload.Close()
	got, err := io.ReadAll(payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "game-save contents" {
		t.Fatalf("unexpected artifact contents: %q", got)
	}
}
