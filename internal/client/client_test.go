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
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/ludusavi/embedded"
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
	if game.Identity.Platform != "PC" {
		t.Fatalf("expected the store-level PC platform, got %q", game.Identity.Platform)
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

// A game whose adapter-discovered save demonstrably holds the same save
// family the profile rules locate — files by the same names — keeps that one
// representation, so one game never tracks two saves whose lineages could
// never converge (FDR-003, decision 10).
func TestManualScanElectsTheCloudSaveOverProfileRules(t *testing.T) {
	steamRoot := t.TempDir()
	installRoot := writeSteamApp(t, steamRoot, "413150", "Stardew Valley", "Stardew Valley")
	remoteDirectory := filepath.Join(steamRoot, "userdata", "76561198000000000", "413150", "remote")
	if err := os.MkdirAll(remoteDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteDirectory, "SaveGameInfo"), []byte("summary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteDirectory, "progress.sav"), []byte("cloud-progress"), 0600); err != nil {
		t.Fatal(err)
	}
	saveDirectory := filepath.Join(installRoot, "Saves")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(saveDirectory, "progress.sav"), []byte("native-progress"), 0600); err != nil {
		t.Fatal(err)
	}

	profiles, err := ludusavi.New([]byte(`
Stardew Valley:
  files:
    <base>/Saves:
      tags: [save]
  steam:
    id: 413150
`))
	if err != nil {
		t.Fatal(err)
	}
	scanner := client.NewScanner(profiles, steamtarget.New(steamlocator.NewInstaller(steamRoot)))
	scans, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 || len(scans[0].Games) != 1 {
		t.Fatalf("expected one Steam game, got %+v", scans)
	}
	saves := scans[0].Games[0].Saves
	if len(saves) != 1 || saves[0].Kind != "cloud" {
		t.Fatalf("expected only the Cloud save representation, got %+v", saves)
	}
	destinations := scans[0].Games[0].Destinations
	if len(destinations) != 1 || destinations[0].Kind != "cloud" {
		t.Fatalf("expected only the Cloud destination, got %+v", destinations)
	}
}

// A mirror holding files that share no names with what the profile rules
// locate is auxiliary content — settings a game syncs, a leftover — and is
// no evidence the real progress is covered, so both representations stay
// tracked exactly as they were before election existed.
func TestManualScanKeepsBothSavesWhenTheMirrorSharesNoNames(t *testing.T) {
	steamRoot := t.TempDir()
	installRoot := writeSteamApp(t, steamRoot, "413150", "Stardew Valley", "Stardew Valley")
	remoteDirectory := filepath.Join(steamRoot, "userdata", "76561198000000000", "413150", "remote")
	if err := os.MkdirAll(remoteDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteDirectory, "options.cfg"), []byte("settings"), 0600); err != nil {
		t.Fatal(err)
	}
	saveDirectory := filepath.Join(installRoot, "Saves")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(saveDirectory, "progress.sav"), []byte("native-progress"), 0600); err != nil {
		t.Fatal(err)
	}

	profiles, err := ludusavi.New([]byte(`
Stardew Valley:
  files:
    <base>/Saves:
      tags: [save]
  steam:
    id: 413150
`))
	if err != nil {
		t.Fatal(err)
	}
	scanner := client.NewScanner(profiles, steamtarget.New(steamlocator.NewInstaller(steamRoot)))
	scans, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 || len(scans[0].Games) != 1 {
		t.Fatalf("expected one Steam game, got %+v", scans)
	}
	saves := scans[0].Games[0].Saves
	if len(saves) != 2 || saves[0].Kind != "cloud" || saves[1].Kind != "local" {
		t.Fatalf("expected the Cloud save and the uncovered native save, got %+v", saves)
	}
}

func TestScanFindsDarkSoulsIIISavesUnderProtonFromTheEmbeddedManifest(t *testing.T) {
	steamRoot := t.TempDir()
	writeSteamApp(t, steamRoot, "374320", "DARK SOULS III", "DARK SOULS III")
	accountDirectory := filepath.Join(steamRoot, "steamapps", "compatdata", "374320", "pfx",
		"drive_c", "users", "steamuser", "AppData", "Roaming", "DarkSoulsIII", "76561198000000000")
	if err := os.MkdirAll(accountDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(accountDirectory, "DS30000.sl2"), []byte("bonfires"), 0600); err != nil {
		t.Fatal(err)
	}

	scanner := client.NewScanner(embedded.Provider(), steamtarget.New(steamlocator.NewInstaller(steamRoot)))
	scans, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 || len(scans[0].Games) != 1 || len(scans[0].Games[0].Saves) != 1 {
		t.Fatalf("expected the embedded profile to find one local save, got %+v", scans)
	}
	// The embedded manifest refreshes from community data, so the test pins
	// the files this scan created rather than the entry's exact rule count.
	save := scans[0].Games[0].Saves[0]
	if save.Kind != "local" || save.Metadata["profile_provider_id"] != "374320" {
		t.Fatalf("expected a Ludusavi profile save for Dark Souls III, got %+v", save)
	}
	// A Proton install is still the same Steam purchase, so it claims PC too.
	if platform := scans[0].Games[0].Game.Identity.Platform; platform != "PC" {
		t.Fatalf("expected the store-level PC platform, got %q", platform)
	}
	foundSave := false
	for _, file := range save.Files {
		if file.RelativePath == filepath.Join("76561198000000000", "DS30000.sl2") {
			foundSave = true
		}
	}
	if !foundSave {
		t.Fatalf("expected the account-scoped save file, got %+v", save.Files)
	}
	foundDestination := false
	for _, destination := range scans[0].Games[0].Destinations {
		for _, location := range destination.Locations {
			if filepath.Base(location.Path) == "DarkSoulsIII" {
				foundDestination = true
			}
		}
	}
	if !foundDestination {
		t.Fatalf("expected the DarkSoulsIII directory among prospective destinations, got %+v", scans[0].Games[0].Destinations)
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
