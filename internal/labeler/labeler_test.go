package labeler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

// The fixtures under testdata are real Slay the Spire II saves: one run in
// progress and three finished runs (a win, a death to an event, an abandon).
// A revision is the whole save directory at one moment, so each case below is
// one plausible file set a commit would carry.

const spireGameID = "game-spire2"

func TestLabelsFollowARunAcrossItsLife(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		label string
	}{
		{
			name: "mid-run snapshots name the fight in progress",
			files: map[string]string{
				"remote/profile.save":                          "profile.save",
				"remote/profile1/saves/prefs.save":             "prefs.save",
				"remote/profile1/saves/current_run.save":       "current_run.save",
				"remote/profile1/saves/history/1783479289.run": "1783479289.run",
			},
			label: "Necrobinder A5, Underdocks floor 12, 11/66 HP",
		},
		{
			name: "a finished run is named by its outcome",
			files: map[string]string{
				"remote/profile.save":                          "profile.save",
				"remote/profile1/saves/prefs.save":             "prefs.save",
				"remote/profile1/saves/history/1783479289.run": "1783479289.run",
				"remote/profile1/saves/history/1783567598.run": "1783567598.run",
				"remote/profile1/saves/history/1783569631.run": "1783569631.run",
			},
			label: "Necrobinder A5 abandoned, Hive floor 23",
		},
		{
			name: "wins carry the climb and its duration",
			files: map[string]string{
				"remote/profile.save":                          "profile.save",
				"remote/profile1/saves/history/1783479289.run": "1783479289.run",
			},
			label: "Necrobinder A4 win, 45 floors, 1h02m",
		},
		{
			name: "deaths to events name the event",
			files: map[string]string{
				"remote/profile.save":                          "profile.save",
				"remote/profile1/saves/history/1783567598.run": "1783567598.run",
			},
			label: "Necrobinder A5 died to Slippery Bridge, Hive floor 20",
		},
		{
			name: "a fresh profile has nothing to say",
			files: map[string]string{
				"remote/profile.save":              "profile.save",
				"remote/profile1/saves/prefs.save": "prefs.save",
			},
			label: "",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			files, artifacts := fixtureRevision(t, testCase.files)
			named := spireLabeler(t, artifacts)
			label := named.NameRevision(context.Background(), spireGameID, files)
			if label != testCase.label {
				t.Fatalf("label = %q, want %q", label, testCase.label)
			}
		})
	}
}

func TestGamesWithoutALabelerStayUnnamed(t *testing.T) {
	files, artifacts := fixtureRevision(t, map[string]string{
		"remote/profile1/saves/current_run.save": "current_run.save",
	})
	named, err := New(&gameDirectory{games: map[string]*catalog.Game{
		"game-unknown": {ID: "game-unknown", Identifiers: []catalog.GameIdentifier{
			{Namespace: "steam.app", Value: "999999"},
		}},
	}}, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if label := named.NameRevision(context.Background(), "game-unknown", files); label != "" {
		t.Fatalf("unregistered game labeled %q, want unnamed", label)
	}
	if label := named.NameRevision(context.Background(), "game-missing", files); label != "" {
		t.Fatalf("unknown game labeled %q, want unnamed", label)
	}
}

func TestAMisbehavingScriptCostsOnlyTheName(t *testing.T) {
	spinning := "GAME_KEYS = [\"test.app:1\"]\n" +
		"def label(snapshot):\n" +
		"    for index in range(1000000000):\n" +
		"        pass\n" +
		"    return \"done\"\n"
	loaded, err := loadScript("spinning.star", []byte(spinning))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.run(context.Background(), emptySnapshot()); err == nil {
		t.Fatal("a spinning script must be cancelled, not waited out")
	}

	wrongType := "GAME_KEYS = [\"test.app:2\"]\n" +
		"def label(snapshot):\n" +
		"    return 42\n"
	loaded, err = loadScript("wrong_type.star", []byte(wrongType))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.run(context.Background(), emptySnapshot()); err == nil {
		t.Fatal("a non-string label must be an error, not a name")
	}
}

func TestOversizedFilesReadAsAbsent(t *testing.T) {
	// The artifact holds tiny content but declares an enormous size, the way a
	// giant save would look to the manifest-driven budget check.
	content := []byte(`{"ok": true}`)
	artifacts := &artifactOpener{blobs: map[string][]byte{hashOf(content): content}}
	files := []omnisave.RevisionFile{{
		Path:     "huge.json",
		Artifact: omnisave.Artifact{Format: "application/json", SHA256: hashOf(content), Size: maxFileBytes + 1},
	}}
	source := "GAME_KEYS = [\"test.app:3\"]\n" +
		"def label(snapshot):\n" +
		"    return \"read it\" if snapshot.json(\"huge.json\") else None\n"
	loaded, err := loadScript("budget.star", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	name, err := loaded.run(context.Background(), newSnapshot(context.Background(), files, artifacts))
	if err != nil || name != "" {
		t.Fatalf("oversized file gave (%q, %v), want it to read as absent", name, err)
	}
}

func TestNamesAreFlattenedAndBounded(t *testing.T) {
	name := cleanName("  spread \n over\t lines  " + strings.Repeat("x", 200))
	if strings.ContainsAny(name, "\n\t") || len([]rune(name)) > maxNameLength {
		t.Fatalf("cleanName gave %q, want one bounded line", name)
	}
}

// spireLabeler is a Labeler whose directory knows the fixtures' game by its
// Steam identity, which is how the built-in registers itself.
func spireLabeler(t *testing.T, artifacts ArtifactOpener) *Labeler {
	t.Helper()
	named, err := New(&gameDirectory{games: map[string]*catalog.Game{
		spireGameID: {ID: spireGameID, Title: "Slay the Spire 2", Identifiers: []catalog.GameIdentifier{
			{Namespace: "hasheous.game", Value: "77001"},
			{Namespace: "steam.app", Value: "2868840"},
		}},
	}}, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	return named
}

// fixtureRevision builds a revision file set from testdata, storing each
// fixture's bytes behind its real content hash.
func fixtureRevision(t *testing.T, paths map[string]string) ([]omnisave.RevisionFile, *artifactOpener) {
	t.Helper()
	files := make([]omnisave.RevisionFile, 0, len(paths))
	artifacts := &artifactOpener{blobs: make(map[string][]byte)}
	for path, fixture := range paths {
		content, err := os.ReadFile(filepath.Join("testdata", fixture))
		if err != nil {
			t.Fatal(err)
		}
		artifacts.blobs[hashOf(content)] = content
		files = append(files, omnisave.RevisionFile{
			Path: path,
			Artifact: omnisave.Artifact{
				Format: "application/json",
				SHA256: hashOf(content),
				Size:   int64(len(content)),
			},
		})
	}
	return files, artifacts
}

func emptySnapshot() *snapshot {
	return newSnapshot(context.Background(), nil, &artifactOpener{})
}

func hashOf(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

type gameDirectory struct {
	games map[string]*catalog.Game
}

func (d *gameDirectory) GetGame(_ context.Context, id string) (*catalog.Game, error) {
	if game, ok := d.games[id]; ok {
		return game, nil
	}
	return nil, storage.ErrNotFound
}

type artifactOpener struct {
	blobs map[string][]byte
}

func (o *artifactOpener) OpenArtifact(_ context.Context, sha256 string) (io.ReadCloser, error) {
	content, ok := o.blobs[sha256]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}
