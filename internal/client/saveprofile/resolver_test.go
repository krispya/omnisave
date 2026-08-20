package saveprofile_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/ludusavi"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

func TestWindowsProfileResolvesInsideAProtonEnvironment(t *testing.T) {
	prefix := t.TempDir()
	saveDirectory := filepath.Join(prefix, "drive_c", "users", "steamuser", "AppData", "Roaming", "Example", "Saves")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(saveDirectory, "progress.sav")
	if err := os.WriteFile(savePath, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}

	game := target.InstalledGame{
		ID:       "steam:123",
		TargetID: "steam",
		Identity: target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}}},
		Environment: target.Environment{
			HostOS:     saveprofile.OSLinux,
			Runtime:    target.RuntimeProton,
			PrefixRoot: prefix,
		},
	}
	profile := saveprofile.Profile{
		Provider:   "ludusavi",
		ProviderID: "Example",
		Rules: []saveprofile.Rule{{
			ID: "1", Path: "<winAppData>/Example/Saves", OS: saveprofile.OSWindows, Store: "steam", Kind: "save",
		}},
	}

	saves, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 1 || saves[0].Files[0].Path != savePath {
		t.Fatalf("expected the Windows rule to resolve inside Proton, got %+v", saves)
	}
}

func TestLudusaviMacProfileResolvesApplicationSupportSave(t *testing.T) {
	home := t.TempDir()
	saveDirectory := filepath.Join(home, "Library", "Application Support", "Example", "Saves")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(saveDirectory, "progress.sav")
	if err := os.WriteFile(savePath, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}

	profiles, err := ludusavi.New([]byte(`
Example:
  files:
    <macAppSupport>/Example/Saves:
      tags: [save]
      when:
        - os: mac
          store: steam
  steam:
    id: 123
`))
	if err != nil {
		t.Fatal(err)
	}
	game := target.InstalledGame{
		ID:       "steam:123",
		TargetID: "steam",
		Identity: target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}}},
		Environment: target.Environment{
			HostOS:  saveprofile.OSMacOS,
			Runtime: target.RuntimeNative,
			Home:    home,
		},
	}
	profile, err := profiles.Find(context.Background(), game.Identity)
	if err != nil {
		t.Fatal(err)
	}
	saves, err := saveprofile.Resolve(game, *profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 1 || saves[0].Files[0].Path != savePath {
		t.Fatalf("expected the macOS Application Support save, got %+v", saves)
	}
}

func TestRecursiveGlobsMatchEveryDepth(t *testing.T) {
	installRoot := t.TempDir()
	nested := filepath.Join(installRoot, "x", "y")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.sav", filepath.Join("x", "b.sav"), filepath.Join("x", "y", "c.sav"), filepath.Join("x", "notes.txt")} {
		if err := os.WriteFile(filepath.Join(installRoot, name), []byte("progress"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	game := target.InstalledGame{
		ID:          "steam:123",
		TargetID:    "steam",
		InstallRoot: installRoot,
		Environment: target.Environment{HostOS: saveprofile.OSLinux, Runtime: target.RuntimeNative},
	}
	profile := saveprofile.Profile{
		Provider:   "ludusavi",
		ProviderID: "Example",
		Rules:      []saveprofile.Rule{{ID: "1", Path: "<base>/**/*.sav", Kind: "save"}},
	}

	saves, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 3 {
		t.Fatalf("expected saves at every depth, got %+v", saves)
	}
	relative := []string{saves[0].Files[0].RelativePath, saves[0].Files[1].RelativePath, saves[0].Files[2].RelativePath}
	expected := []string{"a.sav", filepath.Join("x", "b.sav"), filepath.Join("x", "y", "c.sav")}
	for index := range expected {
		if relative[index] != expected[index] {
			t.Fatalf("expected %v, got %v", expected, relative)
		}
	}
}

func TestRootRulesStayInTheLibraryUnderProton(t *testing.T) {
	library := t.TempDir()
	installDirectory := filepath.Join(library, "steamapps", "common", "Example")
	if err := os.MkdirAll(installDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(installDirectory, "save.dat")
	if err := os.WriteFile(savePath, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}

	game := target.InstalledGame{
		ID:       "steam:123",
		TargetID: "steam",
		Identity: target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}}},
		Environment: target.Environment{
			HostOS:     saveprofile.OSLinux,
			Runtime:    target.RuntimeProton,
			StoreRoot:  library,
			PrefixRoot: filepath.Join(library, "steamapps", "compatdata", "123", "pfx"),
		},
	}
	profile := saveprofile.Profile{
		Provider:   "ludusavi",
		ProviderID: "Example",
		Rules: []saveprofile.Rule{{
			ID: "1", Path: "<root>/steamapps/common/Example/save.dat", OS: saveprofile.OSWindows, Store: "steam", Kind: "save",
		}},
	}

	saves, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 1 || saves[0].Files[0].Path != savePath {
		t.Fatalf("expected the library-relative save under Proton, got %+v", saves)
	}
}

