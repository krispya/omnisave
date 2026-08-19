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
		ExpectedCurrentRevisionID: &first.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "progress.sav", Artifact: laterProgress}},
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
	if storedSave.CurrentRevisionID == nil || *storedSave.CurrentRevisionID != second.ID {
		t.Fatalf("unexpected current revision: %v", storedSave.CurrentRevisionID)
	}

	third, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &second.ID,
		Deletes:                   []string{"settings.json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Files) != 1 || third.Files[0].Path != "progress.sav" {
		t.Fatalf("delete was not materialized: %v", third.Files)
	}
}

func TestRevisionCanBeNamedWithoutChangingItsSnapshot(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "chrono-trigger"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeBlob(t, ctx, saves, "ocean palace")
	revision, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "chrono.srm", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}

	displayName := "  Before Lavos  "
	renamed, err := saves.UpdateRevision(ctx, save.ID, revision.ID, omnisave.UpdateRevision{
		DisplayName: &displayName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.DisplayName != "Before Lavos" || renamed.ID != revision.ID ||
		len(renamed.Files) != 1 || renamed.Files[0].Artifact.SHA256 != artifact.SHA256 {
		t.Fatalf("rename changed more than the label: %+v", renamed)
	}
}

func TestStaleWriterCannotMoveTheCurrentRevision(t *testing.T) {
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
		ExpectedCurrentRevisionID: &root.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &root.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
	})
	var conflict *omnisave.CurrentRevisionConflict
	if !errors.As(err, &conflict) || conflict.ActualCurrentRevisionID == nil || *conflict.ActualCurrentRevisionID != winner.ID {
		t.Fatalf("expected the winning current revision in the conflict, got %v", err)
	}
	history, err := saves.ListRevisions(ctx, save.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("stale commit must not be visible: %v, %v", history, err)
	}
}

func TestUnnamedSavesAlwaysReceiveNumberedDefaultNames(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	first, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	otherGame, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "chrono-trigger-usa"})
	if err != nil {
		t.Fatal(err)
	}
	if first.DisplayName != "Save 1" || second.DisplayName != "Save 2" || otherGame.DisplayName != "Save 1" {
		t.Fatalf("expected per-game numbered defaults, got %q, %q, %q",
			first.DisplayName, second.DisplayName, otherGame.DisplayName)
	}

	if err := saves.Delete(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	next, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	if next.DisplayName != "Save 2" {
		t.Fatalf("expected numbering to continue from the highest surviving name, got %q", next.DisplayName)
	}
}

func TestASaveNameCanNeverBeCleared(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	created, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	empty := "   "
	if _, err := saves.Update(ctx, created.ID, omnisave.UpdateOmnisave{DisplayName: &empty}); !errors.Is(err, omnisave.ErrInvalid) {
		t.Fatalf("expected clearing a display name to be invalid, got %v", err)
	}
}

