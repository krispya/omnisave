package gamesave_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/gamesave"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

const extensionHeader = `ADAPTER_ID = "fixture.slots"
GAME_KEYS = ["fixture.app:1"]
`
const fixtureSlot = `{"root": 0, "parent": "", "directory": "slot1", "key": "one", "label": "Slot 1", "location": "fixture-slot"}`

func TestAnExtensionPlugsInWithoutGameSpecificHostCode(t *testing.T) {
	bundle := fstest.MapFS{"example/rules.star": {Data: []byte(extensionHeader + "def discover(snapshot):\n    return [" + fixtureSlot + "]\n")}}
	adapters, err := gamesave.LoadExtensions(bundle, "*/rules.star")
	if err != nil || len(adapters) != 1 {
		t.Fatalf("load extension: %v", err)
	}
	game := target.InstalledGame{ID: "game", TargetID: "fixture:local", Identity: target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "fixture.app", Value: "1"}}}}
	if !adapters[0].Supports(game.Identity) {
		t.Fatal("declared game was not registered")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "slot1"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "slot1", "progress"), []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}
	slots, err := adapters[0].Discover(context.Background(), game, []target.SaveDestination{{Locations: []target.SaveLocation{{Path: root, Kind: target.SaveLocationDirectory}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 1 || slots[0].Save.Scope.Adapter != "fixture.slots" || len(slots[0].Save.Files) != 1 || slots[0].Save.Files[0].RelativePath != "progress" || slots[0].Save.Cloud != nil {
		t.Fatal("extension did not define the independent local slot")
	}
}

func TestExtensionsCannotBypassNativeOwnership(t *testing.T) {
	for _, c := range []struct {
		name, body string
		symlink    bool
	}{
		{"traversal", `    return [` + strings.Replace(fixtureSlot, `"slot1"`, `"../outside"`, 1) + `]`, false},
		{"overlap", `    return [` + fixtureSlot + `,` + strings.NewReplacer(`"slot1"`, `"slot1/nested"`, `"one"`, `"two"`).Replace(fixtureSlot) + `]`, false},
		{"symlink", `    return [` + fixtureSlot + `]`, true},
		{"cloud traversal", `    return [dict(` + fixtureSlot + `, cloud={"account_id":"fixture", "files":["../sibling"], "directories":[]})]`, false},
		{"malformed answer", `    return [{"root":0}]`, false},
		{"unbounded rules", "    for i in range(1000000000):\n        pass\n    return []", false},
		{"private error", `    fail("private-account-identifier")`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			if c.symlink {
				if err := os.Symlink(outside, filepath.Join(root, "slot1")); err != nil {
					t.Fatal(err)
				}
			}
			bundle := fstest.MapFS{"rules.star": {Data: []byte(extensionHeader + "def discover(snapshot):\n" + c.body + "\n")}}
			adapters, err := gamesave.LoadExtensions(bundle, "rules.star")
			if err != nil {
				t.Fatal(err)
			}
			slots, err := adapters[0].Discover(context.Background(), target.InstalledGame{ID: "game"}, []target.SaveDestination{{Locations: []target.SaveLocation{{Path: root, Kind: target.SaveLocationDirectory}}}})
			if err == nil || len(slots) != 0 {
				t.Fatal("unsafe extension produced usable slots")
			}
			if strings.Contains(err.Error(), "private-account-identifier") || strings.Contains(err.Error(), root) {
				t.Fatal("extension error exposed native context")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatal("extension changed files outside its scope")
			}
		})
	}
}

// A game without slots can still leave files out of its whole save.
func TestAnExtensionCanOnlyIgnoreFiles(t *testing.T) {
	bundle := fstest.MapFS{"rules.star": {Data: []byte(extensionHeader + `IGNORED = ["*.tmp", "logs/*.txt"]` + "\n")}}
	adapters, err := gamesave.LoadExtensions(bundle, "rules.star")
	if err != nil {
		t.Fatal(err)
	}
	slots, err := adapters[0].Discover(context.Background(), target.InstalledGame{ID: "game"}, nil)
	if err != nil || len(slots) != 0 {
		t.Fatal("an extension without discover offered slots")
	}
	for relative, ignored := range map[string]bool{
		"save.tmp": true, "nested/save.tmp": true, "logs/run.txt": true, "old/logs/run.txt": true,
		"logs/nested/run.txt": false, "save.dat": false, "tmp": false,
	} {
		if adapters[0].Ignored().Ignores(relative) != ignored {
			t.Fatalf("%s: ignored should be %v", relative, ignored)
		}
	}
}

func TestIgnoredPatternsCannotNameFilesOutsideTheSave(t *testing.T) {
	for _, ignored := range []string{`["../outside"]`, `["/absolute"]`, `["[unclosed"]`, `["C:\\save"]`, `"*.tmp"`} {
		bundle := fstest.MapFS{"rules.star": {Data: []byte(extensionHeader + "IGNORED = " + ignored + "\n")}}
		if _, err := gamesave.LoadExtensions(bundle, "rules.star"); err == nil {
			t.Fatalf("IGNORED = %s was accepted", ignored)
		}
	}
}

func TestConflictingExtensionsAreRejected(t *testing.T) {
	content := []byte(extensionHeader + "def discover(snapshot):\n    return []\n")
	bundle := fstest.MapFS{"one/rules.star": {Data: content}, "two/rules.star": {Data: []byte(strings.Replace(string(content), "fixture.slots", "fixture.other", 1))}}
	if _, err := gamesave.LoadExtensions(bundle, "*/rules.star"); err == nil {
		t.Fatal("two extensions claimed the same game")
	}
}

func TestSlotIdentityDoesNotDependOnWhereTheSaveFolderLives(t *testing.T) {
	bundle := fstest.MapFS{"rules.star": {Data: []byte(extensionHeader + "def discover(snapshot):\n    return [" + fixtureSlot + "]\n")}}
	adapters, err := gamesave.LoadExtensions(bundle, "rules.star")
	if err != nil {
		t.Fatal(err)
	}
	game := target.InstalledGame{ID: "game", TargetID: "fixture:local"}
	discover := func() string {
		root := t.TempDir()
		slots, err := adapters[0].Discover(context.Background(), game, []target.SaveDestination{{Locations: []target.SaveLocation{{Path: root, Kind: target.SaveLocationDirectory}}}})
		if err != nil || len(slots) != 1 {
			t.Fatalf("discover: %v", err)
		}
		if strings.Contains(slots[0].Save.ID, root) {
			t.Fatal("identity exposed a native path")
		}
		return slots[0].Save.ID
	}
	if discover() != discover() {
		t.Fatal("moving the save folder orphaned the recorded slot choice")
	}
}
