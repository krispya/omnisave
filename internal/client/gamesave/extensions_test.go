package gamesave

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// Adding a shipped extension also adds its own cases; no per-game Go registry
// or test switch is needed. Fixtures are synthetic native trees, never live saves.
//
//go:embed builtin/*/tests.json
var extensionFixtures embed.FS

type extensionCases struct {
	GameKey          string                  `json:"game_key"`
	AdapterID        string                  `json:"adapter_id"`
	CloudFiles       []string                `json:"cloud_files"`
	CloudDirectories []target.CloudDirectory `json:"cloud_directories"`
	Cases            []struct {
		Name        string
		Target      string
		Roots       []string
		Directories []string
		Files       []string
		Symlinks    map[string]string
		Unavailable bool
		Slots       []struct {
			Directory    string
			Label        string
			Files        []string
			CloudPrefix  string `json:"cloud_prefix"`
			CloudAccount string `json:"cloud_account"`
		}
	}
}

func TestExtensionFixtures(t *testing.T) {
	entries, err := fs.Glob(builtinScripts, "builtin/*/rules.star")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Run(filepath.Base(filepath.Dir(entry)), func(t *testing.T) {
			adapters, err := LoadExtensions(builtinScripts, entry)
			if err != nil || len(adapters) != 1 {
				t.Fatalf("load extension: %v", err)
			}
			content, err := extensionFixtures.ReadFile(strings.TrimSuffix(entry, "rules.star") + "tests.json")
			if err != nil {
				t.Fatal("extension must ship tests.json")
			}
			var fixture extensionCases
			if err := json.Unmarshal(content, &fixture); err != nil {
				t.Fatal(err)
			}
			if len(fixture.Cases) == 0 {
				t.Fatal("extension must have test cases")
			}
			adapter := adapters[0]
			namespace, value, found := strings.Cut(fixture.GameKey, ":")
			identity := target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: namespace, Value: value}}}
			if !found || adapter.ID() != fixture.AdapterID || !adapter.Supports(identity) {
				t.Fatal("extension registration differs from fixture")
			}
			for _, c := range fixture.Cases {
				t.Run(c.Name, func(t *testing.T) {
					base := t.TempDir()
					for _, dir := range append(slices.Clone(c.Roots), c.Directories...) {
						if err := os.MkdirAll(filepath.Join(base, filepath.FromSlash(dir)), 0700); err != nil {
							t.Fatal(err)
						}
					}
					for _, file := range c.Files {
						native := filepath.Join(base, filepath.FromSlash(file))
						if err := os.MkdirAll(filepath.Dir(native), 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(native, []byte("fixture"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					for link, destination := range c.Symlinks {
						if err := os.Symlink(filepath.Join(base, filepath.FromSlash(destination)), filepath.Join(base, filepath.FromSlash(link))); err != nil {
							t.Fatal(err)
						}
					}
					var destinations []target.SaveDestination
					for _, root := range c.Roots {
						destinations = append(destinations, target.SaveDestination{Locations: []target.SaveLocation{{Path: filepath.Join(base, filepath.FromSlash(root)), Kind: target.SaveLocationDirectory}}})
					}
					game := target.InstalledGame{ID: "game", TargetID: c.Target, Identity: identity}
					slots, err := adapter.Discover(context.Background(), game, destinations)
					if c.Unavailable {
						if err == nil || len(slots) != 0 {
							t.Fatal("unsupported discovery must refuse all slots")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if len(slots) != len(c.Slots) {
						t.Fatalf("got %d slots, want %d", len(slots), len(c.Slots))
					}
					for i, want := range c.Slots {
						slot := slots[i]
						native := filepath.Join(base, filepath.FromSlash(want.Directory))
						if slot.Destination.Locations[0].Path != native || slot.Save.Slot != want.Label || slot.Destination.Slot != want.Label || slot.Save.Scope != (omnisave.SaveScope{Kind: omnisave.ScopeSlot, Adapter: fixture.AdapterID}) {
							t.Fatal("slot boundary or label differs")
						}
						var paths []string
						for _, file := range slot.Save.Files {
							paths = append(paths, filepath.ToSlash(file.RelativePath))
							if file.Path != filepath.Join(native, file.RelativePath) {
								t.Fatal("file escaped its slot")
							}
						}
						slices.Sort(paths)
						if !slices.Equal(paths, want.Files) {
							t.Fatalf("captured %v, want %v", paths, want.Files)
						}
						cloud := slot.Save.Cloud
						if want.CloudPrefix == "" {
							if cloud != nil {
								t.Fatal("unexpected cloud ownership")
							}
							continue
						}
						cloudRoot := native
						for range strings.Split(want.CloudPrefix, "/") {
							cloudRoot = filepath.Dir(cloudRoot)
						}
						if cloud == nil || cloud.Prefix != want.CloudPrefix || cloud.AccountID != want.CloudAccount || cloud.Root != cloudRoot || !slices.Equal(cloud.Files, fixture.CloudFiles) || !reflect.DeepEqual(cloud.Directories, fixture.CloudDirectories) {
							t.Fatal("cloud membership differs from fixture")
						}
					}
				})
			}
		})
	}
}