func TestUnnamedForksInheritTheSourceNameWithAForkSuffix(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	source, err := saves.Create(ctx, omnisave.CreateOmnisave{
		GameID: "pokemon-emerald-usa", DisplayName: "New Game+",
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
	fork, err := saves.Fork(ctx, source.ID, omnisave.ForkOmnisave{RevisionID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if fork.Omnisave.DisplayName != "New Game+ (fork)" {
		t.Fatalf("expected the fork to be named after its source, got %q", fork.Omnisave.DisplayName)
	}
}

// Repeat divergences from the same Device request the same name every time;
// the server numbers the newcomers so the poster wall can tell them apart.
func TestARequestedNameTheGameAlreadyCarriesIsNumbered(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	source, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
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
	first, err := saves.Fork(ctx, source.ID, omnisave.ForkOmnisave{RevisionID: root.ID, DisplayName: "Steam Deck"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := saves.Fork(ctx, source.ID, omnisave.ForkOmnisave{RevisionID: root.ID, DisplayName: "Steam Deck"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Omnisave.DisplayName != "Steam Deck" || second.Omnisave.DisplayName != "Steam Deck 2" {
		t.Fatalf("expected the repeat to be numbered, got %q and %q",
			first.Omnisave.DisplayName, second.Omnisave.DisplayName)
	}

	// Creates are deconflicted the same way, but only within their game.
	if _, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa", DisplayName: "Steam Deck"}); err != nil {
		t.Fatal(err)
	}
	third, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa", DisplayName: "Steam Deck"})
	if err != nil {
		t.Fatal(err)
	}
	if third.DisplayName != "Steam Deck 4" {
		t.Fatalf("expected the create to take the next free number, got %q", third.DisplayName)
	}
	otherGame, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "chrono-trigger-usa", DisplayName: "Steam Deck"})
	if err != nil {
		t.Fatal(err)
	}
	if otherGame.DisplayName != "Steam Deck" {
		t.Fatalf("expected another game's names to not collide, got %q", otherGame.DisplayName)
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
	if fork.Revision.ID != root.ID || len(fork.Revision.Files) != 1 ||
		fork.Revision.Files[0].Artifact.SHA256 != artifact.SHA256 {
		t.Fatalf("fork should begin at the shared revision node: %v", fork.Revision)
	}

	forkUpdate := storeBlob(t, ctx, saves, "fork progress")
	updatedFork, err := saves.CommitRevision(ctx, fork.Omnisave.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: fork.Omnisave.CurrentRevisionID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: forkUpdate}},
	})
	if err != nil {
		t.Fatal(err)
	}
	storedSource, err := saves.Get(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedSource.CurrentRevisionID == nil || *storedSource.CurrentRevisionID != root.ID ||
		updatedFork.ParentID == nil || *updatedFork.ParentID != fork.Revision.ID {
		t.Fatalf("source and fork should advance independently: source=%v fork=%v", storedSource, updatedFork)
	}
}

func TestRestoreMovesCurrentAndTheNextCommitCreatesABranch(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "chrono-trigger"})
	if err != nil {
		t.Fatal(err)
	}
	rootContent := storeBlob(t, ctx, saves, "millennial fair")
	root, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "chrono.srm", Artifact: rootContent}},
	})
	if err != nil {
		t.Fatal(err)
	}
	laterContent := storeBlob(t, ctx, saves, "ocean palace")
	later, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &root.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "chrono.srm", Artifact: laterContent}},
	})
	if err != nil {
		t.Fatal(err)
	}

	restored, err := saves.Restore(ctx, save.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &later.ID,
		RevisionID:                root.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if restored.CurrentRevisionID == nil || *restored.CurrentRevisionID != root.ID ||
		!restored.CurrentRevisionCreatedAt.Equal(root.CreatedAt) {
		t.Fatalf("restore should select the original node and date: %+v", restored)
	}

	alternateContent := storeBlob(t, ctx, saves, "future changed")
	alternate, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &root.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "chrono.srm", Artifact: alternateContent}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if alternate.ParentID == nil || *alternate.ParentID != root.ID {
		t.Fatalf("the alternate future should branch from the restored node: %+v", alternate)
	}
	jumped, err := saves.Restore(ctx, save.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &alternate.ID,
		RevisionID:                later.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if jumped.CurrentRevisionID == nil || *jumped.CurrentRevisionID != later.ID {
		t.Fatalf("a sibling future should remain directly selectable: %+v", jumped)
	}
	_, err = saves.Restore(ctx, save.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &alternate.ID,
		RevisionID:                root.ID,
	})
	var conflict *omnisave.CurrentRevisionConflict
	if !errors.As(err, &conflict) || conflict.ActualCurrentRevisionID == nil ||
		*conflict.ActualCurrentRevisionID != later.ID {
		t.Fatalf("a stale Device must not move the pointer, got %v", err)
	}
	history, err := saves.ListRevisions(ctx, save.ID)
	if err != nil || len(history) != 3 {
		t.Fatalf("both futures must remain recoverable: history=%+v err=%v", history, err)
	}
}

