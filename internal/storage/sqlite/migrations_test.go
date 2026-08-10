package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/storage"
)

// The shared-ancestry migration turns head_revision_id into the movable Current
// Revision, and adopts forks made before shared ancestry: such a fork owns a
// copied root revision — parent none — alongside its recorded fork point, so
// under membership-by-ancestry its graph would show two roots. The migration
// re-parents the copied root onto the fork point, leaving one connected chain
// with the fork point's ancestry above it.
func TestMigrationGraftsLegacyCopiedRootForksOntoTheirForkPoint(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "omnisave.db")

	// Build the database a pre-graft deployment leaves behind: every migration
	// up to the graft applied, and a linear save beside a fork that copied its
	// root, as forks then did. The graft is found by its content rather than
	// its position so migrations appended later do not move the story's start.
	graft := -1
	for index, migration := range migrations {
		if strings.Contains(migration, "forked_from_revision_id <> revisions.id") {
			graft = index
			break
		}
	}
	if graft < 0 {
		t.Fatal("the graft migration is gone; this story needs rewriting")
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	for index, migration := range migrations[:graft] {
		if _, err := db.Exec(migration); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, index+1); err != nil {
			t.Fatal(err)
		}
	}

	began := time.Now().UTC().Add(-time.Hour)
	stamp := func(minutes int) string {
		return began.Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339Nano)
	}
	for _, statement := range []struct {
		query     string
		arguments []any
	}{
		{`INSERT INTO omnisaves(id, game_id, display_name, head_revision_id,
			forked_from_omnisave_id, forked_from_revision_id, created_at, metadata)
			VALUES (?, ?, ?, ?, NULL, NULL, ?, '{}')`,
			[]any{"source", "game-1", "Main run", "r2", stamp(0)}},
		{`INSERT INTO revisions(id, omnisave_id, parent_id, created_at, metadata)
			VALUES (?, ?, NULL, ?, '{}')`, []any{"r1", "source", stamp(1)}},
		{`INSERT INTO revisions(id, omnisave_id, parent_id, created_at, metadata)
			VALUES (?, ?, ?, ?, '{}')`, []any{"r2", "source", "r1", stamp(2)}},
		{`INSERT INTO omnisaves(id, game_id, display_name, head_revision_id,
			forked_from_omnisave_id, forked_from_revision_id, created_at, metadata)
			VALUES (?, ?, ?, ?, ?, ?, ?, '{}')`,
			[]any{"fork", "game-1", "Retry", "c2", "source", "r1", stamp(3)}},
		{`INSERT INTO revisions(id, omnisave_id, parent_id, created_at, metadata)
			VALUES (?, ?, NULL, ?, '{}')`, []any{"c1", "fork", stamp(3)}},
		{`INSERT INTO revisions(id, omnisave_id, parent_id, created_at, metadata)
			VALUES (?, ?, ?, ?, '{}')`, []any{"c2", "fork", "c1", stamp(4)}},
	} {
		if _, err := db.Exec(statement.query, statement.arguments...); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err := Open(databasePath, filepath.Join(directory, "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	// Current pointers took over from the old heads.
	for saveID, head := range map[string]string{"source": "r2", "fork": "c2"} {
		save, err := repository.GetOmnisave(ctx, saveID)
		if err != nil {
			t.Fatal(err)
		}
		if save.CurrentRevisionID == nil || *save.CurrentRevisionID != head {
			t.Fatalf("expected %s current at its old head %s, got %v", saveID, head, save.CurrentRevisionID)
		}
	}

	// The copied root became an honest child of the fork point.
	copied, err := repository.GetRevision(ctx, "fork", "c1")
	if err != nil {
		t.Fatal(err)
	}
	if copied.ParentID == nil || *copied.ParentID != "r1" {
		t.Fatalf("expected the copied root re-parented onto r1, got %v", copied.ParentID)
	}

	// The fork's history is one connected chain with a single true root, and
	// its membership stops at the fork point's ancestry.
	history, err := repository.ListRevisions(ctx, "fork")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("expected the fork's chain of 3, got %d revision(s)", len(history))
	}
	roots := 0
	for _, revision := range history {
		if revision.ParentID == nil {
			if revision.ID != "r1" {
				t.Fatalf("expected r1 as the single root, got %s", revision.ID)
			}
			roots++
		}
	}
	if roots != 1 {
		t.Fatalf("expected a single root, got %d", roots)
	}
	if _, err := repository.GetRevision(ctx, "fork", "r2"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("the source's later revision leaked into the fork: %v", err)
	}

	// The source's own chain is untouched.
	sourceHistory, err := repository.ListRevisions(ctx, "source")
	if err != nil || len(sourceHistory) != 2 {
		t.Fatalf("expected the source's chain of 2, got %+v (%v)", sourceHistory, err)
	}
}
