package steamcloud_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/steamcloud"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

func steamIdentity(appID string) target.GameIdentity {
	return target.GameIdentity{
		Title:       "Faster Than Light",
		Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: appID}},
	}
}

// A developer declares one folder in Windows terms and then says how the
// other operating systems reach the same place. Each spelling becomes a rule
// of its own, so a Device on any of them looks where that game's saves
// actually are.
func TestSteamCloudSpellsOneFolderForEveryOperatingSystem(t *testing.T) {
	steamRoot := t.TempDir()
	writeAppinfo(t, filepath.Join(steamRoot, "appcache"), app{
		id: 212680,
		section: map[string]any{
			"common": map[string]any{"name": "FTL: Faster Than Light"},
			"ufs": map[string]any{
				"savefiles": map[string]any{
					"0": map[string]any{
						"root":    "WinMyDocuments",
						"path":    "My Games/FasterThanLight",
						"pattern": "prof.sav",
					},
				},
				"rootoverrides": map[string]any{
					"0": map[string]any{
						"root": "WinMyDocuments", "os": "MacOS", "oscompare": "=",
						"useinstead": "MacAppSupport",
						"pathtransforms": map[string]any{
							"0": map[string]any{"find": "My Games/FasterThanLight", "replace": "FasterThanLight"},
						},
					},
					"1": map[string]any{
						"root": "WinMyDocuments", "os": "Linux", "oscompare": "=",
						"useinstead": "LinuxXdgDataHome",
						"pathtransforms": map[string]any{
							"0": map[string]any{"find": "My Games/FasterThanLight", "replace": "FasterThanLight"},
						},
					},
				},
			},
		},
	})

	profile, err := steamcloud.New(steamRoot).Find(context.Background(), steamIdentity("212680"))
	if err != nil {
		t.Fatal(err)
	}
	if profile.Provider != steamcloud.ProviderName || profile.ProviderID != "212680" ||
		profile.Title != "FTL: Faster Than Light" {
		t.Fatalf("unexpected profile identity: %+v", profile)
	}
	spelling := make(map[string]string, len(profile.Rules))
	for _, rule := range profile.Rules {
		if rule.Store != "steam" || rule.Kind != "save" || rule.ID == "" {
			t.Fatalf("unexpected rule shape: %+v", rule)
		}
		spelling[rule.OS] = rule.Path
	}
	wanted := map[string]string{
		saveprofile.OSWindows: "<winDocuments>/My Games/FasterThanLight/prof.sav",
		saveprofile.OSMacOS:   "<macAppSupport>/FasterThanLight/prof.sav",
		saveprofile.OSLinux:   "<xdgData>/FasterThanLight/prof.sav",
	}
	for operatingSystem, path := range wanted {
		if spelling[operatingSystem] != path {
			t.Errorf("expected %s to look in %q, got %q", operatingSystem, path, spelling[operatingSystem])
		}
	}
}

// A folder every operating system reaches the same way needs no constraint,
// and Steam's own recursion and account placeholders survive the translation.
func TestSteamCloudCarriesRecursionAndAccountPlaceholders(t *testing.T) {
	steamRoot := t.TempDir()
	writeAppinfo(t, filepath.Join(steamRoot, "appcache"), app{
		id: 70,
		section: map[string]any{
			"common": map[string]any{"name": "Half-Life"},
			"ufs": map[string]any{
				"savefiles": map[string]any{
					"0": map[string]any{
						"root": "gameinstall", "path": "valve", "pattern": "*.cfg", "recursive": 1,
						"platforms": map[string]any{"0": "all"},
					},
					"1": map[string]any{
						"root": "gameinstall", "path": "{Steam3AccountID}/config", "pattern": "*.vdf",
					},
				},
			},
		},
	})

	profile, err := steamcloud.New(steamRoot).Find(context.Background(), steamIdentity("70"))
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Rules) != 2 {
		t.Fatalf("expected one rule per declared folder, got %+v", profile.Rules)
	}
	for _, rule := range profile.Rules {
		if rule.OS != "" {
			t.Errorf("expected a folder reached the same way everywhere to carry no constraint, got %+v", rule)
		}
	}
	if profile.Rules[0].Path != "<base>/valve/**/*.cfg" {
		t.Errorf("expected recursion to become a ** segment, got %q", profile.Rules[0].Path)
	}
	if profile.Rules[1].Path != "<base>/<storeUserId>/config/*.vdf" {
		t.Errorf("expected Steam's account placeholder to be translated, got %q", profile.Rules[1].Path)
	}
}