func TestDriveRootUserRulesResolveOnEveryRuntime(t *testing.T) {
	profiles, err := ludusavi.New([]byte(`
Example:
  files:
    C:/Users/<osUserName>/Documents/MGR/SaveData/MGR.sav:
      tags: [save]
      when:
        - os: windows
  steam:
    id: 123
`))
	if err != nil {
		t.Fatal(err)
	}
	identity := target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}}}
	profile, err := profiles.Find(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Rules) != 1 || profile.Rules[0].Path != "<home>/Documents/MGR/SaveData/MGR.sav" {
		t.Fatalf("expected the drive-root user path to normalize to <home>, got %+v", profile.Rules)
	}

	prefix := t.TempDir()
	protonSave := filepath.Join(prefix, "drive_c", "users", "steamuser", "Documents", "MGR", "SaveData", "MGR.sav")
	if err := os.MkdirAll(filepath.Dir(protonSave), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(protonSave, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}
	deck := target.InstalledGame{
		ID: "steam:123", TargetID: "steam", Identity: identity,
		Environment: target.Environment{HostOS: saveprofile.OSLinux, Runtime: target.RuntimeProton, PrefixRoot: prefix},
	}
	saves, err := saveprofile.Resolve(deck, *profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || saves[0].Files[0].Path != protonSave {
		t.Fatalf("expected the prefix user save under Proton, got %+v", saves)
	}

	home := t.TempDir()
	windowsSave := filepath.Join(home, "Documents", "MGR", "SaveData", "MGR.sav")
	if err := os.MkdirAll(filepath.Dir(windowsSave), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(windowsSave, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}
	desktop := target.InstalledGame{
		ID: "steam:123", TargetID: "steam", Identity: identity,
		Environment: target.Environment{HostOS: saveprofile.OSWindows, Runtime: target.RuntimeNative, Home: home},
	}
	saves, err = saveprofile.Resolve(desktop, *profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || saves[0].Files[0].Path != windowsSave {
		t.Fatalf("expected the native Windows home save, got %+v", saves)
	}
}

func TestSteamCloudRulesAreLeftToTheSteamAdapter(t *testing.T) {
	profiles, err := ludusavi.New([]byte(`
Example:
  files:
    <home>/.config/Example:
      tags: [save]
    <root>/userdata/<storeUserId>/123/remote:
      tags: [save]
    <root>/userData/<storeUserId>/123/Local:
      tags: [save]
    <home>/.local/share/Steam/userdata/<storeUserId>/123/remote:
      tags: [save]
  steam:
    id: 123
`))
	if err != nil {
		t.Fatal(err)
	}
	identity := target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}}}
	profile, err := profiles.Find(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Rules) != 1 || profile.Rules[0].Path != "<home>/.config/Example" {
		t.Fatalf("expected userdata rules to stay with the steam adapter, got %+v", profile.Rules)
	}
}

func TestZeroRuleDuplicateNeverShadowsARealGame(t *testing.T) {
	profiles, err := ludusavi.New([]byte(`
.Age:
  steam:
    id: 638510
DotAGE:
  files:
    <home>/AppData/LocalLow/CKCGames/dotAGE:
      tags: [save]
  steam:
    id: 638510
`))
	if err != nil {
		t.Fatal(err)
	}
	identity := target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "638510"}}}
	profile, err := profiles.Find(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Title != "DotAGE" || len(profile.Rules) != 1 {
		t.Fatalf("expected the rule-bearing duplicate to win, got %+v", profile)
	}
}

