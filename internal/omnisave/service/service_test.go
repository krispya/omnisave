package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
)

func TestOmnisaveRecordCanBeCreatedListedAndRenamed(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	created, err := saves.Create(ctx, omnisave.CreateOmnisave{
		GameID:   "pokemon-emerald-usa",
		Metadata: map[string]string{"label": "My Pokémon save"},
	})
	if err != nil {
		t.Fatal(err)
	}
	displayName := "  Before the final boss  "
	updated, err := saves.Update(ctx, created.ID, omnisave.UpdateOmnisave{DisplayName: &displayName})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := saves.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || updated.DisplayName != "Before the final boss" ||
		listed[0].Metadata["label"] != "My Pokémon save" {
		t.Fatalf("unexpected save: updated=%v listed=%v", updated, listed)
	}
}

func TestPartialUpdatesMaterializeCompleteSnapshots(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	progress := storeBlob(t, ctx, saves, "first game-save contents")
	settings := storeBlob(t, ctx, saves, "shared settings")
	first, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{
			{Path: "progress.sav", Artifact: progress},
			{Path: "settings.json", Artifact: settings},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	laterProgress := storeBlob(t, ctx, saves, "later game-save contents")
	second, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedHeadID: &first.ID,
		Upserts:        []omnisave.RevisionFile{{Path: "progress.sav", Artifact: laterProgress}},
	})
	if err != nil {
		t.Fatal(err)
	}
	storedSave, err := saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ParentID == nil || *second.ParentID != first.ID || len(second.Files) != 2 ||
		second.Files[1].Artifact.SHA256 != settings.SHA256 {
		t.Fatalf("partial update did not retain a complete snapshot: %v", second)
	}
	if storedSave.HeadRevisionID == nil || *storedSave.HeadRevisionID != second.ID {
		t.Fatalf("unexpected head: %v", storedSave.HeadRevisionID)
	}

	third, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedHeadID: &second.ID,
		Deletes:        []string{"settings.json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Files) != 1 || third.Files[0].Path != "progress.sav" {
		t.Fatalf("delete was not materialized: %v", third.Files)
	}
}

func TestStaleWriterCannotMoveTheSaveHead(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeBlob(t, ctx, saves, "snapshot")
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
	_, err = saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedHeadID: &root.ID,
		Upserts:        []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	var conflict *omnisave.HeadConflict
	if !errors.As(err, &conflict) || conflict.ActualHeadID == nil || *conflict.ActualHeadID != winner.ID {
		t.Fatalf("expected the winning head in the conflict, got %v", err)
	}
	history, err := saves.ListRevisions(ctx, save.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("stale commit must not be visible: %v, %v", history, err)
	}
}

func TestForkCreatesAnotherSelectableSaveWithItsOwnHistory(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	source, err := saves.Create(ctx, omnisave.CreateOmnisave{
		GameID: "pokemon-emerald-usa", Metadata: map[string]string{"platform": "gba"},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeBlob(t, ctx, saves, "snapshot")
	root, err := saves.CommitRevision(ctx, source.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fork, err := saves.Fork(ctx, source.ID, omnisave.ForkOmnisave{
		RevisionID: root.ID, DisplayName: "Alternate route",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fork.Omnisave.ForkedFrom == nil || fork.Omnisave.ForkedFrom.OmnisaveID != source.ID ||
		fork.Omnisave.ForkedFrom.RevisionID != root.ID || fork.Omnisave.Metadata["platform"] != "gba" {
		t.Fatalf("unexpected fork lineage: %v", fork.Omnisave)
	}
	if fork.Revision.ParentID != nil || len(fork.Revision.Files) != 1 ||
		fork.Revision.Files[0].Artifact.SHA256 != artifact.SHA256 {
		t.Fatalf("fork should begin with a copied root snapshot: %v", fork.Revision)
	}

	forkUpdate := storeBlob(t, ctx, saves, "fork progress")
	updatedFork, err := saves.CommitRevision(ctx, fork.Omnisave.ID, omnisave.CreateRevision{
		ExpectedHeadID: fork.Omnisave.HeadRevisionID,
		Upserts:        []omnisave.RevisionFile{{Path: "save.dat", Artifact: forkUpdate}},
	})
	if err != nil {
		t.Fatal(err)
	}
	storedSource, err := saves.Get(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedSource.HeadRevisionID == nil || *storedSource.HeadRevisionID != root.ID ||
		updatedFork.ParentID == nil || *updatedFork.ParentID != fork.Revision.ID {
		t.Fatalf("source and fork should advance independently: source=%v fork=%v", storedSource, updatedFork)
	}
}

func TestRejectInvalidChangesAndReportMissingArtifacts(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	missing := omnisave.Artifact{
		Format: "application/octet-stream",
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Size:   10,
	}
	_, err = saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: missing}},
	})
	var missingError *omnisave.MissingArtifacts
	if !errors.As(err, &missingError) || !slices.Equal(missingError.SHA256, []string{missing.SHA256}) {
		t.Fatalf("expected the missing hash, got %v", err)
	}
	artifact := storeBlob(t, ctx, saves, "snapshot")
	_, err = saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "../save.dat", Artifact: artifact}},
	})
	if !errors.Is(err, omnisave.ErrInvalid) {
		t.Fatalf("unsafe paths must be rejected, got %v", err)
	}
	_, err = saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{
			{Path: "save.dat", Artifact: artifact},
			{Path: "save.dat", Artifact: artifact},
		},
	})
	if !errors.Is(err, omnisave.ErrInvalid) {
		t.Fatalf("duplicate paths must be rejected, got %v", err)
	}
	root, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedHeadID: &root.ID,
		Upserts:        []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
		Deletes:        []string{"save.dat"},
	})
	if !errors.Is(err, omnisave.ErrInvalid) {
		t.Fatalf("a path cannot be updated and deleted together, got %v", err)
	}
}

func storeBlob(t *testing.T, ctx context.Context, saves omnisave.Service, contents string) omnisave.Artifact {
	t.Helper()
	sum := sha256.Sum256([]byte(contents))
	artifact := omnisave.Artifact{
		Format: "application/octet-stream",
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(contents)),
	}
	if err := saves.StoreArtifact(ctx, artifact, bytes.NewReader([]byte(contents))); err != nil {
		t.Fatal(err)
	}
	return artifact
}
