package store_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

func TestObjectsSurviveAndKeepTheirIdentity(t *testing.T) {
	saveStore := openStore(t, t.TempDir())

	contents := strings.Repeat("save data. ", 500)
	hash := hashOf(contents)
	if _, err := saveStore.PutObject(hash, strings.NewReader(contents)); err != nil {
		t.Fatal(err)
	}

	restored := readObject(t, saveStore, hash)
	if restored != contents {
		t.Fatalf("expected the exact content back, got %d bytes", len(restored))
	}
	size, err := saveStore.VerifyObject(hash)
	if err != nil || size != int64(len(contents)) {
		t.Fatalf("expected verification to report %d bytes, got %d (%v)", len(contents), size, err)
	}
}

func TestObjectsAreRefusedWhenTheyDoNotMatchTheirHash(t *testing.T) {
	saveStore := openStore(t, t.TempDir())

	claimed := hashOf("what the caller says this is")
	_, err := saveStore.PutObject(claimed, strings.NewReader("what it actually is"))
	if !errors.Is(err, store.ErrContentMismatch) {
		t.Fatalf("expected a content mismatch, got %v", err)
	}
	if saveStore.HasObject(claimed) {
		t.Fatal("expected nothing to rest under a hash the content did not match")
	}
}

// Damage to one object has to be detectable from the store alone — the name is
// the checksum — and has to leave every other object usable.
func TestDamagedObjectsAreFoundWithoutAnythingToCompareAgainst(t *testing.T) {
	root := t.TempDir()
	saveStore := openStore(t, root)

	damaged, healthy := hashOf("first save"), hashOf("second save")
	for contents, hash := range map[string]string{"first save": damaged, "second save": healthy} {
		if _, err := saveStore.PutObject(hash, strings.NewReader(contents)); err != nil {
			t.Fatal(err)
		}
	}

	path := filepath.Join(root, "objects", damaged[:2], damaged+".gz")
	corrupt, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	corrupt[len(corrupt)/2] ^= 0xff
	if err := os.WriteFile(path, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := saveStore.VerifyObject(damaged); err == nil {
		t.Fatal("expected the damaged object to fail verification")
	}
	if _, err := saveStore.VerifyObject(healthy); err != nil {
		t.Fatalf("expected the untouched object to stay verifiable: %v", err)
	}
}

func TestIdenticalContentIsStoredOnce(t *testing.T) {
	root := t.TempDir()
	saveStore := openStore(t, root)

	contents := "one save, committed twice"
	hash := hashOf(contents)
	for range 3 {
		if _, err := saveStore.PutObject(hash, strings.NewReader(contents)); err != nil {
			t.Fatal(err)
		}
	}

	count := 0
	if err := saveStore.EachObject(func(string) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one object, found %d", count)
	}
}

func TestRecordsRoundTripThroughTheDirectory(t *testing.T) {
	root := t.TempDir()
	saveStore := openStore(t, root)

	created := time.Date(2026, 7, 26, 10, 14, 3, 0, time.UTC)
	manifest := store.Revision{
		ID:        "revision-1",
		Omnisave:  store.RevisionOmnisave{ID: "save-1", DisplayName: "Save 1"},
		Game:      store.RevisionGame{ID: "game-1", Title: "Chrono Trigger", Platform: "snes"},
		CreatedAt: created,
		Files: []store.RevisionFile{
			{Path: "saves/chrono.srm", SHA256: hashOf("srm"), Size: 3},
		},
	}
	if err := saveStore.PutRevision(manifest); err != nil {
		t.Fatal(err)
	}

	// Reopened as a directory somebody else was handed, with no memory of the
	// process that wrote it.
	reopened := openStore(t, root)
	read, err := reopened.GetRevision("revision-1")
	if err != nil {
		t.Fatal(err)
	}
	if read.Omnisave.DisplayName != "Save 1" || read.Game.Title != "Chrono Trigger" {
		t.Fatalf("expected the identity to survive, got %+v", read)
	}
	if !read.CreatedAt.Equal(created) {
		t.Fatalf("expected %s, got %s", created, read.CreatedAt)
	}
	if len(read.Files) != 1 || read.Files[0].Path != "saves/chrono.srm" {
		t.Fatalf("expected the file list to survive, got %+v", read.Files)
	}
}

// A manifest is what a person reads during a recovery, so it has to be plain
// text they can open, not something that needs this program to decode.
func TestManifestsAreReadableText(t *testing.T) {
	root := t.TempDir()
	saveStore := openStore(t, root)
	if err := saveStore.PutRevision(store.Revision{
		ID:       "revision-1",
		Omnisave: store.RevisionOmnisave{ID: "save-1", DisplayName: "Save 1"},
		Files:    []store.RevisionFile{{Path: "saves/chrono.srm", SHA256: hashOf("srm"), Size: 3}},
	}); err != nil {
		t.Fatal(err)
	}

	var found string
	if err := filepath.WalkDir(filepath.Join(root, "revisions"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found = string(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"kind": "revision"`, `"display_name": "Save 1"`, `"path": "saves/chrono.srm"`} {
		if !strings.Contains(found, expected) {
			t.Fatalf("expected a manifest containing %s, got:\n%s", expected, found)
		}
	}
}

// A store found without context — on a backup drive, years later — has to name
// its own format, because docs/RECOVERY.md tells a person what to do with it
// only once they know that is what they are holding.
func TestEveryStoreNamesItsOwnFormat(t *testing.T) {
	root := t.TempDir()
	openStore(t, root)

	version, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil || !strings.HasPrefix(string(version), "omnisave-store ") {
		t.Fatalf("expected the store to name its own format, got %q (%v)", version, err)
	}
}

func TestOpeningAnOlderStoreAdvancesItsFormatMarker(t *testing.T) {
	root := t.TempDir()
	openStore(t, root)
	if err := os.WriteFile(filepath.Join(root, "VERSION"),
		[]byte("omnisave-store 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	openStore(t, root)
	version, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if string(version) != fmt.Sprintf("omnisave-store %d\n", store.Version) {
		t.Fatalf("expected the marker to advance to the writable format, got %q", version)
	}
}

// An older binary must not guess at a newer format when save data is at stake.
func TestAStoreFromTheFutureIsRefused(t *testing.T) {
	root := t.TempDir()
	openStore(t, root)
	if err := os.WriteFile(filepath.Join(root, "VERSION"),
		[]byte("omnisave-store 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(root); err == nil {
		t.Fatal("expected a store newer than this binary to be refused")
	}
}

func openStore(t *testing.T, root string) *store.Store {
	t.Helper()
	saveStore, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return saveStore
}

func readObject(t *testing.T, saveStore *store.Store, hash string) string {
	t.Helper()
	reader, err := saveStore.OpenObject(hash)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	contents, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func hashOf(contents string) string {
	sum := sha256.Sum256([]byte(contents))
	return hex.EncodeToString(sum[:])
}
