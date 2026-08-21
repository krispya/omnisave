package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
)

func TestMigrateLocationsRenamesAWholeLineage(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")
	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "slay-the-spire-2"})
	if err != nil {
		t.Fatal(err)
	}
	first := storeOmnisaveArtifact(t, ctx, saves, "mid-run")
	second := storeOmnisaveArtifact(t, ctx, saves, "later-run")
	older, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{
			{Path: "remote/profile.save", Artifact: first},
			{Path: "remote/profile1/saves/current_run.save", Artifact: first},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := older.ID
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &expected,
		Upserts: []omnisave.RevisionFile{
			{Path: "remote/profile.save", Artifact: second},
			{Path: "remote/profile1/saves/current_run.save", Artifact: second},
		},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := saves.MigrateLocations(ctx, save.ID, omnisave.MigrateLocations{
		From: "remote", To: "aaaa1111", Prefix: "76561198027955092",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Revisions != 2 || result.Files != 4 {
		t.Fatalf("result = %+v", result)
	}

	// A rename must survive the projection and a restart, keep identities,
	// and leave nothing speaking the old vocabulary.
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
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].ID != older.ID {
		t.Fatalf("history = %+v", history)
	}
	for _, revision := range history {
		for _, file := range revision.Files {
			if !strings.HasPrefix(file.Path, "aaaa1111/76561198027955092/profile") {
				t.Fatalf("path = %q", file.Path)
			}
		}
	}

	// Running again finds nothing left to rename.
	_, err = saves.MigrateLocations(ctx, save.ID, omnisave.MigrateLocations{
		From: "remote", To: "aaaa1111", Prefix: "76561198027955092",
	})
	var refused *omnisave.MigrationRefused
	if !errors.As(err, &refused) || refused.Reason != omnisave.MigrationRefusedEmpty {
		t.Fatalf("expected an empty refusal, got %v", err)
	}
}

func TestMigrateLocationsRefusesMixedAndForkedLineages(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	repository, err := sqlite.Open(filepath.Join(directory, "omnisave.db"), filepath.Join(directory, "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves := omnisaveservice.New(repository)

	mixed, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "mixed-game"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "content")
	if _, err := saves.CommitRevision(ctx, mixed.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{
			{Path: "remote/save.dat", Artifact: artifact},
			{Path: "somewhere/else.dat", Artifact: artifact},
		},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = saves.MigrateLocations(ctx, mixed.ID, omnisave.MigrateLocations{From: "remote", To: "abc"})
	var refused *omnisave.MigrationRefused
	if !errors.As(err, &refused) || refused.Reason != omnisave.MigrationRefusedMixed {
		t.Fatalf("expected a mixed refusal, got %v", err)
	}

	forked, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "forked-game"})
	if err != nil {
		t.Fatal(err)
	}
	origin, err := saves.CommitRevision(ctx, forked.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "remote/save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fork, err := saves.Fork(ctx, forked.ID, omnisave.ForkOmnisave{
		RevisionID: origin.ID, DisplayName: "Fork",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = saves.MigrateLocations(ctx, forked.ID, omnisave.MigrateLocations{From: "remote", To: "abc"})
	if !errors.As(err, &refused) || refused.Reason != omnisave.MigrationRefusedForkFamily {
		t.Fatalf("expected a fork-family refusal on the origin, got %v", err)
	}
	_, err = saves.MigrateLocations(ctx, fork.Omnisave.ID, omnisave.MigrateLocations{From: "remote", To: "abc"})
	if !errors.As(err, &refused) || refused.Reason != omnisave.MigrationRefusedForkFamily {
		t.Fatalf("expected a fork-family refusal on the fork, got %v", err)
	}

	if _, err := saves.MigrateLocations(ctx, mixed.ID, omnisave.MigrateLocations{From: "bad/name", To: "abc"}); !errors.Is(err, omnisave.ErrInvalid) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}
