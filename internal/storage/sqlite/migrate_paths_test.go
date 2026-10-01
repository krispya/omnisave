package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
	"github.com/krisbaumgartner/omnisave/internal/storage/sqlite/sqlitetest"
)

func markMirrorPathFormat(t *testing.T, databasePath, saveID string) {
	t.Helper()
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE omnisaves SET path_format_version = ? WHERE id = ?`,
		omnisave.PathFormatMirror, saveID); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateLocationsRenamesAWholeLineage(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")
	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	sqlitetest.AddGame(t, repository, "slay-the-spire-2")
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
	markMirrorPathFormat(t, databasePath, save.ID)
	legacy, err := saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.PathFormatVersion != omnisave.PathFormatMirror {
		t.Fatalf("legacy path format = %d", legacy.PathFormatVersion)
	}
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: legacy.CurrentRevisionID,
		Upserts:                   []omnisave.RevisionFile{{Path: "remote/profile.save", Artifact: first}},
	}); !errors.Is(err, omnisave.ErrPathFormatMigrationRequired) {
		t.Fatalf("legacy commit = %v", err)
	}
	if _, err := saves.Restore(ctx, save.ID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: legacy.CurrentRevisionID,
		RevisionID:                older.ID,
	}); !errors.Is(err, omnisave.ErrPathFormatMigrationRequired) {
		t.Fatalf("legacy restore = %v", err)
	}
	if _, err := saves.Fork(ctx, save.ID, omnisave.ForkOmnisave{
		RevisionID: *legacy.CurrentRevisionID,
	}); !errors.Is(err, omnisave.ErrPathFormatMigrationRequired) {
		t.Fatalf("legacy fork = %v", err)
	}

	result, err := saves.MigrateLocations(ctx, save.ID, omnisave.MigrateLocations{
		ExpectedPathFormatVersion: omnisave.PathFormatMirror,
		To:                        "aaaa1111", Prefix: "76561198027955092",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PathFormatVersion != omnisave.PathFormatNative || result.Revisions != 2 || result.Files != 4 {
		t.Fatalf("result = %+v", result)
	}
	auditDB, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	var recorded string
	if err := auditDB.QueryRow(`SELECT path_migrations FROM omnisaves WHERE id = ?`, save.ID).Scan(&recorded); err != nil {
		auditDB.Close()
		t.Fatal(err)
	}
	if err := auditDB.Close(); err != nil {
		t.Fatal(err)
	}
	var facts []omnisave.PathMigration
	if err := json.Unmarshal([]byte(recorded), &facts); err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Kind != omnisave.MigrationKindLocationRename ||
		facts[0].FromVersion != omnisave.PathFormatMirror ||
		facts[0].ToVersion != omnisave.PathFormatNative || facts[0].From != "remote" ||
		facts[0].To != "aaaa1111/76561198027955092" {
		t.Fatalf("recorded migration fact = %+v", facts)
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
	migrated, err := saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.PathFormatVersion != omnisave.PathFormatNative {
		t.Fatalf("reopened path format = %d", migrated.PathFormatVersion)
	}
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
		ExpectedPathFormatVersion: omnisave.PathFormatMirror,
		To:                        "aaaa1111", Prefix: "76561198027955092",
	})
	var refused *omnisave.MigrationRefused
	if !errors.As(err, &refused) || refused.Reason != omnisave.MigrationRefusedVersion {
		t.Fatalf("expected a version refusal, got %v", err)
	}
}

func TestMigrateLocationsRefusesMixedAndForkedLineages(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	repository, err := sqlite.Open(filepath.Join(directory, "omnisave.db"), filepath.Join(directory, "store"))
	if err != nil {
		t.Fatal(err)
	}
	sqlitetest.AddGame(t, repository, "mixed-game", "forked-game")
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
	markMirrorPathFormat(t, filepath.Join(directory, "omnisave.db"), mixed.ID)
	_, err = saves.MigrateLocations(ctx, mixed.ID, omnisave.MigrateLocations{
		ExpectedPathFormatVersion: omnisave.PathFormatMirror, To: "abc",
	})
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
	markMirrorPathFormat(t, filepath.Join(directory, "omnisave.db"), forked.ID)
	markMirrorPathFormat(t, filepath.Join(directory, "omnisave.db"), fork.Omnisave.ID)
	_, err = saves.MigrateLocations(ctx, forked.ID, omnisave.MigrateLocations{
		ExpectedPathFormatVersion: omnisave.PathFormatMirror, To: "abc",
	})
	if !errors.As(err, &refused) || refused.Reason != omnisave.MigrationRefusedForkFamily {
		t.Fatalf("expected a fork-family refusal on the origin, got %v", err)
	}
	_, err = saves.MigrateLocations(ctx, fork.Omnisave.ID, omnisave.MigrateLocations{
		ExpectedPathFormatVersion: omnisave.PathFormatMirror, To: "abc",
	})
	if !errors.As(err, &refused) || refused.Reason != omnisave.MigrationRefusedForkFamily {
		t.Fatalf("expected a fork-family refusal on the fork, got %v", err)
	}

	if _, err := saves.MigrateLocations(ctx, mixed.ID, omnisave.MigrateLocations{
		ExpectedPathFormatVersion: 99, To: "abc",
	}); !errors.Is(err, omnisave.ErrInvalid) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

// To and Prefix are each well-formed names, but the rename composes them
// onto every legacy rest: a composite longer than any commit may reference
// would mint paths the server itself refuses to touch again, so the
// migration is refused whole instead.
func TestMigrateLocationsRefusesOverlongRenames(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	repository, err := sqlite.Open(databasePath, filepath.Join(directory, "store"))
	if err != nil {
		t.Fatal(err)
	}
	sqlitetest.AddGame(t, repository, "long-game")
	defer repository.Close()
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "long-game"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "content")
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "remote/save.dat", Artifact: artifact}},
	}); err != nil {
		t.Fatal(err)
	}
	markMirrorPathFormat(t, databasePath, save.ID)
	_, err = saves.MigrateLocations(ctx, save.ID, omnisave.MigrateLocations{
		ExpectedPathFormatVersion: omnisave.PathFormatMirror,
		To:                        strings.Repeat("a", 1000),
		Prefix:                    strings.Repeat("b", 100),
	})
	var refused *omnisave.MigrationRefused
	if !errors.As(err, &refused) || refused.Reason != omnisave.MigrationRefusedPathLength {
		t.Fatalf("expected a path-length refusal, got %v", err)
	}
}

// The path bound is a byte bound, because that is what a commit's own
// validator counts. A rename measured in characters would let a history of
// multi-byte names cross it and mint paths the next commit would refuse.
func TestMigrateLocationsMeasuresRenamedPathsInBytes(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	repository, err := sqlite.Open(databasePath, filepath.Join(directory, "store"))
	if err != nil {
		t.Fatal(err)
	}
	sqlitetest.AddGame(t, repository, "wide-game")
	defer repository.Close()
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "wide-game"})
	if err != nil {
		t.Fatal(err)
	}
	// Exactly at the bound as committed: 7 prefix bytes plus 339 three-byte
	// characters. Only 346 characters, so a character count has ample room
	// left where a byte count has none.
	name := strings.Repeat("日", 339)
	committed := omnisave.MirrorLocation + "/" + name
	if len(committed) != omnisave.MaxRevisionPathLength {
		t.Fatalf("fixture path is %d bytes, not the bound %d", len(committed), omnisave.MaxRevisionPathLength)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "content")
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: committed, Artifact: artifact}},
	}); err != nil {
		t.Fatal(err)
	}
	markMirrorPathFormat(t, databasePath, save.ID)

	// Seven bytes of location plus a separator for seven bytes of retired
	// prefix: one byte over as bytes, hundreds of characters under as
	// characters.
	_, err = saves.MigrateLocations(ctx, save.ID, omnisave.MigrateLocations{
		ExpectedPathFormatVersion: omnisave.PathFormatMirror,
		To:                        "battery",
	})
	var refused *omnisave.MigrationRefused
	if !errors.As(err, &refused) || refused.Reason != omnisave.MigrationRefusedPathLength {
		t.Fatalf("expected a path-length refusal, got %v", err)
	}

	// One byte shorter lands exactly on the bound, and the lineage migrates.
	result, err := saves.MigrateLocations(ctx, save.ID, omnisave.MigrateLocations{
		ExpectedPathFormatVersion: omnisave.PathFormatMirror,
		To:                        "batter",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 1 || result.PathFormatVersion != omnisave.PathFormatNative {
		t.Fatalf("result = %+v", result)
	}
	revisions, err := saves.ListRevisions(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	renamed := revisions[0].Files[0].Path
	if renamed != "batter/"+name {
		t.Fatalf("renamed path = %q", renamed)
	}
	if len(renamed) > omnisave.MaxRevisionPathLength {
		t.Fatalf("renamed path is %d bytes, past the bound", len(renamed))
	}
}