// A branch commit names its parent separately from the current revision it
// expects, so a Device whose content continues a node a restore moved away
// from attaches there instead of pretending to continue current. The
// concurrency check still guards the pointer (FDR-005, decision 15).
func TestABranchCommitAttachesToItsParentAndStillGuardsCurrent(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "chrono-trigger"})
	if err != nil {
		t.Fatal(err)
	}
	root, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "chrono.srm", Artifact: storeBlob(t, ctx, saves, "millennial fair")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	later, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &root.ID,
		Upserts: []omnisave.RevisionFile{
			{Path: "chrono.srm", Artifact: storeBlob(t, ctx, saves, "ocean palace")},
			{Path: "chrono.rtc", Artifact: storeBlob(t, ctx, saves, "clock")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saves.Restore(ctx, save.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &later.ID,
		RevisionID:                root.ID,
	}); err != nil {
		t.Fatal(err)
	}

	// Current sits at root; the commit continues later, and its delete covers
	// a path only later carries — proof the parent supplied the base file set.
	branch, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &root.ID,
		ParentRevisionID:          &later.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "chrono.srm", Artifact: storeBlob(t, ctx, saves, "black omen")}},
		Deletes:                   []string{"chrono.rtc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if branch.ParentID == nil || *branch.ParentID != later.ID {
		t.Fatalf("the branch commit should attach to its parent, got %v", branch.ParentID)
	}
	if len(branch.Files) != 1 || branch.Files[0].Path != "chrono.srm" {
		t.Fatalf("the branch commit should build on its parent's files, got %+v", branch.Files)
	}
	stored, err := saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CurrentRevisionID == nil || *stored.CurrentRevisionID != branch.ID {
		t.Fatalf("a branch commit should still move current onto itself, got %v", stored.CurrentRevisionID)
	}

	// Naming a parent does not buy a way past the pointer check.
	_, err = saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &root.ID,
		ParentRevisionID:          &later.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "chrono.srm", Artifact: storeBlob(t, ctx, saves, "lavos")}},
	})
	var conflict *omnisave.CurrentRevisionConflict
	if !errors.As(err, &conflict) || conflict.ActualCurrentRevisionID == nil ||
		*conflict.ActualCurrentRevisionID != branch.ID {
		t.Fatalf("a stale branch commit must be refused with the actual current revision, got %v", err)
	}

	// A parent outside this Omnisave is not a way to reach another lineage.
	other, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "chrono-trigger"})
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := saves.CommitRevision(ctx, other.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "chrono.srm", Artifact: storeBlob(t, ctx, saves, "elsewhere")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &branch.ID,
		ParentRevisionID:          &stranger.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "chrono.srm", Artifact: storeBlob(t, ctx, saves, "lavos")}},
	})
	if !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("a parent from another Omnisave should not resolve, got %v", err)
	}
	blank := ""
	_, err = saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &branch.ID,
		ParentRevisionID:          &blank,
		Upserts:                   []omnisave.RevisionFile{{Path: "chrono.srm", Artifact: storeBlob(t, ctx, saves, "lavos")}},
	})
	if !errors.Is(err, omnisave.ErrInvalid) {
		t.Fatalf("an empty parent should be refused, got %v", err)
	}
}

