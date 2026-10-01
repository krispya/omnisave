// Package sqlitetest opens real repositories for tests, so every domain's
// tests exercise the same persistence rules the server ships rather than a
// second implementation of them.
package sqlitetest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
)

// Open returns a repository in a fresh temporary directory, closed when the
// test ends, whose Library already holds a minimal game for each of games.
func Open(t testing.TB, games ...string) *sqlite.Repository {
	t.Helper()
	directory := t.TempDir()
	repository, err := sqlite.Open(filepath.Join(directory, "omnisave.db"), filepath.Join(directory, "store"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repository.Close() })
	AddGame(t, repository, games...)
	return repository
}

// AddGame stores a minimal Library game with the given IDs, so a test can
// create saves for it without resolving evidence first.
func AddGame(t testing.TB, repository catalog.Repository, ids ...string) {
	t.Helper()
	for _, id := range ids {
		game := catalog.Game{ID: id, Title: id, MetadataSource: "test", RefreshedAt: time.Now().UTC()}
		if err := repository.SaveGame(context.Background(), game); err != nil {
			t.Fatal(err)
		}
	}
}
