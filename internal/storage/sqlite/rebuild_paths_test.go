package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
	"github.com/krisbaumgartner/omnisave/internal/storage/sqlite/sqlitetest"
)

func removeDatabase(t *testing.T, databasePath string) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(databasePath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
}

// A location migration is a durable fact, not a database row: the portable
// store's snapshot manifests are immutable and keep the retired vocabulary
// forever, so losing the database and rebuilding from the store must replay
// the recorded rename to land back on the vocabulary the lineage reached.
func TestALocationMigrationSurvivesDatabaseLoss(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")
	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	sqlitetest.AddGame(t, repository, "chrono-trigger")
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "chrono-trigger"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "battery-content")
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "remote/Chrono Trigger.srm", Artifact: artifact}},
	}); err != nil {
		t.Fatal(err)
	}
	markMirrorPathFormat(t, databasePath, save.ID)
	if _, err := saves.MigrateLocations(ctx, save.ID, omnisave.MigrateLocations{
		ExpectedPathFormatVersion: omnisave.PathFormatMirror, To: "battery",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	removeDatabase(t, databasePath)
	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves = omnisaveservice.New(repository)
	rebuilt, err := saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.PathFormatVersion != omnisave.PathFormatNative {
		t.Fatalf("rebuilt path format = %d", rebuilt.PathFormatVersion)
	}
	history, err := saves.ListRevisions(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, revision := range history {
		for _, file := range revision.Files {
			if !strings.HasPrefix(file.Path, "battery/") {
				t.Fatalf("recovered path still speaks the retired vocabulary: %q", file.Path)
			}
		}
	}
	// The rebuilt lineage is fully alive: ordinary commits are not held.
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: rebuilt.CurrentRevisionID,
		Upserts:                   []omnisave.RevisionFile{{Path: "battery/Chrono Trigger.srm", Artifact: artifact}},
	}); err != nil {
		t.Fatal(err)
	}
}

// The path-format version describes the history, so recovery derives it
// from the paths themselves: a record claiming native over a history still
// speaking the mirror vocabulary is reclassified and held, and the ordinary
// migration then unlocks it.
func TestRecoveryClassifiesLineagesFromTheirHistory(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")
	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	sqlitetest.AddGame(t, repository, "chrono-trigger")
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "chrono-trigger"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := storeOmnisaveArtifact(t, ctx, saves, "mirror-era-content")
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		Upserts: []omnisave.RevisionFile{{Path: "remote/save.dat", Artifact: artifact}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	// The record projected before the version was marked claims native; the
	// manifests speak the mirror. Rebuilding must believe the paths.
	removeDatabase(t, databasePath)
	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves = omnisaveservice.New(repository)
	rebuilt, err := saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.PathFormatVersion != omnisave.PathFormatMirror {
		t.Fatalf("rebuilt path format = %d, want mirror", rebuilt.PathFormatVersion)
	}
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: rebuilt.CurrentRevisionID,
		Upserts:                   []omnisave.RevisionFile{{Path: "remote/save.dat", Artifact: artifact}},
	}); !errors.Is(err, omnisave.ErrPathFormatMigrationRequired) {
		t.Fatalf("reclassified lineage accepted a commit: %v", err)
	}
	result, err := saves.MigrateLocations(ctx, save.ID, omnisave.MigrateLocations{
		ExpectedPathFormatVersion: omnisave.PathFormatMirror, To: "battery",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PathFormatVersion != omnisave.PathFormatNative {
		t.Fatalf("migration result = %+v", result)
	}
}

// Recovery can derive a version only after it has replayed every recorded
// transition. A conflicting or future fact therefore holds this lineage
// instead of letting its already-native-looking paths conceal the gap.
func TestRecoveryHoldsAnUnreplayableMigrationFact(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")
	storeDir := filepath.Join(directory, "store")
	repository, err := sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	sqlitetest.AddGame(t, repository, "future-game")
	saves := omnisaveservice.New(repository)
	save, err := saves.Create(ctx, omnisave.CreateOmnisave{GameID: "future-game"})
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
	if _, err := saves.MigrateLocations(ctx, save.ID, omnisave.MigrateLocations{
		ExpectedPathFormatVersion: omnisave.PathFormatMirror, To: "native-location",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	unknown := []omnisave.PathMigration{{
		Kind:        omnisave.MigrationKindLocationRename,
		FromVersion: omnisave.PathFormatMirror,
		ToVersion:   99,
		From:        omnisave.MirrorLocation,
		To:          "native-location",
	}}
	encoded, err := json.Marshal(unknown)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE omnisaves SET path_migrations = ? WHERE id = ?`,
		string(encoded), save.ID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err = sqlite.Open(databasePath, storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	saves = omnisaveservice.New(repository)
	held, err := saves.Get(ctx, save.ID)
	if err != nil {
		t.Fatal(err)
	}
	if held.PathFormatVersion != omnisave.PathFormatUnclassified {
		t.Fatalf("path format = %d, want unclassified", held.PathFormatVersion)
	}
	if _, err := saves.CommitRevision(ctx, save.ID, omnisave.CreateRevision{
		ExpectedCurrentRevisionID: held.CurrentRevisionID,
		Upserts:                   []omnisave.RevisionFile{{Path: "native-location/save.dat", Artifact: artifact}},
	}); !errors.Is(err, omnisave.ErrPathFormatMigrationRequired) {
		t.Fatalf("held lineage accepted a commit: %v", err)
	}
}
