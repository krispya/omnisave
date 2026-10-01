package gamehub_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/ludusavi/embedded"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/gamehub"
)

// A game GameHub installed is a Steam game running in a Wine prefix of its
// own, so the same community rules that place it under Proton place it here.
func TestScanFindsAGameHubGamesSaveInsideItsPrefix(t *testing.T) {
	gameHub := newGameHub(t)
	gameHub.install(t, "374320", "DARK SOULS III")
	prefix := gameHub.createPrefix(t, "374320")
	writeFile(t, filepath.Join(prefix, "drive_c", "users", "steamuser", "AppData", "Roaming",
		"DarkSoulsIII", "76561198000000000", "DS30000.sl2"))

	scans, err := client.NewScanner(embedded.Provider(), gamehub.New(gameHub.root)).Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 || len(scans[0].Games) != 1 {
		t.Fatalf("expected one GameHub game, got %+v", scans)
	}
	game := scans[0].Games[0]
	// The same Steam purchase as on any other client, so one Game everywhere.
	if appID, _ := game.Game.Identity.Identifier("steam.app"); appID != "374320" || game.Game.Identity.Platform != "PC" {
		t.Fatalf("expected the game's Steam identity, got %+v", game.Game.Identity)
	}
	if len(game.Saves) != 1 || len(game.Saves[0].Files) != 1 ||
		game.Saves[0].Files[0].RelativePath != filepath.Join("76561198000000000", "DS30000.sl2") {
		t.Fatalf("expected the save inside the game's prefix, got %+v", game.Saves)
	}
}

// GameHub creates a game's prefix when it first prepares the game to run.
// Until then the game's Windows folders exist nowhere, so a scan reports the
// game with no place for a save rather than reaching into the Mac's own home.
func TestAGameHubGameWithoutAPrefixHasNoSaveLocationYet(t *testing.T) {
	gameHub := newGameHub(t)
	gameHub.install(t, "413150", "Stardew Valley")
	rules := knownRules{{ID: "saves", Path: "<winAppData>/StardewValley/Saves"}}

	scans, err := client.NewScanner(rules, gamehub.New(gameHub.root)).Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 || len(scans[0].Games) != 1 {
		t.Fatalf("expected the installed game, got %+v", scans)
	}
	if game := scans[0].Games[0]; len(game.Saves) != 0 || len(game.Destinations) != 0 {
		t.Fatalf("expected no save location before the prefix exists, got %+v", game)
	}
}

// fakeGameHub is a GameHub data directory with one Windows library, written
// as GameHub keeps its bookkeeping.
type fakeGameHub struct {
	root    string
	library string
}

func newGameHub(t *testing.T) fakeGameHub {
	t.Helper()
	gameHub := fakeGameHub{root: t.TempDir(), library: t.TempDir()}
	writeJSON(t, filepath.Join(gameHub.root, "steam-client", "steam-library-registry.v1.json"), map[string]any{
		"schemaVersion": 1,
		"libraries":     []any{map[string]any{"platform": "Windows", "canonicalRoot": gameHub.library}},
	})
	return gameHub
}

// install puts a game into the library as Steam's own app manifest and
// install directory.
func (g fakeGameHub) install(t *testing.T, appID, title string) {
	t.Helper()
	steamApps := filepath.Join(g.library, "steamapps")
	if err := os.MkdirAll(filepath.Join(steamApps, "common", title), 0755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("\"AppState\"\n{\n\t\"appid\"\t\t\"%s\"\n\t\"name\"\t\t\"%s\"\n\t\"installdir\"\t\t\"%s\"\n}\n", appID, title, title)
	if err := os.WriteFile(filepath.Join(steamApps, "appmanifest_"+appID+".acf"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
}

// createPrefix binds a game to a container and creates the container's
// prefix, as GameHub does when it first prepares the game to run.
func (g fakeGameHub) createPrefix(t *testing.T, appID string) string {
	t.Helper()
	prefix := filepath.Join(g.root, "wine-engine", "containers", "virtual_containers", "1")
	if err := os.MkdirAll(filepath.Join(prefix, "drive_c"), 0755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(g.root, "gamehub", "game_container_store.json"), map[string]any{
		"schema_version": 3,
		"bindings": []any{map[string]any{
			"platform": "steam", "platform_app_id": appID, "virtual_container_id": "1",
		}},
	})
	writeJSON(t, filepath.Join(g.root, "wine-engine", "container", "wine_virtual_containers.json"), map[string]any{
		"virtual_containers": map[string]any{"1": map[string]any{"id": "1", "prefix_path": prefix}},
	})
	return prefix
}

// knownRules answers every game with the same save-location rules.
type knownRules []saveprofile.Rule

func (r knownRules) Find(context.Context, target.GameIdentity) (*saveprofile.Profile, error) {
	return &saveprofile.Profile{Provider: "test", ProviderID: "game", Rules: r}, nil
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}
}
