package labeler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

func TestGamesWithoutALabelerStayUnnamed(t *testing.T) {
	content := []byte(`{"seed": 1}`)
	artifacts := &artifactOpener{blobs: map[string][]byte{hashOf(content): content}}
	files := []omnisave.RevisionFile{{
		Path:     "remote/profile1/saves/current_run.save",
		Artifact: omnisave.Artifact{Format: "application/json", SHA256: hashOf(content), Size: int64(len(content))},
	}}
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

func TestUndertaleIsLabeledLikeItsSaveScreen(t *testing.T) {
	// The fields the in-game menu reads, as the game spells them: quoted
	// zero-fraction numbers under [General], CRLF line ends.
	ini := []byte("[General]\r\nRoom=\"18.000000\"\r\nTime=\"47190.000000\"\r\nLove=\"3.000000\"\r\nName=\"ALMA\"\r\n[Flowey]\r\nMet1=\"1.000000\"\r\n")

	// file0 holds the same fields positionally: name, LOVE, then room and
	// play time on lines 548 and 549.
	lines := make([]string, 549)
	for index := range lines {
		lines[index] = "0 "
	}
	lines[0], lines[1], lines[547], lines[548] = "ALMA ", "3 ", "18 ", "47190 "
	file0 := []byte(strings.Join(lines, "\r\n"))

	blobs := map[string][]byte{hashOf(ini): ini, hashOf(file0): file0}
	named, err := New(&gameDirectory{games: map[string]*catalog.Game{
		"game-undertale": {ID: "game-undertale", Identifiers: []catalog.GameIdentifier{
			{Namespace: "steam.app", Value: "391540"},
		}},
	}}, &artifactOpener{blobs: blobs})
	if err != nil {
		t.Fatal(err)
	}

	fileOf := func(path string, content []byte) omnisave.RevisionFile {
		return omnisave.RevisionFile{Path: path, Artifact: omnisave.Artifact{SHA256: hashOf(content), Size: int64(len(content))}}
	}
	want := "ALMA LV 3, Ruins - Mouse Hole, 26:13"
	full := []omnisave.RevisionFile{fileOf("battery/file0", file0), fileOf("battery/undertale.ini", ini)}
	if label := named.NameRevision(context.Background(), "game-undertale", full); label != want {
		t.Fatalf("full save labeled %q, want %q", label, want)
	}
	fromFile0 := []omnisave.RevisionFile{fileOf("battery/file0", file0)}
	if label := named.NameRevision(context.Background(), "game-undertale", fromFile0); label != want {
		t.Fatalf("save without its ini labeled %q, want %q", label, want)
	}
	if label := named.NameRevision(context.Background(), "game-undertale", nil); label != "" {
		t.Fatalf("empty save labeled %q, want unnamed", label)
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