func TestRuleIdentitySurvivesManifestGrowth(t *testing.T) {
	before, err := ludusavi.New([]byte(`
Example:
  files:
    <home>/.config/Example:
      tags: [save]
  steam:
    id: 123
`))
	if err != nil {
		t.Fatal(err)
	}
	after, err := ludusavi.New([]byte(`
Example:
  files:
    <base>/Saves:
      tags: [save]
    <home>/.config/Example:
      tags: [save]
  steam:
    id: 123
`))
	if err != nil {
		t.Fatal(err)
	}
	identity := target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}}}
	first, err := before.Find(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	second, err := after.Find(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, rule := range second.Rules {
		ids[rule.Path] = rule.ID
	}
	if ids["<home>/.config/Example"] != first.Rules[0].ID {
		t.Fatalf("expected the surviving rule to keep its identity, got %+v then %+v", first.Rules, second.Rules)
	}
	if ids["<base>/Saves"] == ids["<home>/.config/Example"] {
		t.Fatalf("expected distinct identities per template, got %+v", second.Rules)
	}
}

func TestRulesOnlySearchAbsolutePaths(t *testing.T) {
	home := t.TempDir()
	game := target.InstalledGame{
		ID: "steam:123", TargetID: "steam",
		Identity:    target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}}},
		Environment: target.Environment{HostOS: saveprofile.OSLinux, Runtime: target.RuntimeNative, Home: home},
	}
	profile := saveprofile.Profile{
		Provider: "ludusavi", ProviderID: "123",
		Rules: []saveprofile.Rule{
			{ID: "a", Path: "Developer cloud save only?", Kind: "save"},
			{ID: "b", Path: "Library/Application Support/com.example/save.ito", Kind: "save"},
		},
	}
	saves, err := saveprofile.Resolve(game, profile)
	if err != nil || saves != nil {
		t.Fatalf("expected relative manifest entries to resolve nothing, got %+v, %v", saves, err)
	}
	destinations, err := saveprofile.ResolveDestinations(game, profile)
	if err != nil || destinations != nil {
		t.Fatalf("expected relative manifest entries to offer no destination, got %+v, %v", destinations, err)
	}
}

