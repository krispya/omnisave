package steam

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam/steamworks"
)

// placementFixture builds a Steam root whose bookkeeping marks the game as
// API-cloud (every cached file from root 0) and a game directory shipping a
// Steamworks library.
func placementFixture(t *testing.T, apiRoot string) (target.Target, target.InstalledGame) {
	t.Helper()
	root := t.TempDir()
	cache := `"123"
{
	"file.save"
	{
		"root"		"` + apiRoot + `"
	}
}`
	cachePath := filepath.Join(root, "userdata", "42", "123", "remotecache.vdf")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}
	install := filepath.Join(root, "steamapps", "common", "Game")
	if err := os.MkdirAll(install, 0o755); err != nil {
		t.Fatal(err)
	}
	// One library name per platform this test may run on.
	for _, name := range []string{"libsteam_api.so", "steam_api64.dll", "libsteam_api.dylib"} {
		if err := os.WriteFile(filepath.Join(install, name), []byte("lib"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	discovered := target.Target{ID: "steam:" + root, Adapter: adapterName, Root: root}
	game := target.InstalledGame{
		ID:       discovered.ID + ":123",
		TargetID: discovered.ID,
		Identity: target.GameIdentity{Identifiers: []catalog.GameIdentifier{
			{Namespace: "steam.app", Value: "123"},
		}},
		InstallRoot: install,
	}
	return discovered, game
}

func TestFinishPlacementReconcilesAPIGames(t *testing.T) {
	discovered, game := placementFixture(t, "0")
	var received steamworks.Request
	adapter := New()
	adapter.runHelper = func(_ context.Context, request steamworks.Request) (steamworks.Result, error) {
		received = request
		return steamworks.Result{
			Written:   []string{"file.save"},
			Unchanged: []string{"other.save"},
			Extras:    []string{"gone.save"},
			Failed:    []steamworks.Failure{{Name: "big.save", Cause: "quota"}},
		}, nil
	}
	save := target.Save{Files: []target.File{{Path: filepath.Join(game.InstallRoot, "file.save")}}}
	report, err := adapter.FinishPlacement(context.Background(), discovered, game, save)
	if err != nil {
		t.Fatal(err)
	}
	if received.AppID != "123" {
		t.Fatalf("app id = %q", received.AppID)
	}
	if received.Library == "" {
		t.Fatal("no library passed to the helper")
	}
	if len(received.Files) != 1 {
		t.Fatalf("files = %v", received.Files)
	}
	// Unchanged entries stay out of the report; only real writes register.
	if len(report.Registered) != 1 || report.Registered[0] != "file.save" || len(report.Extras) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.Failed["big.save"] != "quota" {
		t.Fatalf("failed = %v", report.Failed)
	}
}

func TestFinishPlacementLeavesFolderGamesToSteam(t *testing.T) {
	discovered, game := placementFixture(t, "2")
	adapter := New()
	adapter.runHelper = func(context.Context, steamworks.Request) (steamworks.Result, error) {
		t.Fatal("a folder-replicated game must not be reconciled")
		return steamworks.Result{}, nil
	}
	report, err := adapter.FinishPlacement(context.Background(), discovered, game, target.Save{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Registered) != 0 || report.Skipped != "" {
		t.Fatalf("report = %+v", report)
	}
}

func TestFinishPlacementReportsAMissingLibrary(t *testing.T) {
	discovered, game := placementFixture(t, "0")
	game.InstallRoot = t.TempDir()
	adapter := New()
	if _, err := adapter.FinishPlacement(context.Background(), discovered, game, target.Save{}); err == nil {
		t.Fatal("expected an error when the game ships no steamworks library")
	}
}