func TestAForkKeepsItsForkPointAfterRewindingBelowIt(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	source, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "pokemon-emerald-usa"})
	if err != nil {
		t.Fatal(err)
	}
	root, err := saves.CommitRevision(ctx, source.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeBlob(t, ctx, saves, "start")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mid, err := saves.CommitRevision(ctx, source.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &root.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeBlob(t, ctx, saves, "midgame")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tip, err := saves.CommitRevision(ctx, source.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &mid.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeBlob(t, ctx, saves, "endgame")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fork, err := saves.Fork(ctx, source.ID, omnisave.ForkOmnisave{RevisionID: mid.ID})
	if err != nil {
		t.Fatal(err)
	}

	rewound, err := saves.Restore(ctx, fork.Omnisave.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &mid.ID,
		RevisionID:                root.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The fork was started at mid, so mid stays in its tree wherever current sits.
	history, err := saves.ListRevisions(ctx, fork.Omnisave.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("the rewound fork should still see its fork point: history=%+v err=%v", history, err)
	}
	returned, err := saves.Restore(ctx, fork.Omnisave.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: rewound.CurrentRevisionID,
		RevisionID:                mid.ID,
	})
	if err != nil || returned.CurrentRevisionID == nil || *returned.CurrentRevisionID != mid.ID {
		t.Fatalf("the fork should fast-forward back to its fork point: save=%+v err=%v", returned, err)
	}
	// The source's later progress was never shared with the fork.
	_, err = saves.Restore(ctx, fork.Omnisave.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &mid.ID,
		RevisionID:                tip.ID,
	})
	if !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("a source-only descendant must stay outside the fork's tree, got %v", err)
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
		ExpectedCurrentRevisionID: &root.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: artifact}},
		Deletes:                   []string{"save.dat"},
	})
	if !errors.Is(err, omnisave.ErrInvalid) {
		t.Fatalf("a path cannot be updated and deleted together, got %v", err)
	}
}

// notingNamer answers every revision with one fixed name and remembers which
// game it was asked about, standing in for a game's labeler.
type notingNamer struct {
	name    string
	gameIDs []string
}

func (n *notingNamer) NameRevision(_ context.Context, gameID string, _ []omnisave.RevisionFile) string {
	n.gameIDs = append(n.gameIDs, gameID)
	return n.name
}

func TestCommittedRevisionsAreNamedByTheGamesLabeler(t *testing.T) {
	ctx := context.Background()
	namer := &notingNamer{name: "Necro A5, Underdocks flr 12"}
	saves := omnisaveservice.NewWithNamer(storagetest.NewMemoryRepository(), namer)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-spire2"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeBlob(t, ctx, saves, "run in progress")
	revision, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "remote/current_run.save", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision.DisplayName != "Necro A5, Underdocks flr 12" ||
		revision.NameSource != omnisave.NameSourceLabeler {
		t.Fatalf("commit did not carry the labeler's name: %+v", revision)
	}
	if len(namer.gameIDs) != 1 || namer.gameIDs[0] != "game-spire2" {
		t.Fatalf("the namer was asked about %v, want the save's game", namer.gameIDs)
	}

	// A person's rename outranks the labeler's: the name changes hands and the
	// source records that it is now manual.
	displayName := "The run that beat the Spire"
	renamed, err := saves.UpdateRevision(ctx, save.ID, revision.ID, omnisave.UpdateRevision{
		DisplayName: &displayName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.DisplayName != displayName || renamed.NameSource != omnisave.NameSourceManual {
		t.Fatalf("rename did not take the name over: %+v", renamed)
	}

	// A labeler with nothing to say leaves the revision unnamed.
	namer.name = "   "
	second, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &revision.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "remote/history/1.run", Artifact: artifact}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.DisplayName != "" || second.NameSource != "" {
		t.Fatalf("a declined name still landed on the revision: %+v", second)
	}
}

// A divergence jump preserves unsynced progress as a branch it immediately
// leaves: the commit attaches to its parent, names itself after the device,
// and the current pointer never moves.
func TestAKeepCurrentCommitAttachesABranchWithoutMovingCurrent(t *testing.T) {
	ctx := context.Background()
	namer := &notingNamer{name: "labeler name"}
	saves := omnisaveservice.NewWithNamer(storagetest.NewMemoryRepository(), namer)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1"})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeBlob(t, ctx, saves, "baseline")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &baseline.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeBlob(t, ctx, saves, "another device")}},
	})
	if err != nil {
		t.Fatal(err)
	}

	kept, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &current.ID,
		ParentRevisionID:          &baseline.ID,
		KeepCurrent:               true,
		DisplayName:               "Steam Deck",
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeBlob(t, ctx, saves, "shelved progress")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if kept.ParentID == nil || *kept.ParentID != baseline.ID {
		t.Fatalf("expected the branch to attach to the baseline, got %+v", kept.ParentID)
	}
	// The supplied name records the user's answer, so it outranks the labeler
	// and is never automation's to replace.
	if kept.DisplayName != "Steam Deck" || kept.NameSource != omnisave.NameSourceManual {
		t.Fatalf("expected the device's name to take the revision over, got %+v", kept)
	}
	after, err := saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.CurrentRevisionID == nil || *after.CurrentRevisionID != current.ID {
		t.Fatalf("expected the current pointer to stay at %s, got %v", current.ID, after.CurrentRevisionID)
	}

	// The pointer is still guarded: a keep-current commit with a stale
	// expectation is refused like any other.
	_, err = saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &baseline.ID,
		ParentRevisionID:          &baseline.ID,
		KeepCurrent:               true,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: storeBlob(t, ctx, saves, "stale writer")}},
	})
	var conflict *omnisave.CurrentRevisionConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("expected a current revision conflict, got %v", err)
	}
}