// A game that reaches Steam Cloud through the API has no replicated folder
// for Steam to describe. Saying so is not the same as saying the game is
// unknown: only one of the two means this Device cannot protect it.
func TestSteamCloudDistinguishesAnAPIGameFromAnUnknownOne(t *testing.T) {
	steamRoot := t.TempDir()
	writeAppinfo(t, filepath.Join(steamRoot, "appcache"),
		app{
			id: 2868840,
			section: map[string]any{
				"common": map[string]any{"name": "Slay the Spire 2"},
				"ufs":    map[string]any{"quota": 50000000, "maxnumfiles": 10000},
			},
		},
		app{
			id:      440,
			section: map[string]any{"common": map[string]any{"name": "Team Fortress 2"}},
		},
	)
	cache := filepath.Join(steamRoot, "userdata", "67689364", "2868840")
	if err := os.MkdirAll(cache, 0755); err != nil {
		t.Fatal(err)
	}
	remotecache := "\"2868840\"\n{\n\t\"profile.save\"\n\t{\n\t\t\"root\"\t\t\"0\"\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(cache, "remotecache.vdf"), []byte(remotecache), 0600); err != nil {
		t.Fatal(err)
	}

	provider := steamcloud.New(steamRoot)
	if _, err := provider.Find(context.Background(), steamIdentity("2868840")); !errors.Is(err, steamcloud.ErrCloudAPI) {
		t.Fatalf("expected the API-stored game to say so, got %v", err)
	}
	if _, err := provider.Find(context.Background(), steamIdentity("440")); !errors.Is(err, saveprofile.ErrNotFound) {
		t.Fatalf("expected a game with no cloud configuration to be a plain miss, got %v", err)
	}
	if _, err := provider.Find(context.Background(), steamIdentity("999999")); !errors.Is(err, saveprofile.ErrNotFound) {
		t.Fatalf("expected an app Steam has never cached to be a plain miss, got %v", err)
	}
}

