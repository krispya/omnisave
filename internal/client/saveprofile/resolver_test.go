package saveprofile_test

import (
	"context"
	"os"
	"path/filepath"
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