func TestBracketedLibraryPathsStayLiteral(t *testing.T) {
	parent := t.TempDir()
	installRoot := filepath.Join(parent, "Games [SSD]", "Example")
	saveDirectory := filepath.Join(installRoot, "Saves")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(saveDirectory, "progress.sav")
	if err := os.WriteFile(savePath, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}

	game := target.InstalledGame{
		ID: "steam:123", TargetID: "steam", InstallRoot: installRoot,
		Environment: target.Environment{HostOS: saveprofile.OSLinux, Runtime: target.RuntimeNative},
	}
	profile := saveprofile.Profile{
		Provider: "ludusavi", ProviderID: "123",
		Rules: []saveprofile.Rule{{ID: "1", Path: "<base>/Saves", Kind: "save"}},
	}
	saves, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 1 || saves[0].Files[0].Path != savePath {
		t.Fatalf("expected the bracketed library save, got %+v", saves)
	}
	destinations, err := saveprofile.ResolveDestinations(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(destinations) != 1 || destinations[0].Locations[0].Path != saveDirectory {
		t.Fatalf("expected the literal bracketed destination, got %+v", destinations)
	}
}

func TestGlobMatchingIgnoresManifestCasing(t *testing.T) {
	installRoot := t.TempDir()
	saveDirectory := filepath.Join(installRoot, "SaveData")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(saveDirectory, "slot1.sav")
	if err := os.WriteFile(savePath, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}

	game := target.InstalledGame{
		ID: "steam:123", TargetID: "steam", InstallRoot: installRoot,
		Environment: target.Environment{HostOS: saveprofile.OSLinux, Runtime: target.RuntimeNative},
	}
	profile := saveprofile.Profile{
		Provider: "ludusavi", ProviderID: "123",
		Rules: []saveprofile.Rule{{ID: "1", Path: "<base>/SaveData/*.SAV", Kind: "save"}},
	}
	saves, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 1 || saves[0].Files[0].Path != savePath {
		t.Fatalf("expected community casing to still match, got %+v", saves)
	}
}

func TestLiteralPathsIgnoreManifestCasing(t *testing.T) {
	installRoot := t.TempDir()
	saveDirectory := filepath.Join(installRoot, "SaveData")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(saveDirectory, "profile.bin")
	if err := os.WriteFile(savePath, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}
	manifestCased := filepath.Join(installRoot, "savedata", "PROFILE.BIN")
	_, err := os.Lstat(manifestCased)
	caseSensitive := err != nil

	game := target.InstalledGame{
		ID: "steam:123", TargetID: "steam", InstallRoot: installRoot,
		Environment: target.Environment{HostOS: saveprofile.OSLinux, Runtime: target.RuntimeNative},
	}
	profile := saveprofile.Profile{
		Provider: "ludusavi", ProviderID: "123",
		Rules: []saveprofile.Rule{{ID: "1", Path: "<base>/savedata/PROFILE.BIN", Kind: "save"}},
	}
	saves, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 1 {
		t.Fatalf("expected community casing to still match a literal rule, got %+v", saves)
	}
	got := saves[0].Files[0].Path
	if caseSensitive {
		// The exact spelling is absent, so resolution must report the
		// file's on-disk casing.
		if got != savePath {
			t.Fatalf("expected on-disk casing %q, got %q", savePath, got)
		}
	} else if !strings.EqualFold(got, savePath) {
		t.Fatalf("expected the save under any casing of %q, got %q", savePath, got)
	}

	destinations, err := saveprofile.ResolveDestinations(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(destinations) != 1 || len(destinations[0].Locations) != 1 {
		t.Fatalf("expected one destination for the literal rule, got %+v", destinations)
	}
	location := destinations[0].Locations[0]
	if location.Kind != target.SaveLocationFile {
		t.Fatalf("expected the existing file's kind, got %+v", location)
	}
	if caseSensitive && location.Path != savePath {
		t.Fatalf("expected the destination to carry on-disk casing %q, got %q", savePath, location.Path)
	}
}

func TestLiteralDirectoryRulesIgnoreManifestCasing(t *testing.T) {
	installRoot := t.TempDir()
	saveDirectory := filepath.Join(installRoot, "SaveData")
	if err := os.MkdirAll(saveDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(saveDirectory, "slot1.sav")
	if err := os.WriteFile(savePath, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}

	game := target.InstalledGame{
		ID: "steam:123", TargetID: "steam", InstallRoot: installRoot,
		Environment: target.Environment{HostOS: saveprofile.OSLinux, Runtime: target.RuntimeNative},
	}
	profile := saveprofile.Profile{
		Provider: "ludusavi", ProviderID: "123",
		Rules: []saveprofile.Rule{{ID: "1", Path: "<base>/savedata", Kind: "save"}},
	}
	saves, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 1 {
		t.Fatalf("expected the differently cased directory rule to match, got %+v", saves)
	}
	file := saves[0].Files[0]
	if !strings.EqualFold(file.Path, savePath) || file.RelativePath != "slot1.sav" {
		t.Fatalf("expected %q relative to its matched directory, got %+v", savePath, file)
	}
}

func TestAmbiguousLiteralCasingIsNotGuessed(t *testing.T) {
	installRoot := t.TempDir()
	first := filepath.Join(installRoot, "SaveData")
	second := filepath.Join(installRoot, "savedata")
	if err := os.Mkdir(first, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(second, 0755); err != nil {
		if os.IsExist(err) {
			t.Skip("filesystem does not support case-distinct sibling directories")
		}
		t.Fatal(err)
	}
	for _, directory := range []string{first, second} {
		if err := os.WriteFile(filepath.Join(directory, "slot1.sav"), []byte("progress"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	game := target.InstalledGame{
		ID: "steam:123", TargetID: "steam", InstallRoot: installRoot,
		Environment: target.Environment{HostOS: saveprofile.OSLinux, Runtime: target.RuntimeNative},
	}
	profile := saveprofile.Profile{
		Provider: "ludusavi", ProviderID: "123",
		Rules: []saveprofile.Rule{{ID: "1", Path: "<base>/SAVEDATA", Kind: "save"}},
	}
	saves, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if saves != nil {
		t.Fatalf("expected ambiguous literal spellings to resolve no save, got %+v", saves)
	}

	destinations, err := saveprofile.ResolveDestinations(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if destinations != nil {
		t.Fatalf("expected ambiguous literal spellings to offer no destination, got %+v", destinations)
	}
}

func TestGlobsFollowSymlinkedDirectories(t *testing.T) {
	relocated := t.TempDir()
	if err := os.MkdirAll(filepath.Join(relocated, "Saves"), 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(relocated, "Saves", "progress.sav")
	if err := os.WriteFile(savePath, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}
	installRoot := t.TempDir()
	if err := os.Symlink(relocated, filepath.Join(installRoot, "Foo")); err != nil {
		t.Fatal(err)
	}

	game := target.InstalledGame{
		ID: "steam:123", TargetID: "steam", InstallRoot: installRoot,
		Environment: target.Environment{HostOS: saveprofile.OSLinux, Runtime: target.RuntimeNative},
	}
	profile := saveprofile.Profile{
		Provider: "ludusavi", ProviderID: "123",
		Rules: []saveprofile.Rule{{ID: "1", Path: "<base>/*/Saves/*.sav", Kind: "save"}},
	}
	saves, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 1 {
		t.Fatalf("expected the save behind the symlinked directory, got %+v", saves)
	}
}

func TestUnreadableEntriesNarrowDiscoveryInsteadOfFailingIt(t *testing.T) {
	installRoot := t.TempDir()
	saveDirectory := filepath.Join(installRoot, "Saves")
	locked := filepath.Join(saveDirectory, "locked")
	if err := os.MkdirAll(locked, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(saveDirectory, "ok.sav")
	if err := os.WriteFile(savePath, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0755) })

	game := target.InstalledGame{
		ID: "steam:123", TargetID: "steam", InstallRoot: installRoot,
		Environment: target.Environment{HostOS: saveprofile.OSLinux, Runtime: target.RuntimeNative},
	}
	profile := saveprofile.Profile{
		Provider: "ludusavi", ProviderID: "123",
		Rules: []saveprofile.Rule{{ID: "1", Path: "<base>/Saves", Kind: "save"}},
	}
	saves, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 1 || saves[0].Files[0].Path != savePath {
		t.Fatalf("expected the readable save beside the locked directory, got %+v", saves)
	}
}

func TestDuplicateSteamIdsKeepTheFirstTitle(t *testing.T) {
	profiles, err := ludusavi.New([]byte(`
Alpha:
  files:
    <home>/.config/Alpha:
      tags: [save]
  steam:
    id: 123
Alpha Deluxe:
  files:
    <home>/.config/AlphaDeluxe:
      tags: [save]
  steam:
    id: 123
`))
	if err != nil {
		t.Fatal(err)
	}
	identity := target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}}}
	profile, err := profiles.Find(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Title != "Alpha" || len(profile.Rules) != 1 || profile.Rules[0].Path != "<home>/.config/Alpha" {
		t.Fatalf("expected the first sorted title to win, got %+v", profile)
	}
}

func TestStoreUserIdMatchesAnyAccountDirectory(t *testing.T) {
	home := t.TempDir()
	accountDirectory := filepath.Join(home, "AppData", "Roaming", "Example", "76561198000000000")
	if err := os.MkdirAll(accountDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(accountDirectory, "progress.sav")
	if err := os.WriteFile(savePath, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}

	game := target.InstalledGame{
		ID:       "steam:123",
		TargetID: "steam",
		Identity: target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}}},
		Environment: target.Environment{
			HostOS:  saveprofile.OSWindows,
			Runtime: target.RuntimeNative,
			Home:    home,
		},
	}
	profile := saveprofile.Profile{
		Provider:   "ludusavi",
		ProviderID: "Example",
		Rules: []saveprofile.Rule{{
			ID: "1", Path: "<winAppData>/Example/<storeUserId>", OS: saveprofile.OSWindows, Store: "steam", Kind: "save",
		}},
	}

	saves, err := saveprofile.Resolve(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || len(saves[0].Files) != 1 || saves[0].Files[0].Path != savePath {
		t.Fatalf("expected the account directory to match, got %+v", saves)
	}
	if saves[0].Files[0].RelativePath != filepath.Join("76561198000000000", "progress.sav") {
		t.Fatalf("expected an account-scoped relative path, got %+v", saves[0].Files[0])
	}
}

func TestProfileResolvesAProspectiveDestinationBeforeItsPathsExist(t *testing.T) {
	home := t.TempDir()
	game := target.InstalledGame{
		ID: "steam:123", TargetID: "steam",
		Identity:    target.GameIdentity{Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "123"}}},
		Environment: target.Environment{HostOS: saveprofile.OSMacOS, Runtime: target.RuntimeNative, Home: home},
	}
	profile := saveprofile.Profile{
		Provider: "ludusavi", ProviderID: "Example",
		Rules: []saveprofile.Rule{
			{ID: "save", Path: "<macAppSupport>/Example/Saves", OS: saveprofile.OSMacOS, Store: "steam"},
			{ID: "config", Path: "<macPreferences>/com.example.game.plist", OS: saveprofile.OSMacOS, Store: "steam"},
		},
	}

	destinations, err := saveprofile.ResolveDestinations(game, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(destinations) != 1 || len(destinations[0].Locations) != 2 {
		t.Fatalf("expected one prospective profile save with both locations, got %+v", destinations)
	}
	if destinations[0].Locations[0].Kind != target.SaveLocationUnknown ||
		destinations[0].Locations[0].Path != filepath.Join(home, "Library", "Application Support", "Example", "Saves") ||
		destinations[0].Locations[1].Path != filepath.Join(home, "Library", "Preferences", "com.example.game.plist") {
		t.Fatalf("unexpected prospective profile locations: %+v", destinations[0].Locations)
	}
}