// Steam owns this format and may change it. A cache this reader cannot follow
// leaves a game unknown rather than failing every scan that touches Steam.
func TestSteamCloudTreatsAnUnreadableCacheAsNoKnowledge(t *testing.T) {
	steamRoot := t.TempDir()
	appcache := filepath.Join(steamRoot, "appcache")
	if err := os.MkdirAll(appcache, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appcache, "appinfo.vdf"), []byte("not appinfo"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := steamcloud.New(steamRoot).Find(context.Background(), steamIdentity("70")); !errors.Is(err, saveprofile.ErrNotFound) {
		t.Fatalf("expected an unreadable cache to be a miss, got %v", err)
	}
	missing := steamcloud.New(t.TempDir())
	if _, err := missing.Find(context.Background(), steamIdentity("70")); !errors.Is(err, saveprofile.ErrNotFound) {
		t.Fatalf("expected a Steam installation with no cache to be a miss, got %v", err)
	}
}

// An override exists because that operating system reaches the folder another
// way. A platform list that omits it is describing where Steam syncs, not
// where the game keeps its saves, and reading it literally would leave that
// OS with no save location at all.
func TestSteamCloudSpellsAnOSItsPlatformListOmitsButAnOverrideNames(t *testing.T) {
	steamRoot := t.TempDir()
	writeAppinfo(t, filepath.Join(steamRoot, "appcache"), app{
		id: 239030,
		section: map[string]any{
			"common": map[string]any{"name": "Papers, Please"},
			"ufs": map[string]any{
				"savefiles": map[string]any{
					"0": map[string]any{
						"root": "WinMyDocuments", "path": "PapersPlease", "pattern": "*",
						"platforms": map[string]any{"0": "Windows"},
					},
				},
				"rootoverrides": map[string]any{
					"0": map[string]any{
						"root": "WinMyDocuments", "os": "MacOS", "oscompare": "=",
						"useinstead": "MacAppSupport",
					},
				},
			},
		},
	})

	profile, err := steamcloud.New(steamRoot).Find(context.Background(), steamIdentity("239030"))
	if err != nil {
		t.Fatal(err)
	}
	spelling := make(map[string]string, len(profile.Rules))
	for _, rule := range profile.Rules {
		spelling[rule.OS] = rule.Path
	}
	if spelling[saveprofile.OSMacOS] != "<macAppSupport>/PapersPlease/*" {
		t.Errorf("expected the override's operating system to be spelled, got %+v", spelling)
	}
	if spelling[saveprofile.OSWindows] != "<winDocuments>/PapersPlease/*" {
		t.Errorf("expected the declared platform to keep its spelling, got %+v", spelling)
	}
}

// Declared folders are a configuration, not an observation. When Steam's own
// bookkeeping shows every synced file was written through the API, the game
// keeps no folder to protect however its configuration still reads.
func TestSteamCloudTrustsWhatWasSyncedOverWhatWasDeclared(t *testing.T) {
	steamRoot := t.TempDir()
	writeAppinfo(t, filepath.Join(steamRoot, "appcache"), app{
		id: 400,
		section: map[string]any{
			"common": map[string]any{"name": "Portal"},
			"ufs": map[string]any{
				"savefiles": map[string]any{
					"0": map[string]any{"root": "gameinstall", "path": "portal/save", "pattern": "*.sav"},
				},
			},
		},
	})
	writeRemoteCache(t, steamRoot, "67689364", "400", "0")

	provider := steamcloud.New(steamRoot)
	if _, err := provider.Find(context.Background(), steamIdentity("400")); !errors.Is(err, steamcloud.ErrCloudAPI) {
		t.Fatalf("expected a game synced only through the API to say so, got %v", err)
	}

	// One file from a real folder is enough to show the folders are in use.
	writeRemoteCache(t, steamRoot, "67689364", "400", "1")
	profile, err := steamcloud.New(steamRoot).Find(context.Background(), steamIdentity("400"))
	if err != nil {
		t.Fatalf("expected the declared folder to stand, got %v", err)
	}
	if len(profile.Rules) != 1 || profile.Rules[0].Path != "<base>/portal/save/*.sav" {
		t.Fatalf("unexpected rules: %+v", profile.Rules)
	}
}

// One account's stale bookkeeping must not answer for another's.
func TestSteamCloudReadsEveryAccountsBookkeeping(t *testing.T) {
	steamRoot := t.TempDir()
	writeAppinfo(t, filepath.Join(steamRoot, "appcache"), app{
		id: 400,
		section: map[string]any{
			"common": map[string]any{"name": "Portal"},
			"ufs": map[string]any{
				"savefiles": map[string]any{
					"0": map[string]any{"root": "gameinstall", "path": "portal/save", "pattern": "*.sav"},
				},
			},
		},
	})
	writeRemoteCache(t, steamRoot, "11111111", "400", "0")
	writeRemoteCache(t, steamRoot, "22222222", "400", "1")

	if _, err := steamcloud.New(steamRoot).Find(context.Background(), steamIdentity("400")); err != nil {
		t.Fatalf("expected the account syncing from a folder to decide it, got %v", err)
	}
}

func writeRemoteCache(t *testing.T, steamRoot, account, appID, root string) {
	t.Helper()
	directory := filepath.Join(steamRoot, "userdata", account, appID)
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	cache := "\"" + appID + "\"\n{\n\t\"save.dat\"\n\t{\n\t\t\"root\"\t\t\"" + root + "\"\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(directory, "remotecache.vdf"), []byte(cache), 0600); err != nil {
		t.Fatal(err)
	}
}

