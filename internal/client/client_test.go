package client_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/ludusavi"
	"github.com/krisbaumgartner/omnisave/internal/client/target/retroarch"
	"github.com/krisbaumgartner/omnisave/internal/client/target/retroarch/locator"
	steamtarget "github.com/krisbaumgartner/omnisave/internal/client/target/steam"
	steamlocator "github.com/krisbaumgartner/omnisave/internal/client/target/steam/locator"
)

func TestManualScanFindsSNESSavesOnly(t *testing.T) {
	directory := t.TempDir()
	saveDirectory := filepath.Join(directory, "saves")
	playlistDirectory := filepath.Join(directory, "playlists")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(playlistDirectory, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(directory, "retroarch.cfg")
	config := fmt.Sprintf("savefile_directory = %q\nplaylist_directory = %q\n", saveDirectory, playlistDirectory)
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	writePlaylist(t, playlistDirectory, "Nintendo - Super Nintendo Entertainment System.lpl", map[string]string{
		"path": filepath.Join(directory, "roms", "Chrono Trigger.sfc"), "label": "Chrono Trigger", "crc32": "2D206BF7|crc",
	})
	writePlaylist(t, playlistDirectory, "Nintendo - Game Boy Advance.lpl", map[string]string{
		"path": filepath.Join(directory, "roms", "Pokemon Emerald.gba"), "label": "Pokemon Emerald", "crc32": "1F1C08FB|crc",
	})
	if err := os.WriteFile(filepath.Join(saveDirectory, "Chrono Trigger.srm"), []byte("snes save"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(saveDirectory, "Pokemon Emerald.srm"), []byte("gba save"), 0600); err != nil {
		t.Fatal(err)
	}

	scanner := client.NewScanner(nil, retroarch.New(locator.NewInstaller(configPath)))
	var progress []client.ScanProgress
	scans, err := scanner.ScanWithProgress(context.Background(), func(event client.ScanProgress) {
		progress = append(progress, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 || len(scans[0].Games) != 1 || len(scans[0].Games[0].Saves) != 1 {
		t.Fatalf("expected one target with one enabled-platform save, got %+v", scans)
	}
	if scans[0].Target.Source != "installer" {
		t.Fatalf("expected installer target, got %+v", scans[0].Target)
	}
	game := scans[0].Games[0].Game
	if game.Identity.Platform != "snes" || game.Identity.Title != "Chrono Trigger" ||
		len(game.Identity.Fingerprints) != 1 || game.Identity.Fingerprints[0] != (catalog.GameFingerprint{
		Platform: "snes", Algorithm: "crc32", Value: "2d206bf7",
	}) {
		t.Fatalf("unexpected installed game: %+v", game)
	}
	save := scans[0].Games[0].Saves[0]
	if save.Kind != "battery" || save.GameID != game.ID {
		t.Fatalf("unexpected discovered save: %+v", save)
	}
	if len(save.Files) != 1 || save.Files[0].Path != filepath.Join(saveDirectory, "Chrono Trigger.srm") {
		t.Fatalf("unexpected save files: %+v", save.Files)
	}
	destinations := scans[0].Games[0].Destinations
	if len(destinations) != 1 || destinations[0].ID != save.ID || len(destinations[0].Locations) != 1 ||
		destinations[0].Locations[0].Path != saveDirectory {
		t.Fatalf("expected the prospective RetroArch save location, got %+v", destinations)
	}
	if len(progress) != 2 || progress[0].Stage != client.ScanStarted || progress[1].Stage != client.ScanCompleted {
		t.Fatalf("expected adapter start and completion progress, got %+v", progress)
	}
	if err := os.Remove(filepath.Join(saveDirectory, "Chrono Trigger.srm")); err != nil {
		t.Fatal(err)
	}
	freshScans, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(freshScans[0].Games[0].Saves) != 0 || len(freshScans[0].Games[0].Destinations) != 1 {
		t.Fatalf("expected the RetroArch destination before a save exists, got %+v", freshScans[0].Games[0])
	}
}

func TestManualScanReportsRetroArchBeforeItHasConfigurationOrSaves(t *testing.T) {
	application := filepath.Join(t.TempDir(), "RetroArch.app")
	binaryDirectory := filepath.Join(application, "Contents", "MacOS")
	if err := os.MkdirAll(binaryDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binaryDirectory, "RetroArch"), nil, 0755); err != nil {
		t.Fatal(err)
	}

	scanner := client.NewScanner(nil, retroarch.New(locator.NewApplication(application)))
	scans, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 {
		t.Fatalf("expected the installed application to be reported, got %+v", scans)
	}
	if scans[0].Target.Root != application || scans[0].Target.Location != "" {
		t.Fatalf("expected an installation without configuration, got %+v", scans[0].Target)
	}
	if len(scans[0].Games) != 0 {
		t.Fatalf("expected no games before RetroArch is configured, got %+v", scans[0].Games)
	}
}

func TestSteamAndInstallerLocatorsDeduplicateOneInstallation(t *testing.T) {
	steamRoot := t.TempDir()
	library := t.TempDir()
	steamApps := filepath.Join(library, "steamapps")
	installation := filepath.Join(steamApps, "common", "RetroArch")
	if err := os.MkdirAll(installation, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(installation, "retroarch.cfg")
	if err := os.WriteFile(configPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(steamApps, "appmanifest_1118310.acf"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(steamRoot, "steamapps"), 0755); err != nil {
		t.Fatal(err)
	}
	libraryFolders := fmt.Sprintf("\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\" %q\n\t}\n}\n", library)
	if err := os.WriteFile(filepath.Join(steamRoot, "steamapps", "libraryfolders.vdf"), []byte(libraryFolders), 0600); err != nil {
		t.Fatal(err)
	}

	adapter := retroarch.New(
		locator.NewSteam(steamRoot),
		locator.NewInstaller(configPath),
	)
	targets, err := adapter.DiscoverTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Source != "steam" || targets[0].Root != installation {
		t.Fatalf("expected one Steam target, got %+v", targets)
	}
}

func TestManualScanFindsOneMultiFileSteamCloudSave(t *testing.T) {
	steamRoot := t.TempDir()
	writeSteamApp(t, steamRoot, "413150", "Stardew Valley", "Stardew Valley")
	remoteDirectory := filepath.Join(steamRoot, "userdata", "76561198000000000", "413150", "remote")
	if err := os.MkdirAll(filepath.Join(remoteDirectory, "profile"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteDirectory, "SaveGameInfo"), []byte("summary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteDirectory, "profile", "player.dat"), []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}

	scanner := client.NewScanner(nil, steamtarget.New(steamlocator.NewInstaller(steamRoot)))
	scans, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 || len(scans[0].Games) != 1 || len(scans[0].Games[0].Saves) != 1 {
		t.Fatalf("expected one Steam target with one app save set, got %+v", scans)
	}

	game := scans[0].Games[0].Game
	steamID, ok := game.Identity.Identifier("steam.app")
	if !ok || steamID != "413150" || game.Identity.Title != "Stardew Valley" {
		t.Fatalf("unexpected installed game: %+v", game)
	}
	save := scans[0].Games[0].Saves[0]
	if save.Kind != "cloud" || save.GameID != game.ID {
		t.Fatalf("unexpected discovered save: %+v", save)
	}
	if save.Metadata["account_id"] != "76561198000000000" {
		t.Fatalf("expected Steam account identity, got %+v", save.Metadata)
	}
	if len(save.Files) != 2 || save.Files[0].RelativePath != "SaveGameInfo" || save.Files[1].RelativePath != filepath.Join("profile", "player.dat") {
		t.Fatalf("expected the complete Cloud file set, got %+v", save.Files)
	}
	destinations := scans[0].Games[0].Destinations
	canonicalRemoteDirectory, err := filepath.EvalSymlinks(remoteDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(destinations) != 1 || destinations[0].ID != save.ID || len(destinations[0].Locations) != 1 ||
		destinations[0].Locations[0].Path != canonicalRemoteDirectory {
		t.Fatalf("expected the prospective Steam Cloud location, got %+v", destinations)
	}
}

func TestManualScanUsesLudusaviForANonCloudSteamGame(t *testing.T) {
	steamRoot := t.TempDir()
	installRoot := writeSteamApp(t, steamRoot, "900001", "Local Save Game", "LocalSaveGame")
	saveDirectory := filepath.Join(installRoot, "Saves")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(saveDirectory, "progress.sav"), []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "settings.json"), []byte("configuration"), 0600); err != nil {
		t.Fatal(err)
	}

	profiles, err := ludusavi.New([]byte(`
Local Save Game:
  files:
    <base>/Saves:
      tags: [save]
    <base>/settings.json:
      tags: [config]
  steam:
    id: 900001
`))
	if err != nil {
		t.Fatal(err)
	}
	scanner := client.NewScanner(profiles, steamtarget.New(steamlocator.NewInstaller(steamRoot)))
	scans, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 || len(scans[0].Games) != 1 || len(scans[0].Games[0].Saves) != 1 {
		t.Fatalf("expected one profile-resolved local save, got %+v", scans)
	}
	save := scans[0].Games[0].Saves[0]
	if save.Kind != "local" || len(save.Files) != 1 || save.Files[0].Path != filepath.Join(saveDirectory, "progress.sav") {
		t.Fatalf("unexpected local save: %+v", save)
	}
	if save.Metadata["profile_provider"] != "ludusavi" {
		t.Fatalf("expected Ludusavi provenance, got %+v", save.Metadata)
	}
}

func writePlaylist(t *testing.T, directory, name string, item map[string]string) {
	t.Helper()
	contents, err := json.Marshal(map[string]any{"items": []any{item}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), contents, 0600); err != nil {
		t.Fatal(err)
	}
}

func writeSteamApp(t *testing.T, steamRoot, appID, title, installDirectory string) string {
	t.Helper()
	steamApps := filepath.Join(steamRoot, "steamapps")
	installRoot := filepath.Join(steamApps, "common", installDirectory)
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`"AppState"
{
	"appid"	"%s"
	"name"	"%s"
	"installdir"	"%s"
}
`, appID, title, installDirectory)
	if err := os.WriteFile(filepath.Join(steamApps, "appmanifest_"+appID+".acf"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(installRoot)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