func TestDeleteRevisionPrunesOnlyUnneededTips(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1"})
	if err != nil {
		t.Fatal(err)
	}
	kept := storeBlob(t, ctx, saves, "kept content")
	pruned := storeBlob(t, ctx, saves, "pruned content")
	first, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "save.dat", Artifact: kept}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &first.ID,
		Upserts:                   []omnisave.RevisionFile{{Path: "save.dat", Artifact: pruned}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reason := func(err error) string {
		t.Helper()
		var inUse *omnisave.RevisionInUse
		if !errors.As(err, &inUse) {
			t.Fatalf("expected a revision-in-use refusal, got %v", err)
		}
		return inUse.Reason
	}
	if got := reason(saves.DeleteRevision(ctx, save.ID, second.ID)); got != omnisave.RevisionInUseCurrent {
		t.Fatalf("deleting the current revision should refuse as current, got %q", got)
	}
	if got := reason(saves.DeleteRevision(ctx, save.ID, first.ID)); got != omnisave.RevisionInUseChildren {
		t.Fatalf("deleting a parent should refuse for its children, got %q", got)
	}
	if err := saves.DeleteRevision(ctx, save.ID, ""); !errors.Is(err, omnisave.ErrInvalid) {
		t.Fatalf("an empty revision id should be invalid, got %v", err)
	}
	if err := saves.DeleteRevision(ctx, save.ID, "does-not-exist"); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("an unknown revision should be not found, got %v", err)
	}

	if _, err := saves.Restore(ctx, save.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: &second.ID,
		RevisionID:                first.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := saves.DeleteRevision(ctx, save.ID, second.ID); err != nil {
		t.Fatalf("an unneeded tip should delete, got %v", err)
	}
	history, err := saves.ListRevisions(ctx, save.ID)
	if err != nil || len(history) != 1 || history[0].ID != first.ID {
		t.Fatalf("expected only the kept revision: history=%+v err=%v", history, err)
	}
	if _, err := saves.StatArtifact(ctx, pruned.SHA256); !errors.Is(err, omnisave.ErrNotFound) {
		t.Fatalf("content only the deleted tip referenced should be gone, got %v", err)
	}
	if _, err := saves.StatArtifact(ctx, kept.SHA256); err != nil {
		t.Fatalf("content the kept revision references should remain: %v", err)
	}
}

func TestDeleteRevisionRefusesAForkOrigin(t *testing.T) {
	ctx := context.Background()
	saves := omnisaveservice.New(storagetest.NewMemoryRepository())
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "game-1"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeBlob(t, ctx, saves, "contents")
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

	err = saves.DeleteRevision(ctx, save.ID, second.ID)
	var inUse *omnisave.RevisionInUse
	if !errors.As(err, &inUse) || inUse.Reason != omnisave.RevisionInUseForkOrigin {
		t.Fatalf("deleting a fork's origin should refuse as fork origin, got %v", err)
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