// Steam has written more than one appinfo format. The one before the key
// table spells every key where it is used and has no table pointer in its
// header; reading it as the newer one would leave every game unknown.
func TestSteamCloudReadsTheFormatBeforeTheKeyTable(t *testing.T) {
	steamRoot := t.TempDir()
	writeAppinfoVersion(t, filepath.Join(steamRoot, "appcache"), 0x07564428, app{
		id: 400,
		section: map[string]any{
			"common": map[string]any{"name": "Portal"},
			"ufs": map[string]any{
				"savefiles": map[string]any{
					"0": map[string]any{"root": "gameinstall", "path": "portal/save", "pattern": "*.sav"},
				},
			},
		},
	})

	profile, err := steamcloud.New(steamRoot).Find(context.Background(), steamIdentity("400"))
	if err != nil {
		t.Fatal(err)
	}
	if profile.Title != "Portal" || len(profile.Rules) != 1 ||
		profile.Rules[0].Path != "<base>/portal/save/*.sav" {
		t.Fatalf("unexpected profile from the older format: %+v", profile)
	}
}

// A root this reader cannot place leaves the game unknown, not answered.
// Guessing where SteamCloudDocuments expands would hand back a folder the
// game may not use; claiming the game keeps no save folder would close a
// question another source could still answer correctly.
func TestSteamCloudLeavesARootItCannotPlaceUnknown(t *testing.T) {
	steamRoot := t.TempDir()
	writeAppinfo(t, filepath.Join(steamRoot, "appcache"), app{
		id: 400,
		section: map[string]any{
			"common": map[string]any{"name": "Portal"},
			"ufs": map[string]any{
				"savefiles": map[string]any{
					"0": map[string]any{"root": "SteamCloudDocuments", "path": "saves", "pattern": "*"},
				},
			},
		},
	})

	_, err := steamcloud.New(steamRoot).Find(context.Background(), steamIdentity("400"))
	if !errors.Is(err, saveprofile.ErrNotFound) {
		t.Fatalf("expected an unplaceable root to leave the game unknown, got %v", err)
	}
}

// Steam records each synced file under the folder it came from, so a declared
// folder nothing was ever synced from is one this game no longer uses.
func TestSteamCloudKeepsOnlyTheFoldersSteamHasSyncedFrom(t *testing.T) {
	steamRoot := t.TempDir()
	writeAppinfo(t, filepath.Join(steamRoot, "appcache"), app{
		id: 400,
		section: map[string]any{
			"common": map[string]any{"name": "Portal"},
			"ufs": map[string]any{
				"savefiles": map[string]any{
					"0": map[string]any{"root": "gameinstall", "path": "portal/save", "pattern": "*.sav"},
					"1": map[string]any{"root": "gameinstall", "path": "legacy/save", "pattern": "*.sav"},
				},
			},
		},
	})
	directory := filepath.Join(steamRoot, "userdata", "67689364", "400")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	cache := "\"400\"\n{\n\t\"ChangeNumber\"\t\t\"9\"\n\t\"portal/save/quick.sav\"\n\t{\n\t\t\"root\"\t\t\"1\"\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(directory, "remotecache.vdf"), []byte(cache), 0600); err != nil {
		t.Fatal(err)
	}

	profile, err := steamcloud.New(steamRoot).Find(context.Background(), steamIdentity("400"))
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Rules) != 1 || profile.Rules[0].Path != "<base>/portal/save/*.sav" {
		t.Fatalf("expected only the folder Steam synced from, got %+v", profile.Rules)
	}
}
