package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

// Achievement marks are lineage state, like revision names: they live beside
// the snapshots rather than inside them, so the portable store has to carry
// them and a rebuilt database has to get them back.

func TestAchievementMarksSurviveLosingTheDatabase(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")

	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-firewatch", DisplayName: "Summer 1989"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "day one")
	revision, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	unlockedAt := revision.CreatedAt.Add(-time.Hour).Truncate(time.Second)
	if _, err := saves.RecordAchievements(ctx, save.ID, []omnisave.AchievementUnlock{{
		ID: "VG_DAY1", Name: "Good first day.", Description: "Completed Day 1", UnlockedAt: unlockedAt,
	}}); err != nil {
		t.Fatal(err)
	}

	// The mark is in the lineage record, where a person reading the store
	// during a recovery can see which snapshot carries which achievement.
	portable, err := store.Open(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	record, err := portable.GetOmnisave(save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Achievements) != 1 || record.Achievements[0].RevisionID != revision.ID ||
		record.Achievements[0].Name != "Good first day." {
		t.Fatalf("expected the mark in the lineage record, got %+v", record.Achievements)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// The loss.
	for _, leftover := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		if err := os.Remove(leftover); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	rebuilt, err := omnisaveservice.New(repository).ListAchievements(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rebuilt) != 1 || rebuilt[0].ID != "VG_DAY1" || rebuilt[0].Description != "Completed Day 1" {
		t.Fatalf("expected the mark back, got %+v", rebuilt)
	}
	if rebuilt[0].RevisionID == nil || *rebuilt[0].RevisionID != revision.ID {
		t.Fatalf("expected the mark still on its revision, got %v", rebuilt[0].RevisionID)
	}
	if !rebuilt[0].UnlockedAt.Equal(unlockedAt) {
		t.Fatalf("expected the unlock time back, got %s", rebuilt[0].UnlockedAt)
	}
}

func TestDeletingARevisionLeavesItsMarksWaitingForTheNextCommit(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	repository, err := sqlite.Open(filepath.Join(directory, "omnisave.db"), filepath.Join(directory, "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-firewatch"})
	if err != nil {
		t.Fatal(err)
	}
	commit := func(payload string, parent *string) *omnisave.Revision {
		t.Helper()
		revision, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
			ExpectedCurrentRevisionID: parent,
			Upserts: []omnisave.RevisionFile{
				{Path: "save.dat", Artifact: storeOmnisaveArtifact(t, ctx, saves, payload)},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return revision
	}
	first := commit("day one", nil)
	tip := commit("day two", &first.ID)
	// Placed on the tip directly: which revision an unlock lands on is the
	// service's rule and is covered there, while this story is about what
	// happens to a mark once its revision is gone.
	if _, err := repository.RecordAchievements(ctx, save.ID, []omnisave.Achievement{{
		AchievementUnlock: omnisave.AchievementUnlock{
			ID: "VG_DAY2", Name: "Back to work.", UnlockedAt: tip.CreatedAt.Truncate(time.Second),
		},
		RevisionID: &tip.ID,
	}}); err != nil {
		t.Fatal(err)
	}

	// Restoring off the tip and deleting it takes away the snapshot the mark
	// was placed on. The achievement itself is still earned, so it goes back
	// to waiting rather than disappearing with the node.
	if _, err := saves.Restore(ctx, save.ID, omnisave.RestoreRevision{
		RevisionID: first.ID, ExpectedCurrentRevisionID: &tip.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := saves.DeleteRevision(ctx, save.ID, tip.ID); err != nil {
		t.Fatal(err)
	}
	waiting, err := saves.ListAchievements(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 || waiting[0].RevisionID != nil {
		t.Fatalf("expected the mark to outlive its revision, got %+v", waiting)
	}

	replacement := commit("day two again", &first.ID)
	claimed, err := saves.ListAchievements(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].RevisionID == nil || *claimed[0].RevisionID != replacement.ID {
		t.Fatalf("expected the next commit to claim the mark, got %+v", claimed)
	}
}
